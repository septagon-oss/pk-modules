// Validates: REQ-AUDIT-001.
// Per: ADR-0017.
// Discipline: C-14.

package audit_test

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pkmodule "github.com/septagon-oss/pk-core/pkg/module"
	"github.com/septagon-oss/pk-core/pkg/security/identity"

	"github.com/septagon-oss/pk-modules/pkg/audit"

	_ "modernc.org/sqlite"
)

// withTenant attaches an authenticated principal scoped to tenantID so handler
// tests exercise the authorized path (the audit surface requires a tenant).
func withTenant(r *http.Request, tenantID string) *http.Request {
	return r.WithContext(identity.ContextWithPrincipal(r.Context(),
		identity.Principal{Subject: "u", TenantID: tenantID, AuthMethod: "session"}))
}

func sqliteDSN(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return "file:" + filepath.Join(dir, "audit.db") + "?_pragma=journal_mode(WAL)"
}

func newModule(t *testing.T) *audit.Module {
	t.Helper()
	m, err := audit.NewModule(audit.WithSQLiteDSN(sqliteDSN(t)))
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}
	return m
}

func TestNewModuleDefaults(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	if m.Service() == nil {
		t.Fatalf("Service() is nil")
	}
	if m.Reader() == nil {
		t.Fatalf("Reader() is nil")
	}
	if m.HTTPHandler() == nil {
		t.Fatalf("HTTPHandler() is nil")
	}
	if m.Store() == nil {
		t.Fatalf("Store() is nil")
	}
}

func TestRecordAndQuery(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()

	e := &audit.Event{TenantID: "t-1", Actor: "user-1", Action: "user.created", Resource: "user:1"}
	if err := m.Service().Record(ctx, e); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if e.ID == "" {
		t.Fatalf("Record did not assign ID")
	}
	if e.Severity != audit.SeverityInfo {
		t.Fatalf("Record did not default severity, got %q", e.Severity)
	}
	if e.EmittedAt.IsZero() {
		t.Fatalf("Record did not set EmittedAt")
	}

	got, err := m.Reader().Query(ctx, audit.QueryFilter{TenantID: "t-1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Query returned %d events, want 1", len(got))
	}
	if got[0].ID != e.ID {
		t.Fatalf("Query ID = %q, want %q", got[0].ID, e.ID)
	}
}

