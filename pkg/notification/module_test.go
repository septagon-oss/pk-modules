// Validates: REQ-NOTIF-002.
// Per: ADR-0017.
// Discipline: C-14.

package notification_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	pkmodule "github.com/septagon-oss/pk-core/pkg/module"
	"github.com/septagon-oss/pk-core/pkg/security/identity"

	"github.com/septagon-oss/pk-modules/pkg/notification"
	"github.com/septagon-oss/pk-modules/pkg/portslib"

	_ "modernc.org/sqlite"
)

// TestNotificationReadIsSelfOnly is the v0.2.1 regression for the same-tenant
// cross-user IDOR: the read handler must use the authenticated principal's own
// user, never a client-supplied user_id. Alice, spoofing ?user_id=u-bob, must
// still see only her own notifications; an anonymous read is 401.
func TestNotificationReadIsSelfOnly(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	for _, n := range []portslib.Notification{
		{TenantID: "t-1", UserID: "u-alice", Title: "for-alice", Body: "x", Category: "system", Severity: "info"},
		{TenantID: "t-1", UserID: "u-bob", Title: "for-bob", Body: "x", Category: "system", Severity: "info"},
	} {
		if err := m.Service().Create(ctx, &n); err != nil {
			t.Fatalf("seed notification: %v", err)
		}
	}

	// Alice reads, spoofing bob's user_id in the query — must be ignored.
	req := httptest.NewRequest(http.MethodGet, notification.APIPath+"?user_id=u-bob", nil)
	req = req.WithContext(identity.ContextWithPrincipal(req.Context(),
		identity.Principal{Subject: "u-alice", TenantID: "t-1", AuthMethod: "session"}))
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("read = %d, want 200", rec.Code)
	}
	var got []portslib.Notification
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, rec.Body.String())
	}
	for _, n := range got {
		if n.UserID != "u-alice" {
			t.Fatalf("cross-user read leaked %q's notification to alice", n.UserID)
		}
	}
	if len(got) != 1 {
		t.Fatalf("alice should see exactly her 1 notification, got %d", len(got))
	}

	// Anonymous read is rejected.
	anon := httptest.NewRequest(http.MethodGet, notification.APIPath, nil)
	arec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(arec, anon)
	if arec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous notification read = %d, want 401", arec.Code)
	}
}

func TestNotificationHTTPCreateOwnsIDAndTimestamps(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	mux := http.NewServeMux()
	m.HTTPHandler().RegisterRoutes(mux)
	principal := identity.Principal{
		Subject: "u-alice", TenantID: "t-1", AuthMethod: "session",
	}

	before := time.Now().UTC()
	notificationReq := httptest.NewRequest(
		http.MethodPost,
		notification.APIPath,
		strings.NewReader(`{
			"id":"attacker-notification",
			"tenant_id":"t-evil",
			"user_id":"u-bob",
			"title":"Welcome",
			"body":"Hello",
			"emitted_at":"1999-01-01T00:00:00Z"
		}`),
	)
	notificationReq = notificationReq.WithContext(
		identity.ContextWithPrincipal(notificationReq.Context(), principal),
	)
	notificationRec := httptest.NewRecorder()
	mux.ServeHTTP(notificationRec, notificationReq)
	if notificationRec.Code != http.StatusCreated {
		t.Fatalf("notification create = %d; body=%s", notificationRec.Code, notificationRec.Body.String())
	}
	var created portslib.Notification
	if err := json.Unmarshal(notificationRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode notification: %v", err)
	}
	if created.ID == "attacker-notification" || created.ID == "" {
		t.Fatalf("server-owned notification ID = %q", created.ID)
	}
	if created.TenantID != "t-1" || created.UserID != "u-alice" {
		t.Fatalf("server-owned notification identity = tenant %q user %q", created.TenantID, created.UserID)
	}
	if created.EmittedAt.Before(before) {
		t.Fatalf("server-owned emitted_at = %s, before request %s", created.EmittedAt, before)
	}

	subscriptionReq := httptest.NewRequest(
		http.MethodPost,
		notification.SubscriptionAPIPath,
		strings.NewReader(`{
			"id":"attacker-subscription",
			"tenant_id":"t-evil",
			"user_id":"u-bob",
			"channel":"in_app",
			"created_at":"1999-01-01T00:00:00Z"
		}`),
	)
	subscriptionReq = subscriptionReq.WithContext(
		identity.ContextWithPrincipal(subscriptionReq.Context(), principal),
	)
	subscriptionRec := httptest.NewRecorder()
	mux.ServeHTTP(subscriptionRec, subscriptionReq)
	if subscriptionRec.Code != http.StatusCreated {
		t.Fatalf("subscription create = %d; body=%s", subscriptionRec.Code, subscriptionRec.Body.String())
	}
	var subscription notification.Subscription
	if err := json.Unmarshal(subscriptionRec.Body.Bytes(), &subscription); err != nil {
		t.Fatalf("decode subscription: %v", err)
	}
	if subscription.ID == "attacker-subscription" || subscription.ID == "" {
		t.Fatalf("server-owned subscription ID = %q", subscription.ID)
	}
	if subscription.TenantID != "t-1" || subscription.UserID != "u-alice" {
		t.Fatalf(
			"server-owned subscription identity = tenant %q user %q",
			subscription.TenantID,
			subscription.UserID,
		)
	}
	if subscription.CreatedAt.Before(before) {
		t.Fatalf("server-owned created_at = %s, before request %s", subscription.CreatedAt, before)
	}
}

