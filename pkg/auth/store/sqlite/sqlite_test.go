// sqlite_test.go exercises the auth_management session sqlite store against a
// real modernc.org/sqlite database opened on a per-test temp file. Tests cover
// the create/get round-trip, not-found behavior, single and per-user revoke
// semantics, and primary-key uniqueness.
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

	"github.com/septagon-oss/pk-modules/pkg/auth/store"
	"github.com/septagon-oss/pk-modules/pkg/auth/store/sqlite"

	_ "modernc.org/sqlite"
)

func newStore(t *testing.T) *sqlite.Store {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "auth.db") + "?_pragma=journal_mode(WAL)"
	s, err := sqlite.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.DB().Close() })
	return s
}

func session(id, userID string) *store.Session {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return &store.Session{
		ID:        id,
		UserID:    userID,
		TenantID:  "tenant-1",
		IssuedAt:  now,
		ExpiresAt: now.Add(time.Hour),
	}
}

func TestNewRejectsNilDB(t *testing.T) {
	t.Parallel()
	if _, err := sqlite.New(nil); err == nil {
		t.Fatal("New(nil) should return an error")
	}
}

func TestCreateThenGet(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()

	in := session("s1", "user-1")
	if err := s.Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, "s1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.UserID != "user-1" || got.TenantID != "tenant-1" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.RevokedAt != nil {
		t.Fatalf("RevokedAt should be nil for a live session: %+v", got)
	}
	if !got.IssuedAt.Equal(in.IssuedAt) || !got.ExpiresAt.Equal(in.ExpiresAt) {
		t.Fatalf("timestamps not preserved: %+v", got)
	}
}

func TestCreateDefaultsIssuedAt(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	in := &store.Session{ID: "s0", UserID: "u", TenantID: "t", ExpiresAt: time.Now().Add(time.Hour)}
	if err := s.Create(context.Background(), in); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if in.IssuedAt.IsZero() {
		t.Fatal("Create should default IssuedAt")
	}
}

func TestGetNotFound(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if _, err := s.Get(context.Background(), "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get(missing) err = %v, want ErrNotFound", err)
	}
}

func TestCreateDuplicate(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	if err := s.Create(ctx, session("dup", "u")); err != nil {
		t.Fatalf("Create #1: %v", err)
	}
	err := s.Create(ctx, session("dup", "u"))
	if !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("Create duplicate err = %v, want ErrDuplicate", err)
	}
}

func TestRevoke(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	if err := s.Create(ctx, session("r", "u")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Revoke(ctx, "r"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	got, err := s.Get(ctx, "r")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.RevokedAt == nil {
		t.Fatal("Revoke did not set RevokedAt")
	}
	// Second revoke is a no-op (already revoked) and must not error.
	if err := s.Revoke(ctx, "r"); err != nil {
		t.Fatalf("Revoke (second) should be a no-op: %v", err)
	}
}

func TestRevokeNotFound(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.Revoke(context.Background(), "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Revoke(missing) err = %v, want ErrNotFound", err)
	}
}

func TestRevokeByUser(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	for _, id := range []string{"a", "b"} {
		if err := s.Create(ctx, session(id, "victim")); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	if err := s.Create(ctx, session("c", "bystander")); err != nil {
		t.Fatalf("Create bystander: %v", err)
	}

	if err := s.RevokeByUser(ctx, "victim"); err != nil {
		t.Fatalf("RevokeByUser: %v", err)
	}
	for _, id := range []string{"a", "b"} {
		got, err := s.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get %s: %v", id, err)
		}
		if got.RevokedAt == nil {
			t.Fatalf("session %s should be revoked", id)
		}
	}
	other, err := s.Get(ctx, "c")
	if err != nil {
		t.Fatalf("Get c: %v", err)
	}
	if other.RevokedAt != nil {
		t.Fatal("bystander session should not be revoked")
	}

	// RevokeByUser for an unknown user is a no-op, not an error.
	if err := s.RevokeByUser(ctx, "nobody"); err != nil {
		t.Fatalf("RevokeByUser(nobody) should be a no-op: %v", err)
	}
}

func TestCreateNil(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.Create(context.Background(), nil); err == nil {
		t.Fatal("Create(nil) should return an error")
	}
}
