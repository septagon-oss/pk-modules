package admin

// module.go owns the admin_management Module struct and its NewModule
// constructor. The module implements portslib.AdminRegistrar via the embedded
// Shell and exposes an HTTPHandler suitable for mounting on the host router
// at the configured BasePath.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0017 (composition
// through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"net/http"

	pkmodule "github.com/septagon-oss/pk-core/pkg/module"
	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

// Module metadata constants used by the catalog and sibling modules that
// reference the canonical provider of portslib.AdminRegistrar.
const (
	ModuleID          = "admin_management"
	ModuleName        = "Admin Management"
	ModuleDescription = "Pluggable admin shell that hosts the registered pages of other modules."
	ModuleVersion     = "0.0.0"
)

// Module is the admin_management plugin. It implements portslib.AdminRegistrar
// through the embedded Shell and exposes an http.Handler that renders the
// admin shell.
type Module struct {
	metadata pkmodule.Metadata
	shell    *Shell
	title    string
	basePath string
	health   portslib.HealthRegistrar
}

// NewModule constructs the admin module.
func NewModule(opts ...Option) (*Module, error) {
	cfg := &config{
		title:    defaultTitle,
		basePath: defaultBasePath,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	if cfg.health == nil {
		cfg.health = portslib.NoopHealthRegistrar()
	}

	shell := NewShell(ShellOptions{
		Title:    cfg.title,
		BasePath: cfg.basePath,
	})

	m := &Module{
		metadata: pkmodule.Metadata{
			ID:          ModuleID,
			Name:        ModuleName,
			Description: ModuleDescription,
			Version:     ModuleVersion,
		},
		shell:    shell,
		title:    shell.Title(),
		basePath: shell.BasePath(),
		health:   cfg.health,
	}
	return m, nil
}

// MustNewModule is the panic-on-error variant for catalog literals.
//
// Panics if NewModule returns an error (for example, when a required option
// is missing). Use NewModule when an error return is preferred.
func MustNewModule(opts ...Option) *Module {
	m, err := NewModule(opts...)
	if err != nil {
		panic(err)
	}
	return m
}

// Compose returns the module.Composable for catalog assembly.
func (m *Module) Compose() pkmodule.Composable {
	return pkmodule.Must(
		m.metadata,
		pkmodule.WithProvides(
			pkmodule.Provide[portslib.AdminRegistrar](ModuleVersion),
		),
	)
}

// Metadata returns the module metadata.
func (m *Module) Metadata() pkmodule.Metadata { return m.metadata }

// Registrar returns the portslib.AdminRegistrar implementation (the Shell).
// Other modules consume this to register their pages.
func (m *Module) Registrar() portslib.AdminRegistrar { return m.shell }

// HTTPHandler returns the mountable http.Handler for the admin shell. Mount
// it at m.BasePath() (default "/admin").
func (m *Module) HTTPHandler() http.Handler { return m.shell }

// Title returns the configured admin shell title.
func (m *Module) Title() string { return m.title }

// BasePath returns the configured URL prefix for the admin shell.
func (m *Module) BasePath() string { return m.basePath }

// Shell exposes the underlying Shell for advanced Pro use (custom rendering,
// introspection of registered modules, etc.). Most callers should not need
// this — the Registrar() and HTTPHandler() methods cover the standard surface.
func (m *Module) Shell() *Shell { return m.shell }
