package apikey_test

// extension_example_test.go demonstrates how a Pro module embeds *Module
// and extends the OSS surface without changing it. The compile-time embed
// plus the runtime check that Service() works through the outer type are
// the guarantees the OSS contract makes to downstream module authors.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/septagon-oss/pk-modules/pkg/apikey"

	_ "modernc.org/sqlite"
)

// ProAPIKeyModule is what pk-pro would build on top of the OSS module.
// The embed gives it Service(), HTTPHandler(), Compose(), Migrations(),
// Authenticator(), and every future OSS surface for free.
type ProAPIKeyModule struct {
	*apikey.Module
	Rotation bool
}

// NewProAPIKeyModule wires the OSS module and adds Pro-only state.
func NewProAPIKeyModule(opts ...apikey.Option) (*ProAPIKeyModule, error) {
	m, err := apikey.NewModule(opts...)
	if err != nil {
		return nil, err
	}
	return &ProAPIKeyModule{Module: m, Rotation: true}, nil
}

func TestProEmbeddingCompiles(t *testing.T) {
	t.Parallel()
	dsn := "file:" + filepath.Join(t.TempDir(), "apikey-pro.db")
	pro, err := NewProAPIKeyModule(
		apikey.WithSQLiteDSN(dsn),
		apikey.WithHasher(fastHasher(t)),
	)
	if err != nil {
		t.Fatalf("NewProAPIKeyModule: %v", err)
	}
	if !pro.Rotation {
		t.Fatalf("Pro-only field Rotation should be true")
	}

	ctx := context.Background()
	plaintext, key, err := pro.Service().Issue(ctx, "t-pro", "u-pro", "ci", []string{"read"}, 0)
	if err != nil {
		t.Fatalf("pro.Service().Issue: %v", err)
	}
	if plaintext == "" || key == nil {
		t.Fatalf("Issue returned empty result")
	}
	got, err := pro.Service().Verify(ctx, plaintext)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.ID != key.ID {
		t.Fatalf("Verify returned wrong key: %+v vs %+v", got, key)
	}
}
