package apikey

// module.go owns the singleton wiring for api_key_management: NewModule
// constructs an OSS *Module, Compose() returns the module.Composable used
// by the catalog, and Service / Authenticator expose the public ports.
//
// Pro embeds *Module to add rotation hooks, finer-grained scopes, and
// per-key telemetry without changing the OSS surface.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0017 (composition through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	pkmodule "github.com/septagon-oss/pk-core/pkg/module"
	"github.com/septagon-oss/pk-core/pkg/observability/health"
	"github.com/septagon-oss/pk-core/pkg/security/identity"
	"github.com/septagon-oss/pk-core/pkg/security/passhash"

	"github.com/septagon-oss/pk-modules/pkg/apikey/migrations"
	"github.com/septagon-oss/pk-modules/pkg/apikey/store"
	"github.com/septagon-oss/pk-modules/pkg/apikey/store/sqlite"
	"github.com/septagon-oss/pk-modules/pkg/audit"
	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

// Module metadata constants used by both the catalog and admin shell.
const (
	ModuleID          = "api_key_management"
	ModuleName        = "API Key Management"
	ModuleDescription = "Tenant-scoped API key issuance, verification, and revocation."
	ModuleVersion     = "0.0.0"
)

// defaultSQLiteDriver matches modernc.org/sqlite's default registration name.
const defaultSQLiteDriver = "sqlite"

// authMethodAPIKey is the AuthMethod label attached to the Principal when
// the authenticator middleware accepts a key.
const authMethodAPIKey = "api_key"

// Module is the OSS api_key_management module. Pro embeds *Module and adds
// Pro-only fields/methods.
type Module struct {
	metadata pkmodule.Metadata
	store    store.Store
	hasher   passhash.Hasher
	audit    audit.AuditEmitter
	admin    portslib.AdminRegistrar
	health   portslib.HealthRegistrar
	svc      *service
	handler  *Handler
	authn    *authenticator
}

// NewModule constructs an api_key_management module.
func NewModule(opts ...Option) (*Module, error) {
	cfg := config{
		sqliteDriver: defaultSQLiteDriver,
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
	if cfg.hasher == nil {
		bh, err := passhash.NewBcrypt(passhash.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("apikey: default hasher: %w", err)
		}
		cfg.hasher = bh
	}

	st, err := resolveStore(cfg)
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
		store:  st,
		hasher: cfg.hasher,
		audit:  cfg.audit,
		admin:  cfg.admin,
		health: cfg.health,
	}
	m.svc = newService(st, cfg.hasher, cfg.audit)
	m.handler = NewHandler(m.svc)
	m.authn = newAuthenticator(m.svc)

	if err := registerAdmin(cfg.admin); err != nil {
		return nil, err
	}
	if err := registerHealth(cfg.health, st); err != nil {
		return nil, err
	}
	return m, nil
}

// MustNewModule is the panic-on-error variant of NewModule.
//
// Panics if NewModule returns an error (for example, when required options
// such as a store or DSN are missing or the backing database cannot be
// opened). Use NewModule when an error return is preferred.
func MustNewModule(opts ...Option) *Module {
	m, err := NewModule(opts...)
	if err != nil {
		panic(err)
	}
	return m
}

func resolveStore(cfg config) (store.Store, error) {
	switch {
	case cfg.store != nil:
		return cfg.store, nil
	case cfg.sqliteDSN != "":
		st, err := sqlite.Open(cfg.sqliteDriver, cfg.sqliteDSN)
		if err != nil {
			return nil, fmt.Errorf("apikey: open sqlite store: %w", err)
		}
		return st, nil
	default:
		return nil, errors.New("apikey: no store configured — use WithStore or WithSQLiteDSN")
	}
}

