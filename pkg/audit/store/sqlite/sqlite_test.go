// sqlite_test.go exercises the audit_management sqlite store against a real
// modernc.org/sqlite database opened on a per-test temp file. Tests cover the
// append-then-query round-trip, chronological ordering, filter combinations,
// limit capping, and primary-key uniqueness.
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

	"github.com/septagon-oss/pk-modules/pkg/audit/store"
	"github.com/septagon-oss/pk-modules/pkg/audit/store/sqlite"

	_ "modernc.org/sqlite"
)

func newStore(t *testing.T) *sqlite.Store {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "audit.db") + "?_pragma=journal_mode(WAL)"
	s, err := sqlite.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.DB().Close() })
	return s
}

func event(id string, at time.Time) *store.Event {
	return &store.Event{
		ID:        id,
		TenantID:  "tenant-1",
		Actor:     "actor-1",
		Action:    "thing.done",
		Resource:  "res-1",
		Severity:  "info",
		Details:   `{"k":"v"}`,
		EmittedAt: at,
	}
}

func TestNewRejectsNilDB(t *testing.T) {
	t.Parallel()
	if _, err := sqlite.New(nil); err == nil {
		t.Fatal("New(nil) should return an error")
	}
}

func TestAppendThenQuery(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()

	e := event("e1", time.Time{}) // zero EmittedAt -> defaulted
	if err := s.Append(ctx, e); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if e.EmittedAt.IsZero() {
		t.Fatal("Append should default EmittedAt")
	}

	got, err := s.Query(ctx, store.QueryFilter{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Query returned %d events, want 1", len(got))
	}
	if got[0].ID != "e1" || got[0].Details != `{"k":"v"}` {
		t.Fatalf("round-trip mismatch: %+v", got[0])
	}
}

func TestAppendDuplicateID(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := s.Append(ctx, event("dup", now)); err != nil {
		t.Fatalf("Append #1: %v", err)
	}
	err := s.Append(ctx, event("dup", now))
	if !errors.Is(err, store.ErrDuplicateID) {
		t.Fatalf("Append duplicate err = %v, want ErrDuplicateID", err)
	}
}

func TestQueryOrdersChronologically(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Insert out of order; expect ascending emitted_at.
	if err := s.Append(ctx, event("c", base.Add(2*time.Hour))); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(ctx, event("a", base)); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(ctx, event("b", base.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	got, err := s.Query(ctx, store.QueryFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("order[%d] = %s, want %s", i, got[i].ID, id)
		}
	}
}

func TestQueryFilters(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	mk := func(id, tenant, actor, action string, at time.Time) *store.Event {
		e := event(id, at)
		e.TenantID, e.Actor, e.Action = tenant, actor, action
		return e
	}
	seed := []*store.Event{
		mk("1", "t1", "alice", "login", base),
		mk("2", "t1", "bob", "logout", base.Add(time.Hour)),
		mk("3", "t2", "alice", "login", base.Add(2*time.Hour)),
	}
	for _, e := range seed {
		if err := s.Append(ctx, e); err != nil {
			t.Fatalf("Append %s: %v", e.ID, err)
		}
	}

	cases := []struct {
		name   string
		filter store.QueryFilter
		wantN  int
	}{
		{"tenant", store.QueryFilter{TenantID: "t1"}, 2},
		{"actor", store.QueryFilter{Actor: "alice"}, 2},
		{"action", store.QueryFilter{Action: "login"}, 2},
		{"tenant+actor", store.QueryFilter{TenantID: "t1", Actor: "alice"}, 1},
		{"since", store.QueryFilter{Since: base.Add(time.Hour)}, 2},
		{"until", store.QueryFilter{Until: base.Add(time.Hour)}, 1},
		{"window", store.QueryFilter{Since: base.Add(time.Hour), Until: base.Add(2 * time.Hour)}, 1},
		{"none-match", store.QueryFilter{Actor: "carol"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.Query(ctx, tc.filter)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(got) != tc.wantN {
				t.Fatalf("got %d events, want %d", len(got), tc.wantN)
			}
		})
	}
}

func TestQueryLimitCap(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	base := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if err := s.Append(ctx, event(string(rune('a'+i)), base.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	got, err := s.Query(ctx, store.QueryFilter{Limit: 2})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Query(limit=2) returned %d, want 2", len(got))
	}
}

func TestAppendNil(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.Append(context.Background(), nil); err == nil {
		t.Fatal("Append(nil) should return an error")
	}
}
