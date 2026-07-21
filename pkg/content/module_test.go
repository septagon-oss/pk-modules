package content_test

// Validates: REQ-CONTENT-001.
// Per: ADR-0017.
// Discipline: C-14.
// module_test.go validates the content_management module against its public
// API. Tests live in content_test to ensure the OSS contract is exercised
// the way callers see it.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	pkmodule "github.com/septagon-oss/pk-core/pkg/module"

	"github.com/septagon-oss/pk-modules/pkg/content"

	_ "modernc.org/sqlite"
)

func sqliteDSN(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return "file:" + filepath.Join(dir, "content.db") + "?_pragma=journal_mode(WAL)"
}

func newModule(t *testing.T) *content.Module {
	t.Helper()
	m, err := content.NewModule(content.WithSQLiteDSN(sqliteDSN(t)))
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}
	return m
}

func TestNewModuleDefaults(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	if m.Service() == nil {
		t.Fatalf("Service() is nil")
	}
	if m.Reader() == nil {
		t.Fatalf("Reader() is nil")
	}
	if m.Publisher() == nil {
		t.Fatalf("Publisher() is nil")
	}
	if m.HTTPHandler() == nil {
		t.Fatalf("HTTPHandler() is nil")
	}
	if m.Store() == nil {
		t.Fatalf("Store() is nil")
	}
}

