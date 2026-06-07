// sqlite_test.go exercises the content_management sqlite store against a real
// modernc.org/sqlite database opened on a per-test temp file. Tests cover the
// full CRUD round-trip, slug lookup, tenant/kind-scoped listing, publish
// transitions, not-found behavior, and the (tenant, kind, slug) uniqueness
// constraint.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/septagon-oss/pk-modules/pkg/content/store"
	"github.com/septagon-oss/pk-modules/pkg/content/store/sqlite"

	_ "modernc.org/sqlite"
)

func newStore(t *testing.T) *sqlite.Store {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "content.db") + "?_pragma=journal_mode(WAL)"
	s, err := sqlite.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.DB().Close() })
	return s
}

func content(id, slug string) *store.Content {
	return &store.Content{
		ID:         id,
		TenantID:   "tenant-1",
		Kind:       "post",
		Slug:       slug,
		Title:      "Title " + slug,
		Body:       "Body",
		BodyFormat: "markdown",
		AuthorID:   "author-1",
	}
}

func TestNewRejectsNilDB(t *testing.T) {
	t.Parallel()
	if _, err := sqlite.New(nil); err == nil {
		t.Fatal("New(nil) should return an error")
	}
}

func TestCRUDRoundTrip(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()

	in := content("c1", "hello")
	if err := s.Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if in.CreatedAt.IsZero() || in.UpdatedAt.IsZero() {
		t.Fatal("Create should default CreatedAt/UpdatedAt")
	}

	got, err := s.Get(ctx, "c1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Slug != "hello" || got.Title != "Title hello" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	got.Title = "Updated"
	got.Slug = "hello-2"
	if err := s.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	after, err := s.Get(ctx, "c1")
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if after.Title != "Updated" || after.Slug != "hello-2" {
		t.Fatalf("Update not persisted: %+v", after)
	}
	if !after.CreatedAt.Equal(in.CreatedAt) {
		t.Fatal("Update should preserve CreatedAt")
	}

	if err := s.Delete(ctx, "c1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, "c1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get after delete err = %v, want ErrNotFound", err)
	}
}

func TestGetBySlug(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	in := content("c1", "findme")
	if err := s.Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.GetBySlug(ctx, "tenant-1", "post", "findme")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if got.ID != "c1" {
		t.Fatalf("GetBySlug ID = %q, want c1", got.ID)
	}
	if _, err := s.GetBySlug(ctx, "tenant-1", "post", "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetBySlug(nope) err = %v, want ErrNotFound", err)
	}
}

func TestCreateSlugTaken(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	if err := s.Create(ctx, content("a", "dup")); err != nil {
		t.Fatalf("Create #1: %v", err)
	}
	// Same (tenant, kind, slug) -> ErrSlugTaken.
	err := s.Create(ctx, content("b", "dup"))
	if !errors.Is(err, store.ErrSlugTaken) {
		t.Fatalf("Create duplicate slug err = %v, want ErrSlugTaken", err)
	}
	// Same slug, different kind is allowed.
	diff := content("c", "dup")
	diff.Kind = "page"
	if err := s.Create(ctx, diff); err != nil {
		t.Fatalf("Create same slug different kind should succeed: %v", err)
	}
}

func TestListScopedAndOrdered(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	older := content("older", "older")
	older.CreatedAt = base
	newer := content("newer", "newer")
	newer.CreatedAt = base.Add(time.Hour)
	page := content("page", "page")
	page.Kind = "page"
	other := content("other", "other")
	other.TenantID = "tenant-2"
	for _, c := range []*store.Content{older, newer, page, other} {
		if err := s.Create(ctx, c); err != nil {
			t.Fatalf("Create %s: %v", c.ID, err)
		}
	}

	all, err := s.List(ctx, "tenant-1", "", 0, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("List(tenant-1) returned %d, want 3", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].CreatedAt.Before(all[i].CreatedAt) {
			t.Fatalf("List not ordered created_at DESC: %v", []string{all[0].ID, all[1].ID, all[2].ID})
		}
	}

	posts, err := s.List(ctx, "tenant-1", "post", 0, 0)
	if err != nil {
		t.Fatalf("List(post): %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("List(post) returned %d, want 2", len(posts))
	}

	// Pagination: limit 1 then offset 1 should yield distinct rows.
	first, err := s.List(ctx, "tenant-1", "post", 1, 0)
	if err != nil {
		t.Fatalf("List page 1: %v", err)
	}
	second, err := s.List(ctx, "tenant-1", "post", 1, 1)
	if err != nil {
		t.Fatalf("List page 2: %v", err)
	}
	if len(first) != 1 || len(second) != 1 || first[0].ID == second[0].ID {
		t.Fatalf("pagination overlap: %v vs %v", first, second)
	}
}

func TestUpdateNotFound(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.Update(context.Background(), content("ghost", "ghost")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Update(ghost) err = %v, want ErrNotFound", err)
	}
}

func TestDeleteNotFound(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.Delete(context.Background(), "ghost"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Delete(ghost) err = %v, want ErrNotFound", err)
	}
}

func TestSetPublished(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	if err := s.Create(ctx, content("p", "publishable")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	at := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := s.SetPublished(ctx, "p", &at); err != nil {
		t.Fatalf("SetPublished: %v", err)
	}
	got, err := s.Get(ctx, "p")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PublishedAt == nil || !got.PublishedAt.Equal(at) {
		t.Fatalf("PublishedAt = %v, want %v", got.PublishedAt, at)
	}
	// Clearing publication (back to draft).
	if err := s.SetPublished(ctx, "p", nil); err != nil {
		t.Fatalf("SetPublished(nil): %v", err)
	}
	got, err = s.Get(ctx, "p")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PublishedAt != nil {
		t.Fatalf("PublishedAt should be nil after clearing: %v", got.PublishedAt)
	}
	// Unknown id.
	if err := s.SetPublished(ctx, "ghost", &at); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("SetPublished(ghost) err = %v, want ErrNotFound", err)
	}
}

func TestCreateNil(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.Create(context.Background(), nil); err == nil {
		t.Fatal("Create(nil) should return an error")
	}
}
