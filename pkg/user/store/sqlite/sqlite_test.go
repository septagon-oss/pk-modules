// sqlite_test.go exercises the user_management sqlite store against a real
// modernc.org/sqlite database opened on a per-test temp file. Tests cover the
// full CRUD round-trip, email/username lookups, tenant-scoped listing,
// password-hash updates, not-found behavior, and the tenant-scoped email and
// username uniqueness constraints.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package sqlite_test

// Validates: REQ-USER-001.
// Per: ADR-0017.
// Discipline: C-14.
import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/septagon-oss/pk-modules/pkg/user/store"
	"github.com/septagon-oss/pk-modules/pkg/user/store/sqlite"

	_ "modernc.org/sqlite"
)

func newStore(t *testing.T) *sqlite.Store {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "user.db") + "?_pragma=journal_mode(WAL)"
	s, err := sqlite.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.DB().Close() })
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

	in := user("u1", "a@example.com", "alice")
	if err := s.Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if in.CreatedAt.IsZero() || in.UpdatedAt.IsZero() {
		t.Fatal("Create should default timestamps")
	}

	got, err := s.Get(ctx, "tenant-1", "u1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Email != "a@example.com" || got.Username != "alice" || !got.Active {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

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
	// Update must not touch PassHash.
	if after.PassHash != "hash" {
		t.Fatalf("Update should preserve PassHash, got %q", after.PassHash)
	}

	if err := s.Delete(ctx, "tenant-1", "u1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, "tenant-1", "u1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get after delete err = %v, want ErrNotFound", err)
	}
}

// TestCrossTenantByIDIsDenied proves the tenant predicate on by-ID operations,
// including the password-reset path: a user created in tenant-1 is invisible
// and immutable to tenant-2 even when tenant-2 knows the exact user ID. This is
// the regression guard for the v0.1.0 cross-tenant IDOR (read, delete, and
// password reset).
func TestCrossTenantByIDIsDenied(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()

	victim := user("victim-id", "victim@example.com", "victim")
	victim.TenantID = "tenant-1"
	if err := s.Create(ctx, victim); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := s.Get(ctx, "tenant-2", "victim-id"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant Get err = %v, want ErrNotFound", err)
	}
	if err := s.Delete(ctx, "tenant-2", "victim-id"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant Delete err = %v, want ErrNotFound", err)
	}
	// The password-reset IDOR: another tenant must not be able to rewrite the
	// victim's credential by ID.
	if err := s.UpdatePassHash(ctx, "tenant-2", "victim-id", "attacker-hash", time.Now().UTC()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant UpdatePassHash err = %v, want ErrNotFound", err)
	}

	got, err := s.Get(ctx, "tenant-1", "victim-id")
	if err != nil {
		t.Fatalf("owner Get: %v", err)
	}
	if got.PassHash == "attacker-hash" {
		t.Fatal("cross-tenant UpdatePassHash rewrote the victim's password")
	}
}

func TestGetByEmailAndUsername(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	if err := s.Create(ctx, user("u1", "a@example.com", "alice")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	byEmail, err := s.GetByEmail(ctx, "tenant-1", "a@example.com")
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	if byEmail.ID != "u1" {
		t.Fatalf("GetByEmail ID = %q, want u1", byEmail.ID)
	}
	byUsername, err := s.GetByUsername(ctx, "tenant-1", "alice")
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	if byUsername.ID != "u1" {
		t.Fatalf("GetByUsername ID = %q, want u1", byUsername.ID)
	}

	if _, err := s.GetByEmail(ctx, "tenant-1", "none@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetByEmail(none) err = %v, want ErrNotFound", err)
	}
	// Email lookup is tenant-scoped.
	if _, err := s.GetByEmail(ctx, "tenant-2", "a@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetByEmail(other tenant) err = %v, want ErrNotFound", err)
	}
}

func TestGetNotFound(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if _, err := s.Get(context.Background(), "tenant-1", "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get(missing) err = %v, want ErrNotFound", err)
	}
}

func TestListScopedAndPaged(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	for _, u := range []*store.User{
		user("1", "b@example.com", "bob"),
		user("2", "a@example.com", "alice"),
		user("3", "c@example.com", "carol"),
	} {
		if err := s.Create(ctx, u); err != nil {
			t.Fatalf("Create %s: %v", u.ID, err)
		}
	}
	other := user("4", "d@example.com", "dave")
	other.TenantID = "tenant-2"
	if err := s.Create(ctx, other); err != nil {
		t.Fatalf("Create other tenant: %v", err)
	}

	got, err := s.List(ctx, "tenant-1", 0, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"alice", "bob", "carol"} // ordered by username
	if len(got) != len(want) {
		t.Fatalf("List returned %d, want %d", len(got), len(want))
	}
	for i, name := range want {
		if got[i].Username != name {
			t.Fatalf("List order[%d] = %s, want %s", i, got[i].Username, name)
		}
	}

	page, err := s.List(ctx, "tenant-1", 1, 1)
	if err != nil {
		t.Fatalf("List page: %v", err)
	}
	if len(page) != 1 || page[0].Username != "bob" {
		t.Fatalf("paged List = %v, want [bob]", page)
	}
}

func TestCreateDuplicateEmail(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	if err := s.Create(ctx, user("u1", "dup@example.com", "alice")); err != nil {
		t.Fatalf("Create #1: %v", err)
	}
	err := s.Create(ctx, user("u2", "dup@example.com", "bob"))
	if !errors.Is(err, store.ErrDuplicateEmail) {
		t.Fatalf("Create duplicate email err = %v, want ErrDuplicateEmail", err)
	}
}

func TestCreateDuplicateUsername(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	if err := s.Create(ctx, user("u1", "a@example.com", "dup")); err != nil {
		t.Fatalf("Create #1: %v", err)
	}
	err := s.Create(ctx, user("u2", "b@example.com", "dup"))
	if !errors.Is(err, store.ErrDuplicateUsername) {
		t.Fatalf("Create duplicate username err = %v, want ErrDuplicateUsername", err)
	}
}

func TestCreateSameEmailDifferentTenant(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	if err := s.Create(ctx, user("u1", "shared@example.com", "alice")); err != nil {
		t.Fatalf("Create #1: %v", err)
	}
	other := user("u2", "shared@example.com", "alice")
	other.TenantID = "tenant-2"
	if err := s.Create(ctx, other); err != nil {
		t.Fatalf("same email in a different tenant should be allowed: %v", err)
	}
}

func TestUpdatePassHash(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	if err := s.Create(ctx, user("u1", "a@example.com", "alice")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	at := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if err := s.UpdatePassHash(ctx, "tenant-1", "u1", "newhash", at); err != nil {
		t.Fatalf("UpdatePassHash: %v", err)
	}
	got, err := s.Get(ctx, "tenant-1", "u1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PassHash != "newhash" {
		t.Fatalf("PassHash = %q, want newhash", got.PassHash)
	}
	if !got.UpdatedAt.Equal(at) {
		t.Fatalf("UpdatedAt = %v, want %v", got.UpdatedAt, at)
	}
	if err := s.UpdatePassHash(ctx, "tenant-1", "missing", "x", at); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("UpdatePassHash(missing) err = %v, want ErrNotFound", err)
	}
}

func TestUpdateNotFound(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	err := s.Update(context.Background(), user("ghost", "g@example.com", "ghost"))
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Update(ghost) err = %v, want ErrNotFound", err)
	}
}

func TestDeleteNotFound(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.Delete(context.Background(), "tenant-1", "ghost"); !errors.Is(err, store.ErrNotFound) {
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
