package health

// module.go owns the singleton wiring for health_management: NewModule
// constructs an OSS *Module, Compose() returns the module.Composable used by
// the catalog, and Registrar() exposes the portslib.HealthRegistrar that
// other modules wire into via WithHealthRegistrar.
//
// Pro embeds *Module to add external check forwarders, SLO scoring, and
// tenant-scoped probes without changing the OSS surface.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0017 (composition through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"fmt"

	pkmodule "github.com/septagon-oss/pk-core/pkg/module"
	"github.com/septagon-oss/pk-core/pkg/observability/health"

	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

// Module metadata constants used by both the catalog and admin shell.
const (
	ModuleID          = "health_management"
	ModuleName        = "Health Management"
	ModuleDescription = "Aggregates module health checks and serves /healthz."
	ModuleVersion     = "0.0.0"
)

// Module is the OSS health_management module. Pro embeds *Module and adds
// Pro-only fields/methods.
type Module struct {
	metadata  pkmodule.Metadata
	registry  health.Registrar
	svc       *service
	registrar *registrarAdapter
	handler   *Handler
	admin     portslib.AdminRegistrar
}

// NewModule constructs a health module. Returns an error for parity with
// sibling modules (user/audit/auth) — admin registration can fail when a
// real admin shell rejects a duplicate registration, and silently swallowing
// that previously masked configuration mistakes.
func NewModule(opts ...Option) (*Module, error) {
	cfg := config{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.registry == nil {
		cfg.registry = health.NewRegistry()
	}
	if cfg.admin == nil {
		cfg.admin = portslib.NoopAdminRegistrar()
	}

	m := &Module{
		metadata: pkmodule.Metadata{
			ID:          ModuleID,
			Name:        ModuleName,
			Description: ModuleDescription,
			Version:     ModuleVersion,
		},
		registry: cfg.registry,
		admin:    cfg.admin,
	}
	m.svc = newService(cfg.registry)
	m.registrar = &registrarAdapter{registry: cfg.registry}
	m.handler = NewHandler(cfg.registry.HTTPHandler())

	if err := registerAdmin(cfg.admin, m.svc); err != nil {
		return nil, fmt.Errorf("health: register admin: %w", err)
	}

	return m, nil
}

// MustNewModule mirrors the panic-on-error variant used by user/audit. It is
// the preferred entry point for catalog literals that cannot accept an error
// return.
func MustNewModule(opts ...Option) *Module {
	m, err := NewModule(opts...)
	if err != nil {
		panic(err)
	}
	return m
}

// Compose returns the module.Composable representation that the catalog
// consumes when validating port wiring.
func (m *Module) Compose() pkmodule.Composable {
	return pkmodule.Must(m.metadata,
		pkmodule.WithProvides(
			pkmodule.Provide[portslib.HealthRegistrar](ModuleVersion),
			pkmodule.Provide[HealthService](ModuleVersion),
		),
		pkmodule.WithDependencies(
			pkmodule.Optional[portslib.AdminRegistrar](pkmodule.DependencySpec{
				Version:           "0.0.0",
				Purpose:           "Mount the health admin page.",
				Category:          pkmodule.DependencyCategoryUI,
				SubCategory:       "admin",
				PreferredProvider: "admin_management",
			}),
		),
	)
}

// Registrar returns this module's portslib.HealthRegistrar implementation so
// other modules can wire it via WithHealthRegistrar.
func (m *Module) Registrar() portslib.HealthRegistrar { return m.registrar }

// Service returns the HealthService for callers that want to evaluate the
// aggregate report directly.
func (m *Module) Service() HealthService { return m.svc }

// HTTPHandler returns the wired HTTP handler so the host application can
// mount it on its router of choice.
func (m *Module) HTTPHandler() *Handler { return m.handler }

// Registry returns the underlying pk-core health.Registrar so Pro can
// register checks with custom timeouts.
func (m *Module) Registry() health.Registrar { return m.registry }
