package health_test

// extension_example_test.go demonstrates how a Pro module embeds *Module and
// extends the OSS surface without changing it. The compile-time embed plus
// the runtime check that Registrar() works through the outer type are the
// guarantees the OSS contract makes to downstream module authors.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"testing"

	corehealth "github.com/septagon-oss/pk-core/pkg/observability/health"

	"github.com/septagon-oss/pk-modules/pkg/health"
)

// ProModule is what pk-pro would build on top of the OSS health module.
type ProModule struct {
	*health.Module
	SLOEnabled bool
}

// NewProModule wires the OSS module and adds Pro-only state.
func NewProModule(opts ...health.Option) *ProModule {
	return &ProModule{Module: health.NewModule(opts...), SLOEnabled: true}
}

func TestProEmbeddingCompiles(t *testing.T) {
	t.Parallel()

	pro := NewProModule()
	if !pro.SLOEnabled {
		t.Fatalf("Pro-only field SLOEnabled should be true")
	}

	// The embedded Module's Registrar() and Service() work through the outer
	// type without any Pro-side plumbing.
	if err := pro.Registrar().Register("storage", corehealth.CheckerFunc(func(context.Context) error { return nil })); err != nil {
		t.Fatalf("Register: %v", err)
	}
	res := pro.Service().Check(context.Background())
	if res.Status != health.StatusHealthy {
		t.Fatalf("Status = %q, want healthy", res.Status)
	}
}
