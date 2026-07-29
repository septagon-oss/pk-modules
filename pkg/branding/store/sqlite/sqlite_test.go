// sqlite_test.go exercises the branding_management sqlite store against a
// real modernc.org/sqlite database opened on a per-test temp file. Tests
// cover not-found behavior, a full Upsert/Get round trip including logo
// bytes and the setup-complete stamp, in-place updates on repeated Upsert
// (created_at preserved, updated_at refreshed, one row), and per-tenant
// isolation.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package sqlite_test

// Validates: REQ-BRANDING-001.
// Per: ADR-0017.
// Discipline: C-14.
import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/septagon-oss/pk-modules/pkg/branding/store"
	"github.com/septagon-oss/pk-modules/pkg/branding/store/sqlite"

	_ "modernc.org/sqlite"
)

func newStore(t *testing.T) *sqlite.Store {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "branding.db") + "?_pragma=journal_mode(WAL)"
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

func TestGetNotFound(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if _, err := s.Get(context.Background(), "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get(missing) err = %v, want ErrNotFound", err)
	}
}

func TestUpsertGetRoundTrip(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()

	completedAt := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	in := &store.Record{
		TenantID:         "t1",
		DisplayName:      "Acme Ops",
		LogoData:         []byte{0x89, 0x50, 0x4e, 0x47, 0x00, 0x01, 0x02, 0x03},
		LogoContentType:  "image/png",
		LogoAlt:          "Acme logo",
		PrimaryColor:     "#14b8a6",
		FontKey:          "editorial",
		SetupCompletedAt: &completedAt,
	}
	if err := s.Upsert(ctx, in); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if in.CreatedAt.IsZero() || in.UpdatedAt.IsZero() {
		t.Fatal("Upsert should stamp timestamps")
	}

	got, err := s.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.TenantID != "t1" ||
		got.DisplayName != "Acme Ops" ||
		string(got.LogoData) != string(in.LogoData) ||
		got.LogoContentType != "image/png" ||
		got.LogoAlt != "Acme logo" ||
		got.PrimaryColor != "#14b8a6" ||
		got.FontKey != "editorial" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.SetupCompletedAt == nil || !got.SetupCompletedAt.Equal(completedAt) {
		t.Fatalf("SetupCompletedAt = %v, want %v", got.SetupCompletedAt, completedAt)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatal("Get should return stamped timestamps")
	}
}