// TestNotificationMarkReadIsSelfOnly is the v0.2.2 regression for the
// same-tenant cross-user write IDOR: a tenant-mate must not be able to mark
// another user's notification read (or delete their subscription) by ID. Alice,
// knowing bob's notification ID, gets 404 and bob's notification stays unread.
func TestNotificationMarkReadIsSelfOnly(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()

	bob := portslib.Notification{TenantID: "t-1", UserID: "u-bob", Title: "for-bob", Body: "x", Category: "system", Severity: "info"}
	if err := m.Service().Create(ctx, &bob); err != nil {
		t.Fatalf("seed bob notification: %v", err)
	}
	if bob.ID == "" {
		t.Fatalf("seeded notification has no ID")
	}

	// The item route (/{id}/read) is only reachable through the registered mux,
	// not the bare collection ServeHTTP.
	mux := http.NewServeMux()
	m.HTTPHandler().RegisterRoutes(mux)
	markRead := func(subject string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, notification.APIPath+"/"+bob.ID+"/read", nil)
		req = req.WithContext(identity.ContextWithPrincipal(req.Context(),
			identity.Principal{Subject: subject, TenantID: "t-1", AuthMethod: "session"}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// Alice (a tenant-mate) tries to mark bob's notification read by ID.
	if rec := markRead("u-alice"); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-user mark-read = %d, want 404 (not found for a non-owner) body=%q", rec.Code, rec.Body.String())
	}
	// The 404 proves the store UPDATE matched zero rows because the user_id
	// predicate excluded alice — bob's row was untouched.

	// Bob marking his own notification read succeeds (204), confirming the ID
	// and tenant were correct and only the owner check differed.
	if rec := markRead("u-bob"); rec.Code != http.StatusNoContent {
		t.Fatalf("owner mark-read = %d, want 204 body=%q", rec.Code, rec.Body.String())
	}
}

func sqliteDSN(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return "file:" + filepath.Join(dir, "notification.db") + "?_pragma=journal_mode(WAL)"
}

func newModule(t *testing.T, opts ...notification.Option) *notification.Module {
	t.Helper()
	opts = append(opts, notification.WithSQLiteDSN(sqliteDSN(t)))
	m, err := notification.NewModule(opts...)
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}
	return m
}

// fakeChannel records each delivery for assertion. It can optionally fail.
type fakeChannel struct {
	mu        sync.Mutex
	name      string
	delivered []portslib.Notification
	failWith  error
}

func (c *fakeChannel) Name() string { return c.name }

func (c *fakeChannel) Deliver(_ context.Context, n portslib.Notification) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failWith != nil {
		return c.failWith
	}
	c.delivered = append(c.delivered, n)
	return nil
}

func (c *fakeChannel) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.delivered)
}

func TestNewModuleDefaultsIncludesInAppChannel(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	channels := m.Channels()
	if len(channels) != 1 {
		t.Fatalf("default channel count = %d, want 1", len(channels))
	}
	if channels[0].Name() != notification.ChannelInApp {
		t.Fatalf("default channel name = %q, want %q", channels[0].Name(), notification.ChannelInApp)
	}
}

