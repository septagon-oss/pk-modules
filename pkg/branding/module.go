// Implements: REQ-BRANDING-001.
// Per: ADR-0017.
// Discipline: C-14.

package branding

// module.go owns the singleton wiring for branding_management: NewModule
// constructs an OSS *Module, Compose() returns the module.Composable used by
// the catalog, and Service exposes the public port.
//
// Pro embeds *Module to add object-storage-backed logos, richer font packs,
// and tenant-tier gating without changing the OSS surface.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0017 (composition through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	pkmodule "github.com/septagon-oss/pk-core/pkg/module"
	"github.com/septagon-oss/pk-core/pkg/observability/health"
	"github.com/septagon-oss/pk-modules/pkg/branding/migrations"
	"github.com/septagon-oss/pk-modules/pkg/branding/store"
	"github.com/septagon-oss/pk-modules/pkg/branding/store/sqlite"
	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

// Module metadata constants used by both the catalog and admin shell.
//
// ModuleVersion looks stranded (nothing in this package reads it directly)
// but is not dead: the OSS `pk explain modules` catalog (pk-tools
// cmd/pk/explain.go) reads every module's exported ModuleVersion const by
// name to build its listing, the same way it already does for
// tenant.ModuleVersion, user.ModuleVersion, and the other reference modules.
// Keep it even though no code in this repo references it.
const (
	ModuleID          = "branding_management"
	ModuleName        = "Branding"
	ModuleDescription = "Tenant branding: display name, logo, palette, and typography."
	ModuleVersion     = "0.1.0"
	ReleaseVersion    = portslib.ReleaseVersion
)

// defaultSQLiteDriver is the driver name pk-modules expects callers to have
// registered. modernc.org/sqlite registers itself as "sqlite" by default.
const defaultSQLiteDriver = "sqlite"

// defaultAdminBasePath mirrors admin.NewShell's own default mount point.
const defaultAdminBasePath = "/admin"

// Module is the OSS branding_management module. Pro embeds *Module and adds
// Pro-only fields/methods.
type Module struct {
	metadata pkmodule.Metadata
	store    store.Store
	svc      *Service
	handler  *Handler
}

// NewModule constructs a branding module.
func NewModule(opts ...Option) (*Module, error) {
	cfg := config{
		sqliteDriver: defaultSQLiteDriver,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	cfg.adminBasePath = normalizeAdminBasePath(cfg.adminBasePath)

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
		store: st,
	}
	m.svc = NewService(st)
	m.handler = NewHandler(m.svc, cfg.adminBasePath)

	if err := registerAdmin(cfg); err != nil {
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
			return nil, fmt.Errorf("branding: open sqlite store: %w", err)
		}
		return st, nil
	default:
		return nil, errors.New("branding: no store configured — use WithStore or WithSQLiteDSN")
	}
}

// normalizeAdminBasePath mirrors admin.NewShell's own normalization: an empty
// path falls back to the default, and the result always has a leading slash
// and no trailing slash.
func normalizeAdminBasePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		path = defaultAdminBasePath
	}
	return "/" + strings.Trim(path, "/")
}

// registerAdmin is a stub for v0.1.0: it takes the full config (including the
// admin registrar and the normalized admin base path) so Task 6 can mount the
// branding admin page without changing this call site. It intentionally does
// nothing yet.
func registerAdmin(cfg config) error {
	return nil
}

// registerHealth registers a "branding_management.store" check that probes
// the configured store with a Get for a sentinel tenant ID. store.ErrNotFound
// is the expected, healthy outcome on a reachable store with no matching
// tenant; only a real error (connection failure, etc.) fails the check.
func registerHealth(r portslib.HealthRegistrar, st store.Store) error {
	if r == nil {
		return nil
	}
	checker := health.CheckerFunc(func(ctx context.Context) error {
		_, err := st.Get(ctx, "healthcheck-probe")
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	})
	return r.Register("branding_management.store", checker)
}

// Compose returns the module.Composable representation that the catalog
// consumes when validating port wiring.
func (m *Module) Compose() pkmodule.Composable {
	return pkmodule.Must(
		m.metadata,
		pkmodule.WithProvides(
			pkmodule.Provide[portslib.BrandingResolver](portslib.BrandingContractVersion),
		),
		pkmodule.WithDependencies(
			pkmodule.OptionalPort[portslib.AdminRegistrar](pkmodule.PortSpec{
				Version:           portslib.AdminRegistrarContractVersion,
				Purpose:           "Mount the tenant branding page.",
				Category:          pkmodule.DependencyCategoryUI,
				SubCategory:       "admin",
				PreferredProvider: "admin_management",
			}),
			pkmodule.OptionalPort[portslib.HealthRegistrar](pkmodule.PortSpec{
				Version:           "0.0.0",
				Purpose:           "Surface branding_management store reachability.",
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
	return migrations.FS()
}

// Service returns the Service backed by this module's store. Pro can embed
// and override.
func (m *Module) Service() *Service { return m.svc }

// HTTPHandler returns the wired HTTP handler so the host application can
// mount it on its router of choice.
func (m *Module) HTTPHandler() *Handler { return m.handler }

// Store returns the underlying Store so Pro can implement object-storage-
// backed logos or wrap with auditing.
func (m *Module) Store() store.Store { return m.store }
