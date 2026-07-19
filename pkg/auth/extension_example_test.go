// Validates: REQ-AUTH-001.
// Per: ADR-0017.
// Discipline: C-14.

package auth_test

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

	"github.com/septagon-oss/pk-modules/pkg/auth"

	_ "modernc.org/sqlite"
)

// ProAuthModule is what pk-pro would build on top of the OSS module. The
// embed gives it Service(), HTTPHandler(), Compose(), Migrations(), and
// every future OSS surface for free, while leaving the Pro-only fields and
// methods uncontaminated.
type ProAuthModule struct {
	*auth.Module
	MFA bool
}

// NewProAuthModule wires the OSS module and adds Pro-only state.
func NewProAuthModule(opts ...auth.Option) (*ProAuthModule, error) {
	m, err := auth.NewModule(opts...)
	if err != nil {
		return nil, err
	}
	return &ProAuthModule{Module: m, MFA: true}, nil
}

func TestProEmbeddingCompiles(t *testing.T) {
	t.Parallel()

	reader := newFakeUserReader()
	hasher := newHasher(t)
	reader.add(makeUserWithPassword(t, hasher, "t-pro", "u-pro", "pro@example.test", "supersecret", true))

	dsn := "file:" + filepath.Join(t.TempDir(), "auth-pro.db")
	pro, err := NewProAuthModule(
		auth.WithSQLiteDSN(dsn),
		auth.WithUserReader(reader),
		auth.WithHasher(hasher),
		auth.WithLoginPolicy(&recordingPolicy{}),
	)
	if err != nil {
		t.Fatalf("NewProAuthModule: %v", err)
	}
	if !pro.MFA {
		t.Fatalf("Pro-only field MFA should be true")
	}

	ctx := context.Background()
	sess, err := pro.Service().Login(ctx, "t-pro", auth.Credentials{
		Email:    "pro@example.test",
		Password: "supersecret",
	})
	if err != nil {
		t.Fatalf("pro.Service().Login: %v", err)
	}
	if sess.UserID != "u-pro" {
		t.Fatalf("Login returned wrong user_id: %q", sess.UserID)
	}
}
