// postgres_test.go runs the api_key_management Postgres adapter through the
// same shared store conformance suite the sqlite adapter passes — list
// tenant-scoping and the revoke (soft-delete) lifecycle — plus a CRUD
// round-trip, against a real Postgres. It is gated on PK_POSTGRES_TEST_DSN:
// with no DSN the tests skip, so `go test ./...` stays green on a machine
// without Postgres, and CI (or a developer with a container) sets the DSN to
// enforce the contract. The point of the pivot: the production adapter is held
// to the identical tenant-isolation and lifecycle guarantees as the embedded
// one, by executable check rather than by faith.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package postgres_test

// Validates: REQ-APIKEY-001, REQ-PORTS-001.
// Per: ADR-0009.
// Discipline: C-14.
import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/septagon-oss/pk-modules/pkg/apikey/store"
	"github.com/septagon-oss/pk-modules/pkg/apikey/store/postgres"
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
	s, err := postgres.Open("pgx", dsn(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.DB().Close() })
	if _, err := s.DB().Exec(`TRUNCATE TABLE api_keys`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
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

func ids(rows []*store.APIKey) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
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
	create := func(s *postgres.Store, seq *int) func(context.Context, string) (string, error) {
		return func(ctx context.Context, tenantID string) (string, error) {
			*seq++
			k := sampleKey("conf-" + itoa(*seq))
			k.TenantID = tenantID
			k.Prefix = "pk_conf" + itoa(*seq)
			return k.ID, s.Create(ctx, k)
		}
	}

	t.Run("list is tenant scoped", func(t *testing.T) {
		s := newStore(t)
		seq := 0
		contracttest.AssertListTenantScoped(t, contracttest.ListScopedStore{
			Create: create(s, &seq),
			List: func(ctx context.Context, tenantID string) ([]string, error) {
				rows, err := s.List(ctx, tenantID)
				return ids(rows), err
			},
		})
	})

	// Revocation is the delete an operator performs, so a revoked key must stop
	// being listed. This is the defect fixed in v0.6.1, now held by a contract.
	t.Run("revoked keys are hidden from list", func(t *testing.T) {
		s := newStore(t)
		seq := 0
		contracttest.AssertRetiredHiddenFromList(t, contracttest.LifecycleStore{
			Create: create(s, &seq),
			Retire: s.Revoke,
			List: func(ctx context.Context, tenantID string) ([]string, error) {
				rows, err := s.List(ctx, tenantID)
				return ids(rows), err
			},
		})
	})
}

func TestPostgresStoreCRUD(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	in := sampleKey("k1")
	if err := s.Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if in.CreatedAt.IsZero() {
		t.Fatal("Create should default CreatedAt")
	}

	// A duplicate primary key is a duplicate ID, not a generic error.
	if err := s.Create(ctx, sampleKey("k1")); !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("duplicate id = %v, want ErrDuplicate", err)
	}

	// Round-trip through Get preserves the row and leaves nullable timestamps nil.
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

	// Management by ID is denied across tenants; the auth lookup is global.
	if _, err := s.Get(ctx, "tenant-2", "k1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant Get = %v, want ErrNotFound", err)
	}
	if err := s.Revoke(ctx, "tenant-2", "k1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant Revoke = %v, want ErrNotFound", err)
	}
	rows, err := s.GetByPrefix(ctx, "pk_k1")
	if err != nil {
		t.Fatalf("GetByPrefix: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "k1" || rows[0].TenantID != "tenant-1" {
		t.Fatalf("GetByPrefix must resolve the key globally, got %+v", rows)
	}
	// The owner's key survived the cross-tenant revoke attempt un-revoked.
	if owner, _ := s.Get(ctx, "tenant-1", "k1"); owner.RevokedAt != nil {
		t.Fatal("cross-tenant Revoke revoked the owner's key")
	}

	// UpdateLastUsed records the timestamp for the owning tenant.
	at := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if err := s.UpdateLastUsed(ctx, "tenant-1", "k1", at); err != nil {
		t.Fatalf("UpdateLastUsed: %v", err)
	}
	got, _ = s.Get(ctx, "tenant-1", "k1")
	if got.LastUsedAt == nil || !got.LastUsedAt.Equal(at) {
		t.Fatalf("LastUsedAt = %v, want %v", got.LastUsedAt, at)
	}

	// Revoke is a soft delete: the row persists with RevokedAt set, leaves the
	// list, and a second revoke is a no-op.
	if err := s.Revoke(ctx, "tenant-1", "k1"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	got, _ = s.Get(ctx, "tenant-1", "k1")
	if got.RevokedAt == nil {
		t.Fatal("Revoke did not set RevokedAt")
	}
	if err := s.Revoke(ctx, "tenant-1", "k1"); err != nil {
		t.Fatalf("second Revoke should be a no-op: %v", err)
	}
	list, err := s.List(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List after revoke = %d rows, want 0 (revoked excluded)", len(list))
	}
}

func TestPostgresListOrderedAndTenantScoped(t *testing.T) {
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
