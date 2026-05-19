package health

// options.go owns functional options used by NewModule. New options should be
// purely additive; never change the meaning of an existing option.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"github.com/septagon-oss/pk-core/pkg/observability/health"

	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

// Option configures a Module at construction time.
type Option func(*config)

type config struct {
	registry health.Registrar
	admin    portslib.AdminRegistrar
}

// WithRegistry wires a caller-provided pk-core health Registrar. If omitted,
// the module constructs a fresh in-memory registry via health.NewRegistry().
func WithRegistry(r health.Registrar) Option {
	return func(c *config) { c.registry = r }
}

// WithAdminRegistrar wires the host application's admin shell.
func WithAdminRegistrar(r portslib.AdminRegistrar) Option {
	return func(c *config) { c.admin = r }
}
