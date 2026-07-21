package content_test

// Validates: REQ-CONTENT-001.
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

	"github.com/septagon-oss/pk-modules/pkg/content"

	_ "modernc.org/sqlite"
)

// ProModule is what pk-pro would build on top of the OSS content module. The
// embed gives it Service(), Reader(), Publisher(), HTTPHandler(), Compose(),
// Migrations(), and every future OSS surface for free.
type ProModule struct {
	*content.Module
	ScheduledPublishing bool
}

// NewProModule wires the OSS module and adds Pro-only state.
func NewProModule(opts ...content.Option) (*ProModule, error) {
	m, err := content.NewModule(opts...)
	if err != nil {
		return nil, err
	}
	return &ProModule{Module: m, ScheduledPublishing: true}, nil
}

func TestProEmbeddingCompiles(t *testing.T) {
	t.Parallel()

	dsn := "file:" + filepath.Join(t.TempDir(), "pro.db")
	pro, err := NewProModule(content.WithSQLiteDSN(dsn))
	if err != nil {
		t.Fatalf("NewProModule: %v", err)
	}
	if !pro.ScheduledPublishing {
		t.Fatalf("Pro-only field ScheduledPublishing should be true")
	}

	ctx := context.Background()
	in := &content.Content{
		TenantID: "t-pro",
		Kind:     content.KindPost,
		Slug:     "pro-launch",
		Title:    "Pro launch",
		Body:     "hi pro",
		AuthorID: "u",
	}
	if err := pro.Service().Create(ctx, in); err != nil {
		t.Fatalf("pro.Service().Create: %v", err)
	}
	if err := pro.Publisher().Publish(ctx, "t-pro", in.ID); err != nil {
		t.Fatalf("pro.Publisher().Publish: %v", err)
	}
	got, err := pro.Reader().GetBySlug(ctx, "t-pro", content.KindPost, "pro-launch")
	if err != nil {
		t.Fatalf("pro.Reader().GetBySlug: %v", err)
	}
	if got.PublishedAt == nil {
		t.Fatalf("Pro published content lacks PublishedAt")
	}
}
