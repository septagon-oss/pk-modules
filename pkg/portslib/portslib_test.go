// Validates: REQ-PORTS-001.
// Per: ADR-0009.
// Discipline: C-14.

package portslib_test

// portslib_test.go validates the shared port contracts in portslib via the
// external test package, so the tests only depend on the public surface.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

func TestAdminPageZeroValueIsUsable(t *testing.T) {
	t.Parallel()

	var p portslib.AdminPage
	if p.ModuleID != "" || p.Path != "" || p.Title != "" {
		t.Fatalf("zero AdminPage has unexpected non-empty fields: %+v", p)
	}
	if p.Render != nil {
		t.Fatalf("zero AdminPage.Render should be nil")
	}
}

func TestSidebarSectionZeroValueIsUsable(t *testing.T) {
	t.Parallel()

	var s portslib.SidebarSection
	if s.Label != "" || s.ModuleID != "" {
		t.Fatalf("zero SidebarSection has unexpected non-empty fields: %+v", s)
	}
	if len(s.Items) != 0 {
		t.Fatalf("zero SidebarSection.Items should be empty")
	}
	if s.Order != 0 {
		t.Fatalf("zero SidebarSection.Order should be 0")
	}
}

func TestNotificationZeroValueDefaults(t *testing.T) {
	t.Parallel()

	var n portslib.Notification
	if n.ID != "" || n.TenantID != "" || n.UserID != "" {
		t.Fatalf("zero Notification has unexpected non-empty IDs: %+v", n)
	}
	if !n.EmittedAt.IsZero() {
		t.Fatalf("zero Notification.EmittedAt should be the zero time")
	}
	if n.Data != nil {
		t.Fatalf("zero Notification.Data should be nil")
	}
}

// fakeChannel verifies that the NotificationChannel interface is satisfiable
// by a small custom implementation. The compile-check happens at the
// var _ NotificationChannel = (*fakeChannel)(nil) line below.
type fakeChannel struct {
	name      string
	delivered []portslib.Notification
	failNext  error
}

func (f *fakeChannel) Name() string { return f.name }

func (f *fakeChannel) Deliver(_ context.Context, n portslib.Notification) error {
	if f.failNext != nil {
		err := f.failNext
		f.failNext = nil
		return err
	}
	f.delivered = append(f.delivered, n)
	return nil
}

var _ portslib.NotificationChannel = (*fakeChannel)(nil)

func TestNotificationChannelDelivers(t *testing.T) {
	t.Parallel()

	ch := &fakeChannel{name: "in-app"}
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	n := portslib.Notification{
		ID:        "n-1",
		TenantID:  "t-1",
		UserID:    "u-1",
		Title:     "Welcome",
		Body:      "Hello",
		Category:  "system",
		Severity:  "info",
		Data:      map[string]any{"foo": "bar"},
		EmittedAt: now,
	}
	if err := ch.Deliver(context.Background(), n); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if got := len(ch.delivered); got != 1 {
		t.Fatalf("delivered count = %d, want 1", got)
	}
	if ch.delivered[0].ID != "n-1" {
		t.Fatalf("delivered ID = %q", ch.delivered[0].ID)
	}
	if ch.Name() != "in-app" {
		t.Fatalf("Name() = %q", ch.Name())
	}
}

func TestNotificationChannelPropagatesError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	ch := &fakeChannel{name: "in-app", failNext: wantErr}
	err := ch.Deliver(context.Background(), portslib.Notification{ID: "n-1"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Deliver err = %v, want %v", err, wantErr)
	}
}

func TestAdminPageRenderInvokesHandler(t *testing.T) {
	t.Parallel()

	called := false
	page := portslib.AdminPage{
		ModuleID: "blog",
		Path:     "/admin/blog",
		Title:    "Blog",
		Render: func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, page.Path, nil)
	page.Render(rec, req)
	if !called {
		t.Fatalf("Render was not invoked")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}
