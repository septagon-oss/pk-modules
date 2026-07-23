// Implements: REQ-AUDIT-001.
// Per: ADR-0017.
// Discipline: C-14.

package audit

// module.go owns the singleton wiring for audit_management: NewModule
// constructs an OSS *Module, Compose() returns the module.Composable used by
// the catalog, and Service/Reader expose the public ports.
//
// Pro embeds *Module to add retention enforcement, partitioning, and
// forwarding to external SIEMs without changing the OSS surface.
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

	"github.com/septagon-oss/pk-modules/pkg/audit/migrations"
	"github.com/septagon-oss/pk-modules/pkg/audit/store"
	"github.com/septagon-oss/pk-modules/pkg/audit/store/sqlite"
	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

// Module metadata constants used by both the catalog and admin shell.
const (
	ModuleID          = "audit_management"
	ModuleName        = "Audit Management"
	ModuleDescription = "Append-only tenant-scoped audit event log."
	ModuleVersion     = "0.0.0"
)

// defaultSQLiteDriver is the driver name pk-modules expects callers to have
// registered. modernc.org/sqlite registers itself as "sqlite" by default.
const defaultSQLiteDriver = "sqlite"

// Module is the OSS audit_management module. Pro embeds *Module and adds
// Pro-only fields/methods.
type Module struct {
	metadata pkmodule.Metadata
	store    store.Store
	svc      *service
	handler  *Handler
}

// NewModule constructs an audit module.
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
			Version:     ModuleVersion,
		},
		store: st,
	}
	m.svc = newService(st)
	m.handler = NewHandler(m.svc, m.svc)

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
			return nil, fmt.Errorf("audit: open sqlite store: %w", err)
		}
		return st, nil
	default:
		return nil, errors.New("audit: no store configured — use WithStore or WithSQLiteDSN")
	}
}

// registerHealth registers a simple "audit_management.store" check that runs
// a bounded Query against the configured store.
func registerHealth(r portslib.HealthRegistrar, st store.Store) error {
	if r == nil {
		return nil
	}
	checker := health.CheckerFunc(func(ctx context.Context) error {
		// Query with an unreachable filter to keep the call cheap; we only
		// care that the store responds without error.
		_, err := st.Query(ctx, store.QueryFilter{
			TenantID: "_health_probe_",
			Until:    time.Unix(0, 0),
			Limit:    1,
		})
		return err
	})
	return r.Register("audit_management.store", checker)
}

// Compose returns the module.Composable representation that the catalog
// consumes when validating port wiring.
func (m *Module) Compose() pkmodule.Composable {
	return pkmodule.Must(
		m.metadata,
		pkmodule.WithProvides(
			pkmodule.Provide[AuditService](ModuleVersion),
			pkmodule.Provide[AuditReader](ModuleVersion),
		),
		pkmodule.WithDependencies(
			pkmodule.OptionalPort[portslib.AdminRegistrar](pkmodule.PortSpec{
				Version:           "0.0.0",
				Purpose:           "Mount the audit log admin page.",
				Category:          pkmodule.DependencyCategoryUI,
				SubCategory:       "admin",
				PreferredProvider: "admin_management",
			}),
			pkmodule.OptionalPort[portslib.HealthRegistrar](pkmodule.PortSpec{
				Version:           "0.0.0",
				Purpose:           "Surface audit_management store reachability.",
				Category:          pkmodule.DependencyCategoryMonitoring,
				SubCategory:       "health",
				PreferredProvider: "health_management",
			}),
		),
	)
}

// Migrations exposes the embedded migrations FS for app-level migration
// runners.
func (m *Module) Migrations() fs.FS { return migrations.FS() }

// Service returns the AuditService backed by this module's store.
func (m *Module) Service() AuditService { return m.svc }

// Reader returns the AuditReader backed by this module's store.
func (m *Module) Reader() AuditReader { return m.svc }

// HTTPHandler returns the wired HTTP handler so the host application can
// mount it on its router of choice.
func (m *Module) HTTPHandler() *Handler { return m.handler }

// Store returns the underlying Store so Pro can wrap with forwarding.
func (m *Module) Store() store.Store { return m.store }