func TestCreateAndGet(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	in := &content.Content{
		TenantID: "t-1",
		Kind:     content.KindPost,
		Slug:     "hello-world",
		Title:    "Hello, world",
		Body:     "# hi",
		AuthorID: "user-1",
	}
	if err := m.Service().Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if in.ID == "" {
		t.Fatalf("Create did not assign ID")
	}
	if in.BodyFormat != content.BodyFormatMarkdown {
		t.Fatalf("BodyFormat default = %q, want markdown", in.BodyFormat)
	}
	got, err := m.Service().Get(ctx, "t-1", in.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != in.Title {
		t.Fatalf("Get title = %q, want %q", got.Title, in.Title)
	}
}

func TestGetBySlug(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	in := &content.Content{
		TenantID: "t-1",
		Kind:     content.KindPage,
		Slug:     "about",
		Title:    "About",
		Body:     "About us",
		AuthorID: "user-1",
	}
	if err := m.Service().Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := m.Reader().GetBySlug(ctx, "t-1", content.KindPage, "about")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if got.ID != in.ID {
		t.Fatalf("GetBySlug ID = %q, want %q", got.ID, in.ID)
	}
}

func TestListByTenantAndKind(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()

	rows := []struct {
		kind string
		slug string
	}{
		{content.KindPost, "post-a"},
		{content.KindPost, "post-b"},
		{content.KindPage, "about"},
	}
	for _, r := range rows {
		c := &content.Content{
			TenantID: "t-1",
			Kind:     r.kind,
			Slug:     r.slug,
			Title:    r.slug,
			Body:     r.slug,
			AuthorID: "u",
		}
		if err := m.Service().Create(ctx, c); err != nil {
			t.Fatalf("Create %q: %v", r.slug, err)
		}
	}
	posts, err := m.Service().List(ctx, "t-1", content.KindPost, 0, 0)
	if err != nil {
		t.Fatalf("List posts: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("len(posts) = %d, want 2", len(posts))
	}
	pages, err := m.Service().List(ctx, "t-1", content.KindPage, 0, 0)
	if err != nil {
		t.Fatalf("List pages: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("len(pages) = %d, want 1", len(pages))
	}
	all, err := m.Service().List(ctx, "t-1", "", 0, 0)
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("len(all) = %d, want 3", len(all))
	}
}

func TestUpdatePreservesCreatedAt(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	in := &content.Content{
		TenantID: "t-1",
		Kind:     content.KindPage,
		Slug:     "policy",
		Title:    "Policy",
		Body:     "v1",
		AuthorID: "u",
	}
	if err := m.Service().Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}
	createdAt := in.CreatedAt

	// Force a measurable gap between Create and Update.
	time.Sleep(2 * time.Millisecond)

	in.Title = "Updated policy"
	in.Body = "v2"
	if err := m.Service().Update(ctx, in); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := m.Service().Get(ctx, "t-1", in.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt changed: got %v, want %v", got.CreatedAt, createdAt)
	}
	if !got.UpdatedAt.After(createdAt) {
		t.Fatalf("UpdatedAt %v should be after CreatedAt %v", got.UpdatedAt, createdAt)
	}
	if got.Title != "Updated policy" {
		t.Fatalf("Title = %q, want Updated policy", got.Title)
	}
}

func TestPublishSetsPublishedAt(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	in := &content.Content{
		TenantID: "t-1",
		Kind:     content.KindPost,
		Slug:     "launch",
		Title:    "Launch",
		Body:     "🚀",
		AuthorID: "u",
	}
	if err := m.Service().Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if in.PublishedAt != nil {
		t.Fatalf("PublishedAt should default to nil for drafts")
	}
	if err := m.Publisher().Publish(ctx, "t-1", in.ID); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	got, err := m.Service().Get(ctx, "t-1", in.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PublishedAt == nil {
		t.Fatalf("PublishedAt nil after Publish")
	}
}

func TestUnpublishClearsPublishedAt(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	in := &content.Content{
		TenantID: "t-1",
		Kind:     content.KindPost,
		Slug:     "retract",
		Title:    "Retract",
		Body:     "draft",
		AuthorID: "u",
	}
	if err := m.Service().Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.Publisher().Publish(ctx, "t-1", in.ID); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if err := m.Publisher().Unpublish(ctx, "t-1", in.ID); err != nil {
		t.Fatalf("Unpublish: %v", err)
	}
	got, err := m.Service().Get(ctx, "t-1", in.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PublishedAt != nil {
		t.Fatalf("PublishedAt = %v, want nil after Unpublish", got.PublishedAt)
	}
}

func TestDuplicateSlugWithinTenantKindRejected(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	a := &content.Content{
		TenantID: "t-1",
		Kind:     content.KindPost,
		Slug:     "dup",
		Title:    "first",
		Body:     "x",
		AuthorID: "u",
	}
	if err := m.Service().Create(ctx, a); err != nil {
		t.Fatalf("Create a: %v", err)
	}
	b := &content.Content{
		TenantID: "t-1",
		Kind:     content.KindPost,
		Slug:     "dup",
		Title:    "second",
		Body:     "y",
		AuthorID: "u",
	}
	if err := m.Service().Create(ctx, b); err == nil {
		t.Fatalf("duplicate slug should error")
	}
}

func TestDifferentTenantsAllowSameSlug(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	for _, tenant := range []string{"t-a", "t-b"} {
		c := &content.Content{
			TenantID: tenant,
			Kind:     content.KindPost,
			Slug:     "share",
			Title:    "shared",
			Body:     tenant,
			AuthorID: "u",
		}
		if err := m.Service().Create(ctx, c); err != nil {
			t.Fatalf("Create %q: %v", tenant, err)
		}
	}
}

func TestCompose(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	c := m.Compose()
	if c == nil {
		t.Fatalf("Compose() returned nil")
	}
	if c.Metadata().ID != content.ModuleID {
		t.Fatalf("metadata ID = %q, want %q", c.Metadata().ID, content.ModuleID)
	}
	if len(c.Provides()) != 3 {
		t.Fatalf("provides len = %d, want 3", len(c.Provides()))
	}
	deps := c.Dependencies()
	if len(deps) != 4 {
		t.Fatalf("dependencies len = %d, want 4", len(deps))
	}
	for _, dep := range deps {
		if dep.Required {
			t.Fatalf("dep %s should be optional", dep.Port.Name)
		}
	}

	catalog := pkmodule.NewCatalog().
		Add(pkmodule.NewBundle("content-bundle", []pkmodule.Entry{
			{ID: content.ModuleID, New: func() pkmodule.Composable { return c }},
		}, []string{content.ModuleID})).
		MustBuild()
	if _, err := pkmodule.Compose(catalog); err != nil {
		t.Fatalf("Compose catalog: %v", err)
	}
}
