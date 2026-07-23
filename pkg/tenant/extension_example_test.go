package tenant_test

// Validates: REQ-TENANT-001.
// Per: ADR-0017.
// Discipline: C-14.
// extension_example_test.go demonstrates how a Pro module embeds *Module and
// extends the OSS surface without changing it. The compile-time embed plus
// the runtime check that Service() works through the outer type are the
// guarantees the OSS contract makes to downstream module authors.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/septagon-oss/pk-modules/pkg/tenant"

	_ "modernc.org/sqlite"
)

// ProModule is what pk-pro would build on top of the OSS module. The embed
// gives it Service(), HTTPHandler(), Compose(), Migrations(), and every
// future OSS surface for free, while leaving the Pro-only fields and methods
// uncontaminated.
type ProModule struct {
	*tenant.Module
	SSO bool
}

// NewProModule wires the OSS module and adds Pro-only state.
func NewProModule(opts ...tenant.Option) (*ProModule, error) {
	m, err := tenant.NewModule(opts...)
	if err != nil {
		return nil, err
	}
	return &ProModule{Module: m, SSO: true}, nil
}

func TestProEmbeddingCompiles(t *testing.T) {
	t.Parallel()

	dsn := "file:" + filepath.Join(t.TempDir(), "pro.db")
	pro, err := NewProModule(tenant.WithSQLiteDSN(dsn))
	if err != nil {
		t.Fatalf("NewProModule: %v", err)
	}
	if !pro.SSO {
		t.Fatalf("Pro-only field SSO should be true")
	}

	// The embedded Module's Service() works through the outer type without
	// any Pro-side plumbing. This is the chainability invariant for OSS.
	ctx := context.Background()
	in := &tenant.Tenant{Slug: "partner-org", Name: "Partner Organization"}
	if err := pro.Service().Create(ctx, in); err != nil {
		t.Fatalf("pro.Service().Create: %v", err)
	}
	got, err := pro.Service().Get(ctx, in.ID)
	if err != nil {
		t.Fatalf("pro.Service().Get: %v", err)
	}
	if got.Slug != "partner-org" {
		t.Fatalf("Get returned wrong slug: %q", got.Slug)
	}
}
