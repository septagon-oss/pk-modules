package notification_test

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

	"github.com/septagon-oss/pk-modules/pkg/notification"
	"github.com/septagon-oss/pk-modules/pkg/portslib"

	_ "modernc.org/sqlite"
)

// ProModule is what pk-pro would build on top of the OSS notification
// module. The embed gives it Service(), HTTPHandler(), Channels(), Compose(),
// Migrations(), and every future OSS surface for free.
type ProModule struct {
	*notification.Module
	DigestEnabled bool
}

// NewProModule wires the OSS module and adds Pro-only state.
func NewProModule(opts ...notification.Option) (*ProModule, error) {
	m, err := notification.NewModule(opts...)
	if err != nil {
		return nil, err
	}
	return &ProModule{Module: m, DigestEnabled: true}, nil
}

func TestProEmbeddingCompiles(t *testing.T) {
	t.Parallel()

	dsn := "file:" + filepath.Join(t.TempDir(), "pro.db")
	pro, err := NewProModule(notification.WithSQLiteDSN(dsn))
	if err != nil {
		t.Fatalf("NewProModule: %v", err)
	}
	if !pro.DigestEnabled {
		t.Fatalf("Pro-only field DigestEnabled should be true")
	}

	ctx := context.Background()
	n := &portslib.Notification{
		TenantID: "t-pro",
		UserID:   "u-pro",
		Title:    "Pro welcome",
		Body:     "hello pro",
	}
	if err := pro.Service().Create(ctx, n); err != nil {
		t.Fatalf("pro.Service().Create: %v", err)
	}
	got, err := pro.Service().GetByUser(ctx, "t-pro", "u-pro", 0, 0)
	if err != nil {
		t.Fatalf("pro.Service().GetByUser: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
}
