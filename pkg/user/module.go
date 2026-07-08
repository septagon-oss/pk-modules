package user

// module.go owns the singleton wiring for user_management: NewModule
// constructs an OSS *Module, Compose() returns the module.Composable used by
// the catalog, and Service exposes the public port.
//
// Pro embeds *Module to add SSO-aware lookups, richer RBAC, billing-tier
// quotas, and audit hooks without changing the OSS surface.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0017 (composition through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	pkmodule "github.com/septagon-oss/pk-core/pkg/module"
	"github.com/septagon-oss/pk-core/pkg/observability/health"
	"github.com/septagon-oss/pk-core/pkg/security/passhash"

	"github.com/septagon-oss/pk-modules/pkg/portslib"
	"github.com/septagon-oss/pk-modules/pkg/tenant"
	"github.com/septagon-oss/pk-modules/pkg/user/migrations"
	"github.com/septagon-oss/pk-modules/pkg/user/store"
	"github.com/septagon-oss/pk-modules/pkg/user/store/sqlite"
)

// Module metadata constants used by both the catalog and admin shell.
const (
	ModuleID          = "user_management"
	ModuleName        = "User Management"
	ModuleDescription = "Tenant-scoped user CRUD with pluggable password hashing."
	ModuleVersion     = "0.0.0"
)

// defaultSQLiteDriver is the driver name pk-modules expects callers to have
// registered. modernc.org/sqlite registers itself as "sqlite" by default.
const defaultSQLiteDriver = "sqlite"

// Module is the OSS user_management module. Pro embeds *Module and adds
// Pro-only fields/methods.
type Module struct {
	metadata pkmodule.Metadata
	store    store.Store
	hasher   passhash.Hasher
	tenant   tenant.TenantService
	svc      *service
	handler  *Handler
	admin    portslib.AdminRegistrar
	health   portslib.HealthRegistrar
}

// NewModule constructs a user module.
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
			return nil, fmt.Errorf("user: default hasher: %w", err)
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
		tenant: cfg.tenant,
		admin:  cfg.admin,
		health: cfg.health,
	}
	m.svc = newService(st, cfg.hasher, cfg.tenant)
	m.handler = NewHandler(m.svc)

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

// resolveStore selects the active store implementation.
func resolveStore(cfg config) (store.Store, error) {
	switch {
	case cfg.store != nil:
		return cfg.store, nil
	case cfg.sqliteDSN != "":
		st, err := sqlite.Open(cfg.sqliteDriver, cfg.sqliteDSN)
		if err != nil {
			return nil, fmt.Errorf("user: open sqlite store: %w", err)
		}
		return st, nil
	default:
		return nil, errors.New("user: no store configured — use WithStore or WithSQLiteDSN")
	}
}

// registerHealth registers a simple "user_management.store" check that pings
// the configured store via a lightweight List call.
func registerHealth(r portslib.HealthRegistrar, st store.Store) error {
	if r == nil {
		return nil
	}
	checker := health.CheckerFunc(func(ctx context.Context) error {
		_, err := st.List(ctx, "_health_probe_", 1, 0)
		return err
	})
	return r.Register("user_management.store", checker)
}

// Compose returns the module.Composable representation that the catalog
// consumes when validating port wiring.
func (m *Module) Compose() pkmodule.Composable {
	return pkmodule.Must(
		m.metadata,
		pkmodule.WithProvides(
			pkmodule.Provide[UserService](ModuleVersion),
			pkmodule.Provide[UserBoundaryReader](ModuleVersion),
		),
		pkmodule.WithDependencies(
			pkmodule.Optional[portslib.AdminRegistrar](pkmodule.DependencySpec{
				Version:           "0.0.0",
				Purpose:           "Mount the users admin page.",
				Category:          pkmodule.DependencyCategoryUI,
				SubCategory:       "admin",
				PreferredProvider: "admin_management",
			}),
			pkmodule.Optional[portslib.HealthRegistrar](pkmodule.DependencySpec{
				Version:           "0.0.0",
				Purpose:           "Surface user_management store reachability.",
				Category:          pkmodule.DependencyCategoryMonitoring,
				SubCategory:       "health",
				PreferredProvider: "health_management",
			}),
			pkmodule.Optional[tenant.TenantService](pkmodule.DependencySpec{
				Version:           "0.0.0",
				Purpose:           "Validate tenant references against the tenant module.",
				Category:          pkmodule.DependencyCategoryBusiness,
				SubCategory:       "tenant",
				PreferredProvider: "tenant_management",
			}),
		),
	)
}

// Migrations exposes the embedded migrations FS for app-level migration
// runners.
func (m *Module) Migrations() fs.FS { return migrations.FS() }

// Service returns a UserService backed by this module's store. Pro can embed
// and override.
func (m *Module) Service() UserService { return m.svc }

// HTTPHandler returns the wired HTTP handler so the host application can
// mount it on its router of choice.
func (m *Module) HTTPHandler() *Handler { return m.handler }

// Store returns the underlying Store so Pro can wrap with auditing.
func (m *Module) Store() store.Store { return m.store }

// Hasher returns the configured password hasher so Pro and callers wiring
// authentication flows can introspect the active algorithm.
func (m *Module) Hasher() passhash.Hasher { return m.hasher }
