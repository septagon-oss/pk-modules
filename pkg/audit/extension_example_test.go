package audit_test

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

	"github.com/septagon-oss/pk-modules/pkg/audit"

	_ "modernc.org/sqlite"
)

// ProModule is what pk-pro would build on top of the OSS audit module. The
// embed gives it Service(), Reader(), HTTPHandler(), Compose(), Migrations(),
// and every future OSS surface for free.
type ProModule struct {
	*audit.Module
	RetentionDays int
}

// NewProModule wires the OSS module and adds Pro-only state.
func NewProModule(opts ...audit.Option) (*ProModule, error) {
	m, err := audit.NewModule(opts...)
	if err != nil {
		return nil, err
	}
	return &ProModule{Module: m, RetentionDays: 365}, nil
}

func TestProEmbeddingCompiles(t *testing.T) {
	t.Parallel()

	dsn := "file:" + filepath.Join(t.TempDir(), "pro.db")
	pro, err := NewProModule(audit.WithSQLiteDSN(dsn))
	if err != nil {
		t.Fatalf("NewProModule: %v", err)
	}
	if pro.RetentionDays != 365 {
		t.Fatalf("Pro-only field RetentionDays should be 365")
	}

	ctx := context.Background()
	in := &audit.Event{TenantID: "t-pro", Actor: "x", Action: "user.created"}
	if err := pro.Service().Record(ctx, in); err != nil {
		t.Fatalf("pro.Service().Record: %v", err)
	}
	list, err := pro.Reader().Query(ctx, audit.QueryFilter{TenantID: "t-pro"})
	if err != nil {
		t.Fatalf("pro.Reader().Query: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("Query len = %d, want 1", len(list))
	}
}
