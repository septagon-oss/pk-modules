package auth

// module.go owns the singleton wiring for auth_management: NewModule
// constructs an OSS *Module, Compose() returns the module.Composable used
// by the catalog, and Service exposes the public port.
//
// Pro embeds *Module to add SSO-aware lookups, MFA, rate limiting, and
// risk scoring without changing the OSS surface.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0017 (composition through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	pkmodule "github.com/septagon-oss/pk-core/pkg/module"
	"github.com/septagon-oss/pk-core/pkg/observability/health"
	"github.com/septagon-oss/pk-core/pkg/security/passhash"

	"github.com/septagon-oss/pk-modules/pkg/audit"
	"github.com/septagon-oss/pk-modules/pkg/auth/migrations"
	authstore "github.com/septagon-oss/pk-modules/pkg/auth/store"
	sqlitestore "github.com/septagon-oss/pk-modules/pkg/auth/store/sqlite"
	"github.com/septagon-oss/pk-modules/pkg/portslib"
	"github.com/septagon-oss/pk-modules/pkg/user"
)

// Module metadata constants used by both the catalog and admin shell.
const (
	ModuleID          = "auth_management"
	ModuleName        = "Auth Management"
	ModuleDescription = "Session-cookie login flow on top of user_management."
	ModuleVersion     = "0.0.0"
)

// defaultSQLiteDriver matches modernc.org/sqlite's default registration name.
const defaultSQLiteDriver = "sqlite"

// defaultSessionTTL is the OSS default session lifetime. Apps that need a
// shorter window should override via WithSessionTTL.
const defaultSessionTTL = 24 * time.Hour

// Module is the OSS auth_management module. Pro embeds *Module and adds
// Pro-only fields/methods.
type Module struct {
	metadata   pkmodule.Metadata
	sessions   SessionStore
	users      user.UserBoundaryReader
	hasher     passhash.Hasher
	policy     LoginPolicy
	audit      audit.AuditEmitter
	admin      portslib.AdminRegistrar
	health     portslib.HealthRegistrar
	sessionTTL time.Duration
	svc        *service
	handler    *Handler
}

