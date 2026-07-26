// postgres_test.go runs the content_management Postgres adapter through the
// same shared store conformance suite the sqlite adapter passes, plus a CRUD
// round-trip, against a real Postgres. It is gated on PK_POSTGRES_TEST_DSN: with
// no DSN the tests skip, so `go test ./...` stays green on a machine without
// Postgres, and CI (or a developer with a container) sets the DSN to enforce
// the contract. The point of the pivot: the production adapter is held to the
// identical tenant-isolation guarantees as the embedded one, by executable
// check rather than by faith.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package postgres_test

// Validates: REQ-CONTENT-001, REQ-PORTS-001.
// Per: ADR-0009.
// Discipline: C-14.
import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/septagon-oss/pk-modules/pkg/content/store"
	"github.com/septagon-oss/pk-modules/pkg/content/store/postgres"
	"github.com/septagon-oss/pk-modules/pkg/contracttest"
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
	if _, err := db.Exec(`TRUNCATE TABLE content`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return s
}

func content(id, slug string) *store.Content {
	return &store.Content{
		ID: id, TenantID: "tenant-x", Kind: "post", Slug: slug,
		Title: "Title " + id, Body: "body", BodyFormat: "markdown", AuthorID: "author-1",
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestPostgresStoreConformance(t *testing.T) {
	s := newStore(t)
	seq := 0
	create := func(ctx context.Context, tenantID string) (string, error) {
		seq++
		id := "conf-" + itoa(seq)
		c := content(id, "conf-slug-"+itoa(seq))
		c.TenantID = tenantID
		return id, s.Create(ctx, c)
	}

	t.Run("list is tenant scoped", func(t *testing.T) {
		contracttest.AssertListTenantScoped(t, contracttest.ListScopedStore{
			Create: create,
			List: func(ctx context.Context, tenantID string) ([]string, error) {
				rows, err := s.List(ctx, tenantID, "", 100, 0)
				out := make([]string, 0, len(rows))
				for _, r := range rows {
					out = append(out, r.ID)
				}
				return out, err
			},
		})
	})

	t.Run("update cannot reassign tenant", func(t *testing.T) {
		contracttest.AssertUpdateCannotReassignTenant(t, contracttest.TenantImmutableStore{
			Create: create,
			Update: func(ctx context.Context, tenantID, id string) error {
				existing, err := s.Get(ctx, tenantID, id)
				if err != nil {
					return err
				}
				existing.Title = "ordinary update"
				return s.Update(ctx, existing)
			},
			UpdateReassigning: func(ctx context.Context, tenantID, id, newTenantID string) error {
				existing, err := s.Get(ctx, tenantID, id)
				if err != nil {
					return err
				}
				existing.TenantID = newTenantID
				existing.Title = "reassignment attempt"
				return s.Update(ctx, existing)
			},
			Get: func(ctx context.Context, tenantID, id string) error {
				_, err := s.Get(ctx, tenantID, id)
				return err
			},
			NotFound: store.ErrNotFound,
		})
	})
}

func TestPostgresStoreCRUD(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	c := content("c1", "hello")
	if err := s.Create(ctx, c); err != nil {
		t.Fatalf("create: %v", err)
	}

	// A duplicate (tenant, kind, slug) is a slug conflict, not a generic error.
	if err := s.Create(ctx, content("c2", "hello")); err != store.ErrSlugTaken {
		t.Fatalf("duplicate slug = %v, want ErrSlugTaken", err)
	}
	// A duplicate primary key is distinguished from a slug conflict.
	if err := s.Create(ctx, content("c1", "other")); err != store.ErrDuplicate {
		t.Fatalf("duplicate id = %v, want ErrDuplicate", err)
	}

	// Get is tenant-scoped: the wrong tenant sees nothing.
	if _, err := s.Get(ctx, "tenant-y", "c1"); err != store.ErrNotFound {
		t.Fatalf("cross-tenant Get = %v, want ErrNotFound", err)
	}
	got, err := s.Get(ctx, "tenant-x", "c1")
	if err != nil || got.Title != "Title c1" {
		t.Fatalf("Get = %v, %v", got, err)
	}

	// Publish, then confirm it round-trips and List sees it.
	now := time.Now().UTC()
	if err := s.SetPublished(ctx, "tenant-x", "c1", &now); err != nil {
		t.Fatalf("publish: %v", err)
	}
	got, _ = s.Get(ctx, "tenant-x", "c1")
	if got.PublishedAt == nil {
		t.Fatal("published_at not persisted")
	}
	rows, err := s.List(ctx, "tenant-x", "post", 10, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("List = %d rows, %v; want 1", len(rows), err)
	}

	// Delete is tenant-scoped and idempotent-on-absence (ErrNotFound).
	if err := s.Delete(ctx, "tenant-y", "c1"); err != store.ErrNotFound {
		t.Fatalf("cross-tenant Delete = %v, want ErrNotFound", err)
	}
	if err := s.Delete(ctx, "tenant-x", "c1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, "tenant-x", "c1"); err != store.ErrNotFound {
		t.Fatalf("Get after delete = %v, want ErrNotFound", err)
	}
}
