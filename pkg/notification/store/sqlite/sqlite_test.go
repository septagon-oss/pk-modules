// sqlite_test.go exercises the notification_management sqlite store against a
// real modernc.org/sqlite database opened on a per-test temp file. Tests cover
// the notification create/list/mark-read round-trip, nullable column handling,
// subscription add/list/remove, and not-found behavior.
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

	"github.com/septagon-oss/pk-modules/pkg/notification/store"
	"github.com/septagon-oss/pk-modules/pkg/notification/store/sqlite"

	_ "modernc.org/sqlite"
)

func newStore(t *testing.T) *sqlite.Store {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "notif.db") + "?_pragma=journal_mode(WAL)"
	s, err := sqlite.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.DB().Close() })
	return s
}

func notif(id, userID string, at time.Time) *store.Notification {
	return &store.Notification{
		ID:        id,
		TenantID:  "tenant-1",
		UserID:    userID,
		Title:     "Hello",
		Body:      "World",
		Category:  "system",
		Severity:  "info",
		Data:      `{"k":"v"}`,
		EmittedAt: at,
	}
}

func TestNewRejectsNilDB(t *testing.T) {
	t.Parallel()
	if _, err := sqlite.New(nil); err == nil {
		t.Fatal("New(nil) should return an error")
	}
}

// TestCrossTenantIsDenied proves the tenant predicate on notification and
// subscription reads/mutations: tenant-2 cannot read tenant-1's notifications,
// mark them read, or remove tenant-1's subscription, even with exact IDs.
func TestCrossTenantIsDenied(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()

	n := notif("n1", "user-1", time.Now().UTC()) // tenant-1
	if err := s.Create(ctx, n); err != nil {
		t.Fatalf("Create notification: %v", err)
	}
	sub := &store.Subscription{ID: "sub1", TenantID: "tenant-1", UserID: "user-1", Channel: "in-app"}
	if err := s.AddSubscription(ctx, sub); err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}

	// Reads scoped to another tenant see nothing.
	if got, err := s.GetByUser(ctx, "tenant-2", "user-1", 0, 0); err != nil || len(got) != 0 {
		t.Fatalf("cross-tenant GetByUser = %v, %v; want 0 rows, nil", got, err)
	}
	if got, err := s.ListSubscriptions(ctx, "tenant-2", "user-1"); err != nil || len(got) != 0 {
		t.Fatalf("cross-tenant ListSubscriptions = %v, %v; want 0 rows, nil", got, err)
	}
	// Mutations scoped to another tenant are denied (ErrNotFound), leaving the
	// owner's rows intact.
	if err := s.MarkRead(ctx, "tenant-2", "n1", time.Now().UTC()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant MarkRead err = %v, want ErrNotFound", err)
	}
	if err := s.RemoveSubscription(ctx, "tenant-2", "sub1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant RemoveSubscription err = %v, want ErrNotFound", err)
	}

	// The owner still sees an unread notification and a live subscription.
	own, err := s.GetByUser(ctx, "tenant-1", "user-1", 0, 0)
	if err != nil || len(own) != 1 || own[0].ReadAt != nil {
		t.Fatalf("owner notification = %v, %v; want 1 unread", own, err)
	}
	subs, err := s.ListSubscriptions(ctx, "tenant-1", "user-1")
	if err != nil || len(subs) != 1 {
		t.Fatalf("owner subscriptions = %v, %v; want 1", subs, err)
	}
}

