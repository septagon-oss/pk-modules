package content

// module.go owns the singleton wiring for content_management: NewModule
// constructs an OSS *Module, Compose() returns the module.Composable used by
// the catalog, and Service/Reader/Publisher expose the public ports.
//
// Pro embeds *Module to add scheduled publishing, revision history, and
// approval workflows without changing the OSS surface.
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

	"github.com/septagon-oss/pk-modules/pkg/audit"
	"github.com/septagon-oss/pk-modules/pkg/content/migrations"
	"github.com/septagon-oss/pk-modules/pkg/content/store"
	"github.com/septagon-oss/pk-modules/pkg/content/store/sqlite"
	"github.com/septagon-oss/pk-modules/pkg/portslib"
	"github.com/septagon-oss/pk-modules/pkg/tenant"
)

// Module metadata constants used by both the catalog and admin shell.
const (
	ModuleID          = "content_management"
	ModuleName        = "Content Management"
	ModuleDescription = "Tenant-scoped pages, posts, and snippets with markdown/HTML bodies."
	ModuleVersion     = "0.0.0"
)

// defaultSQLiteDriver matches modernc.org/sqlite's default registration name.
const defaultSQLiteDriver = "sqlite"

// Module is the OSS content_management module. Pro embeds *Module and adds
// Pro-only fields/methods.
type Module struct {
	metadata pkmodule.Metadata
	store    store.Store
	tenant   tenant.TenantService
	audit    audit.AuditEmitter
	admin    portslib.AdminRegistrar
	health   portslib.HealthRegistrar
	svc      *service
	handler  *Handler
}

// NewModule constructs a content module.
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
		tenant: cfg.tenant,
		audit:  cfg.audit,
		admin:  cfg.admin,
		health: cfg.health,
	}
	m.svc = newService(st, cfg.audit)
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
			return nil, fmt.Errorf("content: open sqlite store: %w", err)
		}
		return st, nil
	default:
		return nil, errors.New("content: no store configured — use WithStore or WithSQLiteDSN")
	}
}

// registerHealth wires a lightweight check that the store responds to a
// bounded List call.
func registerHealth(r portslib.HealthRegistrar, st store.Store) error {
	if r == nil {
		return nil
	}
	checker := health.CheckerFunc(func(ctx context.Context) error {
		_, err := st.List(ctx, "_health_probe_", "", 1, 0)
		return err
	})
	return r.Register("content_management.store", checker)
}

// Compose returns the module.Composable representation the catalog consumes
// when validating port wiring.
func (m *Module) Compose() pkmodule.Composable {
	return pkmodule.Must(m.metadata,
		pkmodule.WithProvides(
			pkmodule.Provide[ContentService](ModuleVersion),
			pkmodule.Provide[ContentReader](ModuleVersion),
			pkmodule.Provide[ContentPublisher](ModuleVersion),
		),
		pkmodule.WithDependencies(
			pkmodule.Optional[tenant.TenantService](pkmodule.DependencySpec{
				Version:           "0.0.0",
				Purpose:           "Validate tenant references for slug uniqueness scope.",
				Category:          pkmodule.DependencyCategoryBusiness,
				SubCategory:       "tenant",
				PreferredProvider: "tenant_management",
			}),
			pkmodule.Optional[audit.AuditEmitter](pkmodule.DependencySpec{
				Version:           "0.0.0",
				Purpose:           "Emit content.created/updated/published/unpublished events.",
				Category:          pkmodule.DependencyCategorySecurity,
				SubCategory:       "audit",
				PreferredProvider: "audit_management",
			}),
			pkmodule.Optional[portslib.AdminRegistrar](pkmodule.DependencySpec{
				Version:           "0.0.0",
				Purpose:           "Mount the content admin page.",
				Category:          pkmodule.DependencyCategoryUI,
				SubCategory:       "admin",
				PreferredProvider: "admin_management",
			}),
			pkmodule.Optional[portslib.HealthRegistrar](pkmodule.DependencySpec{
				Version:           "0.0.0",
				Purpose:           "Surface content_management store reachability.",
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

// Service returns the ContentService backed by this module's store.
func (m *Module) Service() ContentService { return m.svc }

// Reader returns the ContentReader backed by this module's store.
func (m *Module) Reader() ContentReader { return m.svc }

// Publisher returns the ContentPublisher backed by this module's store.
func (m *Module) Publisher() ContentPublisher { return m.svc }

// HTTPHandler returns the wired HTTP handler so the host application can
// mount it on its router of choice.
func (m *Module) HTTPHandler() *Handler { return m.handler }

// Store returns the underlying Store so Pro can wrap with auditing.
func (m *Module) Store() store.Store { return m.store }
