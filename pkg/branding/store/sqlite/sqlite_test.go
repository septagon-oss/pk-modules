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

	// Sleep so the second UpdatedAt is observably later than the first; sqlite
	// stores DATETIME with second-level-or-better resolution, but the two
	// Upserts could otherwise land in the same instant.
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
}
