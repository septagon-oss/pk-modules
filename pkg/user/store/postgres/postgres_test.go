// postgres_test.go runs the user_management Postgres adapter through the same
// shared store conformance suite the sqlite adapter passes, plus a CRUD
// round-trip covering the tenant-scoped uniqueness and cross-tenant IDOR
// guards, against a real Postgres. It is gated on PK_POSTGRES_TEST_DSN: with no
// DSN the tests skip, so `go test ./...` stays green on a machine without
// Postgres, and CI (or a developer with a container) sets the DSN to enforce
// the contract. The point of the pivot: the production adapter is held to the
// identical tenant-isolation guarantees as the embedded one, by executable
// check rather than by faith.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package postgres_test

// Validates: REQ-USER-001, REQ-PORTS-001.
// Per: ADR-0009.
// Discipline: C-14.
import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/septagon-oss/pk-modules/pkg/contracttest"
	"github.com/septagon-oss/pk-modules/pkg/user/store"
	"github.com/septagon-oss/pk-modules/pkg/user/store/postgres"
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
	if _, err := db.Exec(`TRUNCATE TABLE users`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return s
}

func user(id, email, username string) *store.User {
	return &store.User{
		ID:          id,
		TenantID:    "tenant-1",
		Email:       email,
		Username:    username,
		PassHash:    "hash",
		DisplayName: "Display " + username,
		Active:      true,
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
		u := user(id, "conf-"+itoa(seq)+"@example.com", "conf-user-"+itoa(seq))
		u.TenantID = tenantID
		return id, s.Create(ctx, u)
	}

	t.Run("list is tenant scoped", func(t *testing.T) {
		contracttest.AssertListTenantScoped(t, contracttest.ListScopedStore{
			Create: create,
			List: func(ctx context.Context, tenantID string) ([]string, error) {
				rows, err := s.List(ctx, tenantID, 100, 0)
				out := make([]string, 0, len(rows))
				for _, r := range rows {
					out = append(out, r.ID)
				}
				return out, err
			},
		})
	})

	// The Store contract states a row cannot be reassigned to another tenant
	// via Update. An adapter that writes the caller-supplied TenantID instead
	// of preserving the stored one hands a row to a different tenant.
	t.Run("update cannot reassign tenant", func(t *testing.T) {
		contracttest.AssertUpdateCannotReassignTenant(t, contracttest.TenantImmutableStore{
			Create: create,
			Update: func(ctx context.Context, tenantID, id string) error {
				existing, err := s.Get(ctx, tenantID, id)
				if err != nil {
					return err
				}
				existing.DisplayName = "ordinary update"
				return s.Update(ctx, existing)
			},
			UpdateReassigning: func(ctx context.Context, tenantID, id, newTenantID string) error {
				existing, err := s.Get(ctx, tenantID, id)
				if err != nil {
					return err
				}
				existing.TenantID = newTenantID
				existing.DisplayName = "reassignment attempt"
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

	in := user("u1", "a@example.com", "alice")
	if err := s.Create(ctx, in); err != nil {
		t.Fatalf("create: %v", err)
	}
	if in.CreatedAt.IsZero() || in.UpdatedAt.IsZero() {
		t.Fatal("Create should default timestamps")
	}

	// A duplicate (tenant, email) is an email conflict, not a generic error.
	if err := s.Create(ctx, user("u2", "a@example.com", "bob")); !errors.Is(err, store.ErrDuplicateEmail) {
		t.Fatalf("duplicate email = %v, want ErrDuplicateEmail", err)
	}
	// A duplicate (tenant, username) is a username conflict.
	if err := s.Create(ctx, user("u3", "b@example.com", "alice")); !errors.Is(err, store.ErrDuplicateUsername) {
		t.Fatalf("duplicate username = %v, want ErrDuplicateUsername", err)
	}
	// The same email in a different tenant is allowed: uniqueness is tenant-scoped.
	other := user("u4", "a@example.com", "alice")
	other.TenantID = "tenant-2"
	if err := s.Create(ctx, other); err != nil {
		t.Fatalf("same email in a different tenant should be allowed: %v", err)
	}

	// Get is tenant-scoped: the wrong tenant sees nothing.
	if _, err := s.Get(ctx, "tenant-3", "u1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant Get = %v, want ErrNotFound", err)
	}
	got, err := s.Get(ctx, "tenant-1", "u1")
	if err != nil || got.Email != "a@example.com" || got.Username != "alice" || !got.Active {
		t.Fatalf("Get = %+v, %v", got, err)
	}

	// Email and username lookups are tenant-scoped.
	byEmail, err := s.GetByEmail(ctx, "tenant-1", "a@example.com")
	if err != nil || byEmail.ID != "u1" {
		t.Fatalf("GetByEmail = %v, %v; want u1", byEmail, err)
	}
	byUsername, err := s.GetByUsername(ctx, "tenant-1", "alice")
	if err != nil || byUsername.ID != "u1" {
		t.Fatalf("GetByUsername = %v, %v; want u1", byUsername, err)
	}
	if _, err := s.GetByEmail(ctx, "tenant-3", "a@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant GetByEmail = %v, want ErrNotFound", err)
	}

	// Update overwrites mutable fields but preserves pass_hash.
	got.Email = "alice@example.com"
	got.DisplayName = "Alice"
	got.Active = false
	if err := s.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	after, err := s.Get(ctx, "tenant-1", "u1")
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if after.Email != "alice@example.com" || after.DisplayName != "Alice" || after.Active {
		t.Fatalf("Update not persisted: %+v", after)
	}
	if after.PassHash != "hash" {
		t.Fatalf("Update should preserve PassHash, got %q", after.PassHash)
	}

	// UpdatePassHash is tenant-scoped: the cross-tenant password-reset IDOR is denied.
	at := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if err := s.UpdatePassHash(ctx, "tenant-3", "u1", "attacker-hash", at); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant UpdatePassHash = %v, want ErrNotFound", err)
	}
	if err := s.UpdatePassHash(ctx, "tenant-1", "u1", "newhash", at); err != nil {
		t.Fatalf("UpdatePassHash: %v", err)
	}
	after, _ = s.Get(ctx, "tenant-1", "u1")
	if after.PassHash != "newhash" {
		t.Fatalf("PassHash = %q, want newhash", after.PassHash)
	}
	if !after.UpdatedAt.Equal(at) {
		t.Fatalf("UpdatedAt = %v, want %v", after.UpdatedAt, at)
	}

	// List is tenant-scoped and ordered by username.
	rows, err := s.List(ctx, "tenant-1", 10, 0)
	if err != nil || len(rows) != 1 || rows[0].ID != "u1" {
		t.Fatalf("List = %d rows (%v), %v; want 1 [u1]", len(rows), rows, err)
	}

	// Delete is tenant-scoped and reports ErrNotFound on absence.
	if err := s.Delete(ctx, "tenant-3", "u1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant Delete = %v, want ErrNotFound", err)
	}
	if err := s.Delete(ctx, "tenant-1", "u1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, "tenant-1", "u1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get after delete = %v, want ErrNotFound", err)
	}
}