func TestGetNoLogo(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()

	if err := s.Upsert(ctx, &store.Record{TenantID: "t1", DisplayName: "Acme Ops"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := s.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LogoData != nil {
		t.Fatalf("LogoData = %v, want nil", got.LogoData)
	}
	if got.SetupCompletedAt != nil {
		t.Fatalf("SetupCompletedAt = %v, want nil", got.SetupCompletedAt)
	}
}

func TestUpsertTwiceUpdatesInPlace(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()

	if err := s.Upsert(ctx, &store.Record{TenantID: "t1", DisplayName: "Acme Ops"}); err != nil {
		t.Fatalf("Upsert #1: %v", err)
	}
	first, err := s.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("Get after #1: %v", err)
	}

	// Sleep so the second UpdatedAt is observably later than the first.
	// modernc.org/sqlite stores DATETIME as nanosecond-precision text, so
	// resolution isn't the issue — without the sleep, two time.Now() calls in
	// the same test can land on the same instant and the After() assertion
	// below would flake.
	time.Sleep(2 * time.Millisecond)

	if err := s.Upsert(ctx, &store.Record{TenantID: "t1", DisplayName: "Acme Ops Renamed", PrimaryColor: "#000000"}); err != nil {
		t.Fatalf("Upsert #2: %v", err)
	}
	second, err := s.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("Get after #2: %v", err)
	}

	if second.DisplayName != "Acme Ops Renamed" || second.PrimaryColor != "#000000" {
		t.Fatalf("Upsert #2 not persisted: %+v", second)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("CreatedAt changed on update: first=%v second=%v", first.CreatedAt, second.CreatedAt)
	}
	if !second.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("UpdatedAt did not advance: first=%v second=%v", first.UpdatedAt, second.UpdatedAt)
	}

	var rowCount int
	if err := s.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM branding_profiles WHERE tenant_id = ?`, "t1",
	).Scan(&rowCount); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("row count = %d, want 1", rowCount)
	}
}

func TestUpsertNil(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.Upsert(context.Background(), nil); err == nil {
		t.Fatal("Upsert(nil) should return an error")
	}
}

// TestUpsertExistingReportsPersistedCreatedAt guards against a subtle bug:
// a second Upsert for an already-existing tenant passes a zero r.CreatedAt
// (the caller never sets it), and it would be wrong for the store to stamp
// that zero with time.Now() and hand it back on r — the row's created_at was
// never touched by the conflict path, so the caller's in-memory record must
// reflect the original, persisted value, not the moment of the second call.
func TestUpsertExistingReportsPersistedCreatedAt(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()

	first := &store.Record{TenantID: "t1", DisplayName: "Acme Ops"}
	if err := s.Upsert(ctx, first); err != nil {
		t.Fatalf("Upsert #1: %v", err)
	}
	originalCreatedAt := first.CreatedAt

	time.Sleep(2 * time.Millisecond)

	second := &store.Record{TenantID: "t1", DisplayName: "Acme Ops Renamed"}
	if err := s.Upsert(ctx, second); err != nil {
		t.Fatalf("Upsert #2: %v", err)
	}
	if !second.CreatedAt.Equal(originalCreatedAt) {
		t.Fatalf("Upsert on an existing tenant reported CreatedAt = %v, want the persisted value %v",
			second.CreatedAt, originalCreatedAt)
	}

	got, err := s.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.CreatedAt.Equal(originalCreatedAt) {
		t.Fatalf("persisted CreatedAt = %v, want %v", got.CreatedAt, originalCreatedAt)
	}
}

// TestUpsertEmptyLogoNormalizesToNull pins the documented behavior that an
// empty (non-nil) LogoData slice is treated the same as "no logo" and stored
// as SQL NULL, not as a zero-length BLOB.
func TestUpsertEmptyLogoNormalizesToNull(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()

	if err := s.Upsert(ctx, &store.Record{TenantID: "t1", DisplayName: "Acme Ops", LogoData: []byte{}}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := s.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LogoData != nil {
		t.Fatalf("LogoData = %v, want nil (empty slice normalizes to NULL)", got.LogoData)
	}
}

func TestTenantsIsolated(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()

	if err := s.Upsert(ctx, &store.Record{TenantID: "t1", DisplayName: "Tenant One", PrimaryColor: "#111111"}); err != nil {
		t.Fatalf("Upsert t1: %v", err)
	}
	if err := s.Upsert(ctx, &store.Record{TenantID: "t2", DisplayName: "Tenant Two", PrimaryColor: "#222222"}); err != nil {
		t.Fatalf("Upsert t2: %v", err)
	}

	got1, err := s.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("Get t1: %v", err)
	}
	got2, err := s.Get(ctx, "t2")
	if err != nil {
		t.Fatalf("Get t2: %v", err)
	}
	if got1.DisplayName != "Tenant One" || got1.PrimaryColor != "#111111" {
		t.Fatalf("t1 record leaked/incorrect: %+v", got1)
	}
	if got2.DisplayName != "Tenant Two" || got2.PrimaryColor != "#222222" {
		t.Fatalf("t2 record leaked/incorrect: %+v", got2)
	}

	// Re-upserting t1 must not disturb t2's row: ON CONFLICT targets only the
	// conflicting tenant_id, so t2 should read back byte-for-byte identical.
	if err := s.Upsert(ctx, &store.Record{TenantID: "t1", DisplayName: "Tenant One Renamed", PrimaryColor: "#333333"}); err != nil {
		t.Fatalf("Upsert t1 again: %v", err)
	}
	got2Again, err := s.Get(ctx, "t2")
	if err != nil {
		t.Fatalf("Get t2 again: %v", err)
	}
	if got2Again.TenantID != got2.TenantID ||
		got2Again.DisplayName != got2.DisplayName ||
		string(got2Again.LogoData) != string(got2.LogoData) ||
		got2Again.LogoContentType != got2.LogoContentType ||
		got2Again.LogoAlt != got2.LogoAlt ||
		got2Again.PrimaryColor != got2.PrimaryColor ||
		got2Again.FontKey != got2.FontKey ||
		(got2Again.SetupCompletedAt == nil) != (got2.SetupCompletedAt == nil) ||
		!got2Again.CreatedAt.Equal(got2.CreatedAt) ||
		!got2Again.UpdatedAt.Equal(got2.UpdatedAt) {
		t.Fatalf("t2 record changed after re-upserting t1: before=%+v after=%+v", got2, got2Again)
	}
}
