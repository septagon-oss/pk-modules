// Implements: REQ-TENANT-001.
// Per: ADR-0017.
// Discipline: C-14.

package tenant

// module.go owns the singleton wiring for tenant_management: NewModule
// constructs an OSS *Module, Compose() returns the module.Composable used by
// the catalog, and Service/ContextProvider expose the public ports.
//
// Pro embeds *Module to add SSO-aware lookups, RLS, billing-tier quotas, and
// audit hooks without changing the OSS surface.
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
	"github.com/septagon-oss/pk-modules/pkg/portslib"
	"github.com/septagon-oss/pk-modules/pkg/tenant/migrations"
	"github.com/septagon-oss/pk-modules/pkg/tenant/store"
	"github.com/septagon-oss/pk-modules/pkg/tenant/store/sqlite"
)

// Module metadata constants used by both the catalog and admin shell.
const (
	ModuleID          = "tenant_management"
	ModuleName        = "Tenant Management"
	ModuleDescription = "Tenant CRUD, tenant context propagation, and isolation contracts."
	ModuleVersion     = "0.0.0"
	ReleaseVersion    = portslib.ReleaseVersion
)

// defaultSQLiteDriver is the driver name pk-modules expects callers to have
// registered. modernc.org/sqlite registers itself as "sqlite" by default.
const defaultSQLiteDriver = "sqlite"

// Module is the OSS tenant_management module. Pro embeds *Module and adds
// Pro-only fields/methods.
type Module struct {
	metadata pkmodule.Metadata
	store    store.Store
	svc      *service
	provider contextProvider
	handler  *Handler
}

// NewModule constructs a tenant module.
func NewModule(opts ...Option) (*Module, error) {
	cfg := config{
		sqliteDriver: defaultSQLiteDriver,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
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
			Version:     ReleaseVersion,
		},
		store:    st,
		provider: contextProvider{},
	}
	m.svc = newService(st)
	m.handler = NewHandler(m.svc)

	if err := registerAdmin(cfg.admin); err != nil {
		return nil, err
	}
	if err := registerHealth(cfg.health, st); err != nil {
		return nil, err
	}
	return m, nil
}

// MustNewModule is the panic-on-error variant of NewModule. It mirrors
// module.Must from pk-core for callers wiring into a catalog literal.
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

// resolveStore selects the active store implementation. If the caller passed
// WithStore, that wins. Otherwise we try WithSQLiteDSN. If neither is
// configured, we return an error so callers do not silently get an
// uninitialized module.
func resolveStore(cfg config) (store.Store, error) {
	switch {
	case cfg.store != nil:
		return cfg.store, nil
	case cfg.sqliteDSN != "":
		st, err := sqlite.Open(cfg.sqliteDriver, cfg.sqliteDSN)
		if err != nil {
			return nil, fmt.Errorf("tenant: open sqlite store: %w", err)
		}
		return st, nil
	default:
		return nil, errors.New("tenant: no store configured — use WithStore or WithSQLiteDSN")
	}
}

// registerHealth registers a simple "tenant_management.store" check that
// pings the configured store via List with a cancellable context.
func registerHealth(r portslib.HealthRegistrar, st store.Store) error {
	if r == nil {
		return nil
	}
	checker := health.CheckerFunc(func(ctx context.Context) error {
		_, err := st.List(ctx)
		return err
	})
	return r.Register("tenant_management.store", checker)
}

// Compose returns the module.Composable representation that the catalog
// consumes when validating port wiring.
func (m *Module) Compose() pkmodule.Composable {
	return pkmodule.Must(
		m.metadata,
		pkmodule.WithProvides(
			pkmodule.Provide[TenantService](ModuleVersion),
			pkmodule.Provide[TenantContextProvider](ModuleVersion),
		),
		pkmodule.WithDependencies(
			pkmodule.OptionalPort[portslib.AdminRegistrar](pkmodule.PortSpec{
				Version:           portslib.AdminRegistrarContractVersion,
				Purpose:           "Mount the tenants admin page.",
				Category:          pkmodule.DependencyCategoryUI,
				SubCategory:       "admin",
				PreferredProvider: "admin_management",
			}),
			pkmodule.OptionalPort[portslib.HealthRegistrar](pkmodule.PortSpec{
				Version:           "0.0.0",
				Purpose:           "Surface tenant_management store reachability.",
				Category:          pkmodule.DependencyCategoryMonitoring,
				SubCategory:       "health",
				PreferredProvider: "health_management",
			}),
		),
	)
}

// Migrations exposes the embedded migrations FS for app-level migration
// runners.
func (m *Module) Migrations() fs.FS {
	em := migrations.FS()
	return em
}

// Service returns a TenantService backed by this module's store. Pro can
// embed and override.
func (m *Module) Service() TenantService { return m.svc }

// ContextProvider returns the TenantContextProvider for this module.
func (m *Module) ContextProvider() TenantContextProvider { return m.provider }

// HTTPHandler returns the wired HTTP handler so the host application can
// mount it on its router of choice.
func (m *Module) HTTPHandler() *Handler { return m.handler }

// Store returns the underlying Store so Pro can implement
// TenantIsolationEnforcer or wrap with auditing.
func (m *Module) Store() store.Store { return m.store }
