// sqlite_test.go exercises the api_key_management sqlite store against a real
// modernc.org/sqlite database opened on a per-test temp file. Tests cover the
// CRUD/list round-trip, not-found behavior, prefix lookup, revoke semantics,
// and primary-key uniqueness.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package sqlite_test

// Validates: REQ-APIKEY-001.
// Per: ADR-0017.
// Discipline: C-14.
import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/septagon-oss/pk-modules/pkg/apikey/store"
	"github.com/septagon-oss/pk-modules/pkg/apikey/store/sqlite"

	_ "modernc.org/sqlite"
)

// newStore opens a fresh sqlite store backed by an isolated temp-file DSN so
// each test runs against its own schema with no shared global state.
func newStore(t *testing.T) *sqlite.Store {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "apikeys.db") + "?_pragma=journal_mode(WAL)"
	s, err := sqlite.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.DB().Close() })
	return s
}

func sampleKey(id string) *store.APIKey {
	return &store.APIKey{
		ID:       id,
		TenantID: "tenant-1",
		UserID:   "user-1",
		Name:     "ci-token",
		Prefix:   "pk_" + id,
		Hash:     "hash-" + id,
		Scopes:   `["read"]`,
	}
}

func TestNewRejectsNilDB(t *testing.T) {
	t.Parallel()
	if _, err := sqlite.New(nil); err == nil {
		t.Fatal("New(nil) should return an error")
	}
}

func TestCreateGetRoundTrip(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()

	in := sampleKey("k1")
	if err := s.Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if in.CreatedAt.IsZero() {
		t.Fatal("Create should default CreatedAt")
	}

	got, err := s.Get(ctx, "tenant-1", "k1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != in.ID || got.TenantID != in.TenantID || got.Hash != in.Hash || got.Scopes != in.Scopes {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, in)
	}
	if got.LastUsedAt != nil || got.RevokedAt != nil || got.ExpiresAt != nil {
		t.Fatalf("nullable timestamps should be nil: %+v", got)
	}
}

func TestGetNotFound(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if _, err := s.Get(context.Background(), "tenant-1", "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get(missing) err = %v, want ErrNotFound", err)
	}
}

// TestCrossTenantManagementDeniedButAuthLookupGlobal proves the two-sided
// contract: management-by-ID (Get, Revoke) is tenant-scoped so a caller cannot
// touch another tenant's key, while GetByPrefix — the authentication path —
// stays GLOBAL so a presented key resolves regardless of the caller's tenant.
func TestCrossTenantManagementDeniedButAuthLookupGlobal(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()

	in := sampleKey("k1") // tenant-1, prefix pk_k1
	if err := s.Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Management by ID is denied across tenants.
	if _, err := s.Get(ctx, "tenant-2", "k1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant Get err = %v, want ErrNotFound", err)
	}
	if err := s.Revoke(ctx, "tenant-2", "k1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant Revoke err = %v, want ErrNotFound", err)
	}
	// The owner still sees an un-revoked key.
	got, err := s.Get(ctx, "tenant-1", "k1")
	if err != nil {
		t.Fatalf("owner Get: %v", err)
	}
	if got.RevokedAt != nil {
		t.Fatal("cross-tenant Revoke revoked the owner's key")
	}

	// Authentication lookup is global: the presented prefix resolves the key
	// without any tenant context (the key carries its own tenant).
	rows, err := s.GetByPrefix(ctx, "pk_k1")
	if err != nil {
		t.Fatalf("GetByPrefix: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "k1" || rows[0].TenantID != "tenant-1" {
		t.Fatalf("GetByPrefix must resolve the key globally, got %+v", rows)
	}
}

func TestCreateDuplicateID(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	if err := s.Create(ctx, sampleKey("dup")); err != nil {
		t.Fatalf("Create #1: %v", err)
	}
	err := s.Create(ctx, sampleKey("dup"))
	if !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("Create duplicate err = %v, want ErrDuplicate", err)
	}
}

func TestGetByPrefix(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()

	a := sampleKey("a")
	a.Prefix = "shared"
	b := sampleKey("b")
	b.Prefix = "shared"
	c := sampleKey("c")
	c.Prefix = "other"
	for _, k := range []*store.APIKey{a, b, c} {
		if err := s.Create(ctx, k); err != nil {
			t.Fatalf("Create %s: %v", k.ID, err)
		}
	}

	got, err := s.GetByPrefix(ctx, "shared")
	if err != nil {
		t.Fatalf("GetByPrefix: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("GetByPrefix returned %d keys, want 2", len(got))
	}

	empty, err := s.GetByPrefix(ctx, "nope")
	if err != nil {
		t.Fatalf("GetByPrefix(nope): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("GetByPrefix(nope) = %d, want 0", len(empty))
	}
}

func TestListByTenantOrdered(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	older := sampleKey("older")
	older.CreatedAt = base
	newer := sampleKey("newer")
	newer.CreatedAt = base.Add(time.Hour)
	otherTenant := sampleKey("other")
	otherTenant.TenantID = "tenant-2"
	for _, k := range []*store.APIKey{older, newer, otherTenant} {
		if err := s.Create(ctx, k); err != nil {
			t.Fatalf("Create %s: %v", k.ID, err)
		}
	}

	got, err := s.List(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List returned %d, want 2 (tenant scoped)", len(got))
	}
	if got[0].ID != "newer" || got[1].ID != "older" {
		t.Fatalf("List order = [%s, %s], want [newer, older] (created_at DESC)", got[0].ID, got[1].ID)
	}
}

func TestRevoke(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	if err := s.Create(ctx, sampleKey("rev")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Revoke(ctx, "tenant-1", "rev"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	got, err := s.Get(ctx, "tenant-1", "rev")
	if err != nil {
		t.Fatalf("Get after revoke: %v", err)
	}
	if got.RevokedAt == nil {
		t.Fatal("Revoke did not set RevokedAt")
	}
	// Revoking again is a no-op (already revoked) and must not error.
	if err := s.Revoke(ctx, "tenant-1", "rev"); err != nil {
		t.Fatalf("Revoke (second) should be a no-op: %v", err)
	}
}

func TestRevokeNotFound(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.Revoke(context.Background(), "tenant-1", "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Revoke(missing) err = %v, want ErrNotFound", err)
	}
}

func TestUpdateLastUsed(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	if err := s.Create(ctx, sampleKey("lu")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	at := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if err := s.UpdateLastUsed(ctx, "tenant-1", "lu", at); err != nil {
		t.Fatalf("UpdateLastUsed: %v", err)
	}
	got, err := s.Get(ctx, "tenant-1", "lu")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LastUsedAt == nil || !got.LastUsedAt.Equal(at) {
		t.Fatalf("LastUsedAt = %v, want %v", got.LastUsedAt, at)
	}
}

func TestCreateNil(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.Create(context.Background(), nil); err == nil {
		t.Fatal("Create(nil) should return an error")
	}
}

func TestListExcludesRevoked(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()

	active := sampleKey("active")
	revoked := sampleKey("revoked")
	for _, k := range []*store.APIKey{active, revoked} {
		if err := s.Create(ctx, k); err != nil {
			t.Fatalf("Create %s: %v", k.ID, err)
		}
	}
	if err := s.Revoke(ctx, "tenant-1", "revoked"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	got, err := s.List(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != "active" {
		t.Fatalf("List after revoke = %+v, want only the active key", got)
	}
}
