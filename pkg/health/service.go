package health

// service.go owns the default HealthService implementation and the
// portslib.HealthRegistrar adapter. The service is a thin shim over the
// pk-core health.Registrar primitive; the adapter narrows pk-core's
// variadic-option Register signature down to the portslib contract.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"errors"
	"strings"

	"github.com/septagon-oss/pk-core/pkg/observability/health"
)

// service is the default HealthService implementation. Pro can embed
// *service to add SLO scoring and tenant-scoped probes.
type service struct {
	registry health.Registrar
}

func newService(r health.Registrar) *service { return &service{registry: r} }

// Check evaluates every registered checker and returns the aggregate report.
func (s *service) Check(ctx context.Context) Result {
	return s.registry.Check(ctx)
}

// registrarAdapter narrows pk-core's variadic-option Register signature to
// the portslib contract. The adapter rejects empty names and nil checkers so
// callers see a clean error before the underlying pk-core registry panics.
type registrarAdapter struct {
	registry health.Registrar
}

// Register implements portslib.HealthRegistrar.
func (a *registrarAdapter) Register(name string, check health.Checker) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("health: name must not be empty")
	}
	if check == nil {
		return errors.New("health: checker must not be nil")
	}
	a.registry.Register(name, check)
	return nil
}