// NewModule constructs an auth module.
func NewModule(opts ...Option) (*Module, error) {
	cfg := config{
		sqliteDriver: defaultSQLiteDriver,
		sessionTTL:   defaultSessionTTL,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.admin == nil {
		cfg.admin = portslib.NoopAdminRegistrar()
	}
	if cfg.health == nil {
		cfg.health = portslib.NoopHealthRegistrar()
	}
	if cfg.policy == nil {
		cfg.policy = PermissiveLoginPolicy()
	}
	if cfg.sessionTTL <= 0 {
		cfg.sessionTTL = defaultSessionTTL
	}
	if cfg.hasher == nil {
		bh, err := passhash.NewBcrypt(passhash.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("auth: default hasher: %w", err)
		}
		cfg.hasher = bh
	}

	sessions, err := resolveSessionStore(cfg)
	if err != nil {
		return nil, err
	}

	m := &Module{
		metadata: pkmodule.Metadata{
			ID:          ModuleID,
			Name:        ModuleName,
			Description: ModuleDescription,
			Version:     ModuleVersion,
		},
		sessions:   sessions,
		users:      cfg.users,
		hasher:     cfg.hasher,
		policy:     cfg.policy,
		audit:      cfg.audit,
		admin:      cfg.admin,
		health:     cfg.health,
		sessionTTL: cfg.sessionTTL,
	}
	m.svc = newService(sessions, cfg.users, cfg.hasher, cfg.policy, cfg.audit, cfg.sessionTTL)
	m.handler = NewHandler(m.svc)

	if err := registerAdmin(cfg.admin); err != nil {
		return nil, err
	}
	if err := registerHealth(cfg.health, sessions); err != nil {
		return nil, err
	}
	return m, nil
}

// MustNewModule is the panic-on-error variant of NewModule.
func MustNewModule(opts ...Option) *Module {
	m, err := NewModule(opts...)
	if err != nil {
		panic(err)
	}
	return m
}

// resolveSessionStore selects the active session store implementation.
func resolveSessionStore(cfg config) (SessionStore, error) {
	switch {
	case cfg.sessions != nil:
		return cfg.sessions, nil
	case cfg.sqliteDSN != "":
		st, err := sqlitestore.Open(cfg.sqliteDriver, cfg.sqliteDSN)
		if err != nil {
			return nil, fmt.Errorf("auth: open sqlite session store: %w", err)
		}
		return newStoreAdapter(st), nil
	default:
		return nil, errors.New("auth: no session store configured — use WithSessionStore or WithSQLiteDSN")
	}
}

// registerHealth wires a lightweight check that confirms the session store
// answers a probe lookup for a synthetic session id without error other
// than ErrNotFound.
func registerHealth(r portslib.HealthRegistrar, sessions SessionStore) error {
	if r == nil {
		return nil
	}
	checker := health.CheckerFunc(func(ctx context.Context) error {
		_, err := sessions.Get(ctx, "_health_probe_")
		if err == nil || errors.Is(err, authstore.ErrNotFound) {
			return nil
		}
		return err
	})
	return r.Register("auth_management.sessions", checker)
}

// Compose returns the module.Composable representation the catalog consumes
// when validating port wiring.
func (m *Module) Compose() pkmodule.Composable {
	return pkmodule.Must(m.metadata,
		pkmodule.WithProvides(
			pkmodule.Provide[AuthService](ModuleVersion),
		),
		pkmodule.WithDependencies(
			pkmodule.Require[user.UserBoundaryReader](pkmodule.DependencySpec{
				Version:           "0.0.0",
				Purpose:           "Resolve user credentials at login time.",
				Category:          pkmodule.DependencyCategoryBusiness,
				SubCategory:       "user",
				PreferredProvider: "user_management",
			}),
			pkmodule.Optional[audit.AuditEmitter](pkmodule.DependencySpec{
				Version:           "0.0.0",
				Purpose:           "Emit auth.login_success and auth.login_failure events.",
				Category:          pkmodule.DependencyCategorySecurity,
				SubCategory:       "audit",
				PreferredProvider: "audit_management",
			}),
			pkmodule.Optional[portslib.AdminRegistrar](pkmodule.DependencySpec{
				Version:           "0.0.0",
				Purpose:           "Mount the sessions admin page.",
				Category:          pkmodule.DependencyCategoryUI,
				SubCategory:       "admin",
				PreferredProvider: "admin_management",
			}),
			pkmodule.Optional[portslib.HealthRegistrar](pkmodule.DependencySpec{
				Version:           "0.0.0",
				Purpose:           "Surface auth_management session store reachability.",
				Category:          pkmodule.DependencyCategoryMonitoring,
				SubCategory:       "health",
				PreferredProvider: "health_management",
			}),
		),
	)
}

// Migrations exposes the embedded migrations FS for app-level migration runners.
func (m *Module) Migrations() fs.FS { return migrations.FS() }

// Service returns an AuthService backed by this module's session store.
func (m *Module) Service() AuthService { return m.svc }

// HTTPHandler returns the wired HTTP handler so the host application can
// mount it on its router of choice.
func (m *Module) HTTPHandler() *Handler { return m.handler }

// Sessions returns the underlying SessionStore so Pro can wrap with auditing.
func (m *Module) Sessions() SessionStore { return m.sessions }

// Hasher returns the configured password hasher.
func (m *Module) Hasher() passhash.Hasher { return m.hasher }

// SessionTTL returns the configured session lifetime.
func (m *Module) SessionTTL() time.Duration { return m.sessionTTL }

// storeAdapter bridges the row-shape SessionStore implemented by
// pkg/auth/store/sqlite into the public auth.SessionStore that operates on
// auth.Session values.
type storeAdapter struct {
	inner *sqlitestore.Store
}

func newStoreAdapter(s *sqlitestore.Store) SessionStore {
	return &storeAdapter{inner: s}
}

func (a *storeAdapter) Create(ctx context.Context, s *Session) error {
	if s == nil {
		return errors.New("auth: nil session")
	}
	return a.inner.Create(ctx, toStoreSession(s))
}

func (a *storeAdapter) Get(ctx context.Context, id string) (*Session, error) {
	row, err := a.inner.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return fromStoreSession(row), nil
}

func (a *storeAdapter) Revoke(ctx context.Context, id string) error {
	return a.inner.Revoke(ctx, id)
}

func (a *storeAdapter) RevokeByUser(ctx context.Context, userID string) error {
	return a.inner.RevokeByUser(ctx, userID)
}

func toStoreSession(s *Session) *authstore.Session {
	return &authstore.Session{
		ID:        s.ID,
		UserID:    s.UserID,
		TenantID:  s.TenantID,
		IssuedAt:  s.IssuedAt,
		ExpiresAt: s.ExpiresAt,
		RevokedAt: s.RevokedAt,
	}
}

func fromStoreSession(r *authstore.Session) *Session {
	if r == nil {
		return nil
	}
	return &Session{
		ID:        r.ID,
		UserID:    r.UserID,
		TenantID:  r.TenantID,
		IssuedAt:  r.IssuedAt,
		ExpiresAt: r.ExpiresAt,
		RevokedAt: r.RevokedAt,
	}
}
