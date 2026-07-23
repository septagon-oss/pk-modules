// sqlite_test.go exercises the tenant_management sqlite store against a real
// modernc.org/sqlite database opened on a per-test temp file. Tests cover the
// full CRUD round-trip, slug lookup, listing order, not-found behavior, and the
// slug uniqueness constraint.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package sqlite_test

// Validates: REQ-TENANT-001.
// Per: ADR-0017.
// Discipline: C-14.
import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/septagon-oss/pk-modules/pkg/tenant/store"
	"github.com/septagon-oss/pk-modules/pkg/tenant/store/sqlite"

	_ "modernc.org/sqlite"
)

func newStore(t *testing.T) *sqlite.Store {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "tenant.db") + "?_pragma=journal_mode(WAL)"
	s, err := sqlite.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.DB().Close() })
	return s
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

	in := &store.Tenant{ID: "t1", Slug: "example-org", Name: "Example Organization"}
	if err := s.Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if in.CreatedAt.IsZero() || in.UpdatedAt.IsZero() {
		t.Fatal("Create should default timestamps")
	}

	got, err := s.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Slug != "example-org" || got.Name != "Example Organization" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	got.Name = "Renamed Organization"
	got.Slug = "renamed-org"
	if err := s.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	after, err := s.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if after.Name != "Renamed Organization" || after.Slug != "renamed-org" {
		t.Fatalf("Update not persisted: %+v", after)
	}

	if err := s.Delete(ctx, "t1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, "t1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get after delete err = %v, want ErrNotFound", err)
	}
}

func TestGetBySlug(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	if err := s.Create(ctx, &store.Tenant{ID: "t1", Slug: "globex", Name: "Globex"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.GetBySlug(ctx, "globex")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if got.ID != "t1" {
		t.Fatalf("GetBySlug ID = %q, want t1", got.ID)
	}
	if _, err := s.GetBySlug(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetBySlug(nope) err = %v, want ErrNotFound", err)
	}
}

func TestGetNotFound(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if _, err := s.Get(context.Background(), "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get(missing) err = %v, want ErrNotFound", err)
	}
}

func TestListOrderedBySlug(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	for _, tn := range []*store.Tenant{
		{ID: "2", Slug: "charlie", Name: "C"},
		{ID: "1", Slug: "alpha", Name: "A"},
		{ID: "3", Slug: "bravo", Name: "B"},
	} {
		if err := s.Create(ctx, tn); err != nil {
			t.Fatalf("Create %s: %v", tn.ID, err)
		}
	}
	got, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"alpha", "bravo", "charlie"}
	if len(got) != len(want) {
		t.Fatalf("List returned %d, want %d", len(got), len(want))
	}
	for i, slug := range want {
		if got[i].Slug != slug {
			t.Fatalf("List order[%d] = %s, want %s", i, got[i].Slug, slug)
		}
	}
}

func TestCreateDuplicateSlug(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	if err := s.Create(ctx, &store.Tenant{ID: "a", Slug: "dup", Name: "A"}); err != nil {
		t.Fatalf("Create #1: %v", err)
	}
	err := s.Create(ctx, &store.Tenant{ID: "b", Slug: "dup", Name: "B"})
	if !errors.Is(err, store.ErrDuplicateSlug) {
		t.Fatalf("Create duplicate slug err = %v, want ErrDuplicateSlug", err)
	}
}

func TestUpdateDuplicateSlug(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	if err := s.Create(ctx, &store.Tenant{ID: "a", Slug: "alpha", Name: "A"}); err != nil {
		t.Fatalf("Create a: %v", err)
	}
	if err := s.Create(ctx, &store.Tenant{ID: "b", Slug: "bravo", Name: "B"}); err != nil {
		t.Fatalf("Create b: %v", err)
	}
	// Renaming b's slug to alpha must trip the unique constraint.
	err := s.Update(ctx, &store.Tenant{ID: "b", Slug: "alpha", Name: "B"})
	if !errors.Is(err, store.ErrDuplicateSlug) {
		t.Fatalf("Update to duplicate slug err = %v, want ErrDuplicateSlug", err)
	}
}

func TestUpdateNotFound(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	err := s.Update(context.Background(), &store.Tenant{ID: "ghost", Slug: "ghost", Name: "G"})
	if !errors.Is(err, store.ErrNotFound) {
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

func TestCreateNil(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.Create(context.Background(), nil); err == nil {
		t.Fatal("Create(nil) should return an error")
	}
}
