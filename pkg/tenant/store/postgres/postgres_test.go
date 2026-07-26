// postgres_test.go exercises the tenant_management Postgres adapter against a
// real Postgres, mirroring the coverage the sqlite adapter's sqlite_test.go runs:
// the full CRUD round-trip, slug lookup, listing order, not-found behavior, and
// the slug uniqueness constraint on both Create and Update. It is gated on
// PK_POSTGRES_TEST_DSN: with no DSN every test skips, so `go test ./...` stays
// green on a machine without Postgres, and CI (or a developer with a container)
// sets the DSN to enforce the contract. The point of the pivot: the production
// adapter is held to the identical guarantees as the embedded one, by executable
// check rather than by faith.
//
// The tenant module has no shared conformance suite to wire (its Store has no
// tenant_id column — a tenant is itself the isolation boundary — so
// AssertListTenantScoped and AssertUpdateCannotReassignTenant do not apply, and
// there is no soft-delete/lifecycle for AssertRetiredHiddenFromList). The checks
// below therefore mirror the sqlite adapter's own unit tests.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package postgres_test

// Validates: REQ-TENANT-001, REQ-PORTS-001.
// Per: ADR-0009.
// Discipline: C-14.
import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/septagon-oss/pk-modules/pkg/tenant/store"
	"github.com/septagon-oss/pk-modules/pkg/tenant/store/postgres"
)

// dsn returns the Postgres test DSN or skips the test. Example:
//
//	PK_POSTGRES_TEST_DSN='postgres://postgres:pk@127.0.0.1:55432/pktest?sslmode=disable'
func dsn(t *testing.T) string {
	t.Helper()
	v := os.Getenv("PK_POSTGRES_TEST_DSN")
	if v == "" {
		t.Skip("PK_POSTGRES_TEST_DSN not set; skipping Postgres conformance")
	}
	return v
}

// newStore opens the adapter and truncates the table so each test starts clean.
// The tests are serial (no t.Parallel) because they share one database.
func newStore(t *testing.T) *postgres.Store {
	t.Helper()
	db, err := sql.Open("pgx", dsn(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := postgres.New(db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if _, err := db.Exec(`TRUNCATE TABLE tenants`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return s
}

func TestPostgresTenantCRUD(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	// New and Create reject nil inputs.
	if _, err := postgres.New(nil); err == nil {
		t.Fatal("New(nil) should return an error")
	}
	if err := s.Create(ctx, nil); err == nil {
		t.Fatal("Create(nil) should return an error")
	}

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

func TestPostgresTenantGetBySlug(t *testing.T) {
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

func TestPostgresTenantGetNotFound(t *testing.T) {
	s := newStore(t)
	if _, err := s.Get(context.Background(), "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get(missing) err = %v, want ErrNotFound", err)
	}
}

func TestPostgresTenantListOrderedBySlug(t *testing.T) {
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

func TestPostgresTenantCreateDuplicateSlug(t *testing.T) {
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

func TestPostgresTenantUpdateDuplicateSlug(t *testing.T) {
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

func TestPostgresTenantUpdateNotFound(t *testing.T) {
	s := newStore(t)
	err := s.Update(context.Background(), &store.Tenant{ID: "ghost", Slug: "ghost", Name: "G"})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Update(ghost) err = %v, want ErrNotFound", err)
	}
}

func TestPostgresTenantDeleteNotFound(t *testing.T) {
	s := newStore(t)
	if err := s.Delete(context.Background(), "ghost"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Delete(ghost) err = %v, want ErrNotFound", err)
	}
}