func TestQueryByActor(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	for _, actor := range []string{"a", "b", "a", "c"} {
		if err := m.Service().Record(ctx, &audit.Event{TenantID: "t-1", Actor: actor, Action: "x"}); err != nil {
			t.Fatalf("Record %q: %v", actor, err)
		}
	}
	got, err := m.Reader().Query(ctx, audit.QueryFilter{TenantID: "t-1", Actor: "a"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	for _, e := range got {
		if e.Actor != "a" {
			t.Fatalf("Actor = %q, want a", e.Actor)
		}
	}
}

func TestQueryByAction(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	for _, action := range []string{"user.created", "user.deleted", "user.created"} {
		if err := m.Service().Record(ctx, &audit.Event{TenantID: "t-1", Actor: "x", Action: action}); err != nil {
			t.Fatalf("Record %q: %v", action, err)
		}
	}
	got, err := m.Reader().Query(ctx, audit.QueryFilter{TenantID: "t-1", Action: "user.created"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
}

func TestQueryByTime(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()

	t0 := time.Now().UTC().Add(-2 * time.Hour)
	t1 := t0.Add(time.Hour)
	t2 := t0.Add(2 * time.Hour)

	for _, ts := range []time.Time{t0, t1, t2} {
		if err := m.Service().Record(ctx, &audit.Event{TenantID: "t-1", Actor: "x", Action: "y", EmittedAt: ts}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	got, err := m.Reader().Query(ctx, audit.QueryFilter{
		TenantID: "t-1",
		Since:    t0.Add(30 * time.Minute),
		Until:    t0.Add(90 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1; events = %+v", len(got), got)
	}
}

func TestMultiRecordOrder(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	t0 := time.Now().UTC()
	want := []string{}
	for i := range 4 {
		e := &audit.Event{
			TenantID:  "t-1",
			Actor:     "x",
			Action:    "y",
			EmittedAt: t0.Add(time.Duration(i) * time.Second),
		}
		if err := m.Service().Record(ctx, e); err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
		want = append(want, e.ID)
	}
	got, err := m.Reader().Query(ctx, audit.QueryFilter{TenantID: "t-1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("ascending order broken at %d: got %q, want %q", i, got[i].ID, id)
		}
	}
}

func TestRecordIDsUniqueUnderContention(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	const n = 200
	seen := make(map[string]struct{}, n)
	// Pin EmittedAt so the only differentiator is the random suffix; this
	// guarantees the test fails if generateID ever drops its random portion.
	pinned := time.Now().UTC()
	for i := range n {
		e := &audit.Event{
			TenantID:  "t-collision",
			Actor:     "x",
			Action:    "y",
			EmittedAt: pinned,
		}
		if err := m.Service().Record(ctx, e); err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
		if _, dup := seen[e.ID]; dup {
			t.Fatalf("duplicate ID at %d: %q", i, e.ID)
		}
		seen[e.ID] = struct{}{}
	}
	if len(seen) != n {
		t.Fatalf("len(seen) = %d, want %d", len(seen), n)
	}
}

func TestAuditEmitterDelegates(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	em := audit.EmitterFor(m.Service(), "t-1", "user-1", audit.SeverityWarning)
	err := em.Emit(ctx, "user.deleted", "user:42", map[string]any{"reason": "self-serve"})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	got, err := m.Reader().Query(ctx, audit.QueryFilter{TenantID: "t-1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	e := got[0]
	if e.Severity != audit.SeverityWarning {
		t.Fatalf("Severity = %q, want warning", e.Severity)
	}
	if e.Action != "user.deleted" {
		t.Fatalf("Action = %q", e.Action)
	}
	// Verify the JSON payload round-tripped.
	var payload map[string]any
	if err := json.Unmarshal([]byte(e.Details), &payload); err != nil {
		t.Fatalf("Details JSON: %v", err)
	}
	if payload["reason"] != "self-serve" {
		t.Fatalf("Details payload = %+v", payload)
	}
}

func TestValidationRejectsBadSeverity(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	err := m.Service().Record(ctx, &audit.Event{TenantID: "t-1", Action: "x", Severity: "panic"})
	if err == nil {
		t.Fatalf("Record with bad severity should error")
	}
}

func TestComposeReturnsValidComposable(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	c := m.Compose()
	if c == nil {
		t.Fatalf("Compose() returned nil")
	}
	if c.Metadata().ID != audit.ModuleID {
		t.Fatalf("metadata ID = %q, want %q", c.Metadata().ID, audit.ModuleID)
	}
	if len(c.Provides()) != 2 {
		t.Fatalf("provides len = %d, want 2", len(c.Provides()))
	}
	deps := c.Dependencies()
	if len(deps) != 2 {
		t.Fatalf("dependencies len = %d, want 2", len(deps))
	}
	for _, dep := range deps {
		if dep.Required {
			t.Fatalf("dep %s should be optional", dep.Port.Name)
		}
	}

	catalog := pkmodule.NewCatalog().
		Add(pkmodule.NewBundle("audit-bundle", []pkmodule.Entry{
			{ID: audit.ModuleID, New: func() pkmodule.Composable { return c }},
		}, []string{audit.ModuleID})).
		MustBuild()
	if _, err := pkmodule.Compose(catalog); err != nil {
		t.Fatalf("Compose catalog: %v", err)
	}
}

func TestHandlerListRejectsMalformedQueryParams(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	cases := []struct {
		name string
		raw  string
	}{
		{"bad since", "/api/v1/audit-events?since=not-a-time"},
		{"bad until", "/api/v1/audit-events?until=2026-13-99T00:00:00Z"},
		{"bad limit", "/api/v1/audit-events?limit=abc"},
		{"negative limit", "/api/v1/audit-events?limit=-3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := withTenant(httptest.NewRequest(http.MethodGet, tc.raw, nil), "t-1")
			rec := httptest.NewRecorder()
			m.HTTPHandler().ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandlerListAcceptsValidQueryParams(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	req := withTenant(httptest.NewRequest(http.MethodGet,
		"/api/v1/audit-events?since=2026-01-01T00:00:00Z&until=2027-01-01T00:00:00Z&limit=10",
		nil), "t-1")
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandlerListRequiresTenant proves the audit read surface rejects an
// anonymous (no-principal) request with 401 before it touches the store, so an
// unauthenticated caller can never read any tenant's audit trail.
func TestHandlerListRequiresTenant(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-events", nil)
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous audit query = %d, want 401", rec.Code)
	}
}

// TestHandlerRejectsWrites is the v0.2.2 regression for audit-log forgery over
// HTTP: the audit trail is append-only from trusted in-process emitters, so the
// HTTP surface must refuse every write verb with 405 rather than let a client
// fabricate or backdate an entry.
func TestHandlerRejectsWrites(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		req := withTenant(httptest.NewRequest(method, audit.APIPath,
			strings.NewReader(`{"action":"forged","target":"x"}`)), "t-1")
		rec := httptest.NewRecorder()
		m.HTTPHandler().ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s = %d, want 405 (audit log is read-only)", method, audit.APIPath, rec.Code)
		}
	}
}

func TestMigrationsEmbedded(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	migFS := m.Migrations()
	entries, err := fs.ReadDir(migFS, ".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Name() == "0001_initial.up.sql" {
			found = true
		}
	}
	if !found {
		t.Fatalf("migrations FS missing 0001_initial.up.sql; entries = %+v", entries)
	}
}