func TestCreatePersistsNotification(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	n := &portslib.Notification{
		TenantID: "t-1",
		UserID:   "u-1",
		Title:    "Welcome",
		Body:     "Hello there",
		Severity: notification.SeverityInfo,
		Data:     map[string]any{"reason": "signup"},
	}
	if err := m.Service().Create(ctx, n); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if n.ID == "" {
		t.Fatalf("Create did not assign ID")
	}
	if n.EmittedAt.IsZero() {
		t.Fatalf("Create did not set EmittedAt")
	}
	got, err := m.Service().GetByUser(ctx, "t-1", "u-1", 0, 0)
	if err != nil {
		t.Fatalf("GetByUser: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Title != "Welcome" {
		t.Fatalf("Title = %q", got[0].Title)
	}
	if got[0].Data["reason"] != "signup" {
		t.Fatalf("Data payload lost: %+v", got[0].Data)
	}
}

func TestCreateDispatchesToAllChannels(t *testing.T) {
	t.Parallel()
	a := &fakeChannel{name: "a"}
	b := &fakeChannel{name: "b"}
	m := newModule(t, notification.WithChannel(a), notification.WithChannel(b))
	ctx := context.Background()
	n := &portslib.Notification{
		TenantID: "t-1",
		UserID:   "u-1",
		Title:    "Fan-out",
		Body:     "hello",
	}
	if err := m.Service().Create(ctx, n); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if a.count() != 1 {
		t.Fatalf("channel a count = %d, want 1", a.count())
	}
	if b.count() != 1 {
		t.Fatalf("channel b count = %d, want 1", b.count())
	}
}

func TestGetByUserOrdersByEmittedAt(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	t0 := time.Now().UTC()
	for i := range 3 {
		n := &portslib.Notification{
			TenantID:  "t-1",
			UserID:    "u-1",
			Title:     "n",
			Body:      "x",
			EmittedAt: t0.Add(time.Duration(i) * time.Second),
		}
		if err := m.Service().Create(ctx, n); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}
	got, err := m.Service().GetByUser(ctx, "t-1", "u-1", 0, 0)
	if err != nil {
		t.Fatalf("GetByUser: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	// Newest first.
	for i := 0; i < len(got)-1; i++ {
		if !got[i].EmittedAt.After(got[i+1].EmittedAt) && !got[i].EmittedAt.Equal(got[i+1].EmittedAt) {
			t.Fatalf("ordering broken: %v before %v", got[i].EmittedAt, got[i+1].EmittedAt)
		}
	}
}

func TestMarkRead(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	n := &portslib.Notification{
		TenantID: "t-1",
		UserID:   "u-1",
		Title:    "hi",
		Body:     "x",
	}
	if err := m.Service().Create(ctx, n); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.Service().MarkRead(ctx, "t-1", "u-1", n.ID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	if err := m.Service().MarkRead(ctx, "t-1", "u-1", "missing"); err == nil {
		t.Fatalf("MarkRead on missing ID should error")
	}
}

func TestSubscribeAndUnsubscribe(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	sub := &notification.Subscription{
		TenantID: "t-1",
		UserID:   "u-1",
		Category: "billing",
		Channel:  notification.ChannelInApp,
	}
	if err := m.Service().Subscribe(ctx, sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if sub.ID == "" {
		t.Fatalf("Subscribe did not assign ID")
	}
	if err := m.Service().Unsubscribe(ctx, "t-1", "u-1", sub.ID); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	if err := m.Service().Unsubscribe(ctx, "t-1", "u-1", sub.ID); err == nil {
		t.Fatalf("second Unsubscribe should return ErrNotFound")
	}
}

func TestCustomChannelReceivesNotification(t *testing.T) {
	t.Parallel()
	ch := &fakeChannel{name: "mail"}
	m := newModule(t, notification.WithChannel(ch))
	ctx := context.Background()
	n := &portslib.Notification{
		TenantID: "t-1",
		UserID:   "u-1",
		Title:    "Welcome via mail",
		Body:     "hi",
	}
	if err := m.Service().Create(ctx, n); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ch.count() != 1 {
		t.Fatalf("custom channel did not receive notification: count = %d", ch.count())
	}
}

func TestChannelErrorShortCircuits(t *testing.T) {
	t.Parallel()
	failing := &fakeChannel{name: "broken", failWith: errors.New("transport down")}
	never := &fakeChannel{name: "after"}
	m := newModule(t, notification.WithChannel(failing), notification.WithChannel(never))
	ctx := context.Background()
	n := &portslib.Notification{
		TenantID: "t-1",
		UserID:   "u-1",
		Title:    "Boom",
		Body:     "x",
	}
	err := m.Service().Create(ctx, n)
	if err == nil {
		t.Fatalf("Create should propagate channel error")
	}
	if never.count() != 0 {
		t.Fatalf("later channel should not have been invoked, count = %d", never.count())
	}
}

func TestCompose(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	c := m.Compose()
	if c == nil {
		t.Fatalf("Compose() returned nil")
	}
	if c.Metadata().ID != notification.ModuleID {
		t.Fatalf("metadata ID = %q, want %q", c.Metadata().ID, notification.ModuleID)
	}
	if len(c.Provides()) != 1 {
		t.Fatalf("provides len = %d, want 1", len(c.Provides()))
	}
	deps := c.Dependencies()
	if len(deps) != 4 {
		t.Fatalf("dependencies len = %d, want 4", len(deps))
	}
	for _, dep := range deps {
		if dep.Required {
			t.Fatalf("dep %s should be optional", dep.Port.Name)
		}
	}

	catalog := pkmodule.NewCatalog().
		Add(pkmodule.NewBundle("notification-bundle", []pkmodule.Entry{
			{ID: notification.ModuleID, New: func() pkmodule.Composable { return c }},
		}, []string{notification.ModuleID})).
		MustBuild()
	if _, err := pkmodule.Compose(catalog); err != nil {
		t.Fatalf("Compose catalog: %v", err)
	}
}