// registerHealth wires a check that the store responds to a List call.
func registerHealth(r portslib.HealthRegistrar, st store.Store) error {
	if r == nil {
		return nil
	}
	checker := health.CheckerFunc(func(ctx context.Context) error {
		_, err := st.List(ctx, "_health_probe_")
		return err
	})
	return r.Register("api_key_management.store", checker)
}

// Compose returns the module.Composable representation the catalog
// consumes when validating port wiring.
func (m *Module) Compose() pkmodule.Composable {
	return pkmodule.Must(
		m.metadata,
		pkmodule.WithProvides(
			pkmodule.Provide[APIKeyService](ModuleVersion),
			pkmodule.Provide[APIKeyAuthenticator](ModuleVersion),
		),
		pkmodule.WithDependencies(
			pkmodule.Optional[audit.AuditEmitter](pkmodule.DependencySpec{
				Version:           "0.0.0",
				Purpose:           "Emit apikey.issued / apikey.revoked events.",
				Category:          pkmodule.DependencyCategorySecurity,
				SubCategory:       "audit",
				PreferredProvider: "audit_management",
			}),
			pkmodule.Optional[portslib.AdminRegistrar](pkmodule.DependencySpec{
				Version:           "0.0.0",
				Purpose:           "Mount the API keys admin page.",
				Category:          pkmodule.DependencyCategoryUI,
				SubCategory:       "admin",
				PreferredProvider: "admin_management",
			}),
			pkmodule.Optional[portslib.HealthRegistrar](pkmodule.DependencySpec{
				Version:           "0.0.0",
				Purpose:           "Surface api_key_management store reachability.",
				Category:          pkmodule.DependencyCategoryMonitoring,
				SubCategory:       "health",
				PreferredProvider: "health_management",
			}),
		),
	)
}

// Migrations exposes the embedded migrations FS for app-level migration runners.
func (m *Module) Migrations() fs.FS { return migrations.FS() }

// Service returns the APIKeyService backed by this module's store.
func (m *Module) Service() APIKeyService { return m.svc }

// HTTPHandler returns the wired HTTP handler so the host application can
// mount it on its router of choice.
func (m *Module) HTTPHandler() *Handler { return m.handler }

// Authenticator returns the APIKeyAuthenticator middleware so apps can
// install it on protected routers.
func (m *Module) Authenticator() APIKeyAuthenticator { return m.authn }

// Store returns the underlying Store so Pro can wrap with auditing.
func (m *Module) Store() store.Store { return m.store }

// Hasher returns the configured password hasher.
func (m *Module) Hasher() passhash.Hasher { return m.hasher }

// authenticator implements APIKeyAuthenticator over an APIKeyService.
type authenticator struct {
	svc APIKeyService
}

func newAuthenticator(svc APIKeyService) *authenticator {
	return &authenticator{svc: svc}
}

// Middleware returns an http middleware that attaches an
// identity.Principal to the request context when the Authorization header
// carries a valid bearer token. On invalid/missing/expired/revoked keys
// the middleware short-circuits with 401.
func (a *authenticator) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			plaintext, ok := extractBearer(header)
			if !ok {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			key, err := a.svc.Verify(r.Context(), plaintext)
			if err != nil {
				http.Error(w, "invalid api key", http.StatusUnauthorized)
				return
			}
			principal := identity.Principal{
				Subject:    key.UserID,
				TenantID:   key.TenantID,
				Scopes:     append([]string(nil), key.Scopes...),
				AuthMethod: authMethodAPIKey,
			}
			ctx := identity.ContextWithPrincipal(r.Context(), principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// extractBearer parses a "Bearer <token>" Authorization header. Comparison
// of the scheme is case-insensitive per RFC 6750.
func extractBearer(header string) (string, bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", false
	}
	const scheme = "bearer "
	if len(header) <= len(scheme) || !strings.EqualFold(header[:len(scheme)], scheme) {
		return "", false
	}
	token := strings.TrimSpace(header[len(scheme):])
	if token == "" {
		return "", false
	}
	return token, true
}