func TestCreateThenGetByUser(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()

	n := notif("n1", "user-1", time.Time{}) // zero EmittedAt -> defaulted
	if err := s.Create(ctx, n); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if n.EmittedAt.IsZero() {
		t.Fatal("Create should default EmittedAt")
	}

	got, err := s.GetByUser(ctx, "tenant-1", "user-1", 0, 0)
	if err != nil {
		t.Fatalf("GetByUser: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("GetByUser returned %d, want 1", len(got))
	}
	if got[0].Category != "system" || got[0].Data != `{"k":"v"}` {
		t.Fatalf("round-trip mismatch: %+v", got[0])
	}
	if got[0].ReadAt != nil {
		t.Fatalf("ReadAt should be nil for unread: %+v", got[0])
	}
}

func TestEmptyNullableColumns(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	n := notif("n1", "user-1", time.Now().UTC())
	n.Category = ""
	n.Data = ""
	if err := s.Create(ctx, n); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.GetByUser(ctx, "tenant-1", "user-1", 0, 0)
	if err != nil {
		t.Fatalf("GetByUser: %v", err)
	}
	if got[0].Category != "" || got[0].Data != "" {
		t.Fatalf("empty nullable columns should round-trip empty: %+v", got[0])
	}
}

func TestGetByUserOrderedAndScoped(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := s.Create(ctx, notif("old", "user-1", base)); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, notif("new", "user-1", base.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, notif("other", "user-2", base)); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetByUser(ctx, "tenant-1", "user-1", 0, 0)
	if err != nil {
		t.Fatalf("GetByUser: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("GetByUser(user-1) returned %d, want 2", len(got))
	}
	if got[0].ID != "new" || got[1].ID != "old" {
		t.Fatalf("order = [%s, %s], want [new, old] (emitted_at DESC)", got[0].ID, got[1].ID)
	}

	// Pagination.
	page, err := s.GetByUser(ctx, "tenant-1", "user-1", 1, 1)
	if err != nil {
		t.Fatalf("GetByUser page: %v", err)
	}
	if len(page) != 1 || page[0].ID != "old" {
		t.Fatalf("paged GetByUser = %v, want [old]", page)
	}
}

func TestMarkRead(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	if err := s.Create(ctx, notif("n1", "user-1", time.Now().UTC())); err != nil {
		t.Fatalf("Create: %v", err)
	}
	at := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if err := s.MarkRead(ctx, "tenant-1", "n1", at); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	got, err := s.GetByUser(ctx, "tenant-1", "user-1", 0, 0)
	if err != nil {
		t.Fatalf("GetByUser: %v", err)
	}
	if got[0].ReadAt == nil || !got[0].ReadAt.Equal(at) {
		t.Fatalf("ReadAt = %v, want %v", got[0].ReadAt, at)
	}
	if err := s.MarkRead(ctx, "tenant-1", "missing", at); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("MarkRead(missing) err = %v, want ErrNotFound", err)
	}
}

func TestSubscriptionLifecycle(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()

	sub := &store.Subscription{ID: "sub1", TenantID: "tenant-1", UserID: "user-1", Category: "billing", Channel: "in-app"}
	if err := s.AddSubscription(ctx, sub); err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}
	if sub.CreatedAt.IsZero() {
		t.Fatal("AddSubscription should default CreatedAt")
	}

	subNoCat := &store.Subscription{ID: "sub2", TenantID: "tenant-1", UserID: "user-1", Channel: "in-app"}
	if err := s.AddSubscription(ctx, subNoCat); err != nil {
		t.Fatalf("AddSubscription (no category): %v", err)
	}

	list, err := s.ListSubscriptions(ctx, "tenant-1", "user-1")
	if err != nil {
		t.Fatalf("ListSubscriptions: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListSubscriptions returned %d, want 2", len(list))
	}

	if err := s.RemoveSubscription(ctx, "tenant-1", "sub1"); err != nil {
		t.Fatalf("RemoveSubscription: %v", err)
	}
	list, err = s.ListSubscriptions(ctx, "tenant-1", "user-1")
	if err != nil {
		t.Fatalf("ListSubscriptions after remove: %v", err)
	}
	if len(list) != 1 || list[0].ID != "sub2" {
		t.Fatalf("after remove = %v, want [sub2]", list)
	}
	if list[0].Category != "" {
		t.Fatalf("empty category should round-trip empty: %q", list[0].Category)
	}

	if err := s.RemoveSubscription(ctx, "tenant-1", "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("RemoveSubscription(missing) err = %v, want ErrNotFound", err)
	}
}

func TestAddSubscriptionDuplicate(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	sub := &store.Subscription{ID: "dup", TenantID: "t", UserID: "u", Channel: "in-app"}
	if err := s.AddSubscription(ctx, sub); err != nil {
		t.Fatalf("AddSubscription #1: %v", err)
	}
	err := s.AddSubscription(ctx, &store.Subscription{ID: "dup", TenantID: "t", UserID: "u", Channel: "in-app"})
	if !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("AddSubscription duplicate err = %v, want ErrDuplicate", err)
	}
}

func TestCreateNil(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.Create(context.Background(), nil); err == nil {
		t.Fatal("Create(nil) should return an error")
	}
	if err := s.AddSubscription(context.Background(), nil); err == nil {
		t.Fatal("AddSubscription(nil) should return an error")
	}
}
