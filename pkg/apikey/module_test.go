// Validates: REQ-APIKEY-001.
// Per: ADR-0017.
// Discipline: C-14.

package apikey_test

import (
	"context"
	"errors"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	pkmodule "github.com/septagon-oss/pk-core/pkg/module"
	"github.com/septagon-oss/pk-core/pkg/security/identity"
	"github.com/septagon-oss/pk-core/pkg/security/passhash"

	"github.com/septagon-oss/pk-modules/pkg/apikey"

	_ "modernc.org/sqlite"
)

// fakeAuditEmitter is a test double that records every Emit call. It
// satisfies audit.AuditEmitter without importing the real audit module's
// store-backed implementation.
type fakeAuditEmitter struct {
	mu    sync.Mutex
	calls []auditCall
}

type auditCall struct {
	Action   string
	Resource string
	Details  map[string]any
}

func (f *fakeAuditEmitter) Emit(_ context.Context, action, resource string, details map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Copy details so subsequent mutations by the service don't race with
	// the assertion reads.
	cp := make(map[string]any, len(details))
	maps.Copy(cp, details)
	f.calls = append(f.calls, auditCall{Action: action, Resource: resource, Details: cp})
	return nil
}

func (f *fakeAuditEmitter) Calls() []auditCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]auditCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func sqliteDSN(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return "file:" + filepath.Join(dir, "apikeys.db") + "?_pragma=journal_mode(WAL)"
}

func fastHasher(t *testing.T) passhash.Hasher {
	t.Helper()
	h, err := passhash.NewBcrypt(passhash.MinCost)
	if err != nil {
		t.Fatalf("NewBcrypt: %v", err)
	}
	return h
}

func newModule(t *testing.T, opts ...apikey.Option) *apikey.Module {
	t.Helper()
	base := []apikey.Option{
		apikey.WithSQLiteDSN(sqliteDSN(t)),
		apikey.WithHasher(fastHasher(t)),
	}
	base = append(base, opts...)
	m, err := apikey.NewModule(base...)
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
	if m.Authenticator() == nil {
		t.Fatalf("Authenticator() is nil")
	}
	if m.HTTPHandler() == nil {
		t.Fatalf("HTTPHandler() is nil")
	}
	if m.Store() == nil {
		t.Fatalf("Store() is nil")
	}
	if m.Hasher() == nil {
		t.Fatalf("Hasher() is nil")
	}
}

func TestNewModuleRequiresStore(t *testing.T) {
	t.Parallel()
	_, err := apikey.NewModule()
	if err == nil {
		t.Fatalf("NewModule() with no store should error")
	}
}

func TestIssueProducesPlaintextAndStoresHash(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()

	plaintext, key, err := m.Service().Issue(ctx, "t-1", "u-1", "ci-bot", []string{"read", "write"}, 0)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if plaintext == "" {
		t.Fatalf("Issue returned empty plaintext")
	}
	if !strings.HasPrefix(plaintext, "pk_") {
		t.Fatalf("plaintext %q missing pk_ prefix", plaintext)
	}
	if key.Hash == "" {
		t.Fatalf("Issue did not set Hash")
	}
	if key.Hash == plaintext {
		t.Fatalf("Hash equals plaintext — bcrypt not applied")
	}
	if len(key.Scopes) != 2 || key.Scopes[0] != "read" || key.Scopes[1] != "write" {
		t.Fatalf("Issue returned wrong scopes: %+v", key.Scopes)
	}
	if key.Prefix == "" || !strings.Contains(plaintext, key.Prefix) {
		t.Fatalf("Issue prefix %q not embedded in plaintext %q", key.Prefix, plaintext)
	}
}

func TestVerifyAcceptsCorrectKey(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	plaintext, issued, err := m.Service().Issue(ctx, "t-1", "u-1", "ci-bot", []string{"read"}, 0)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	got, err := m.Service().Verify(ctx, plaintext)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.ID != issued.ID {
		t.Fatalf("Verify ID = %q, want %q", got.ID, issued.ID)
	}
	if got.UserID != "u-1" || got.TenantID != "t-1" {
		t.Fatalf("Verify returned wrong tenant/user: %+v", got)
	}
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	_, _, err := m.Service().Issue(ctx, "t-1", "u-1", "ci-bot", nil, 0)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	_, err = m.Service().Verify(ctx, "pk_thisisdefinitelynotthecorrectkey1234567890")
	if !errors.Is(err, apikey.ErrInvalidKey) {
		t.Fatalf("Verify err = %v, want ErrInvalidKey", err)
	}
}

func TestVerifyRejectsRevokedKey(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	plaintext, issued, err := m.Service().Issue(ctx, "t-1", "u-1", "ci-bot", nil, 0)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := m.Service().Revoke(ctx, issued.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	_, err = m.Service().Verify(ctx, plaintext)
	if !errors.Is(err, apikey.ErrKeyRevoked) {
		t.Fatalf("Verify err = %v, want ErrKeyRevoked", err)
	}
}

func TestVerifyRejectsExpiredKey(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	plaintext, _, err := m.Service().Issue(ctx, "t-1", "u-1", "ci-bot", nil, 1*time.Millisecond)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	_, err = m.Service().Verify(ctx, plaintext)
	if !errors.Is(err, apikey.ErrKeyExpired) {
		t.Fatalf("Verify err = %v, want ErrKeyExpired", err)
	}
}

func TestRevokeKeyMarksRevoked(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	_, issued, err := m.Service().Issue(ctx, "t-1", "u-1", "ci-bot", nil, 0)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := m.Service().Revoke(ctx, issued.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	keys, err := m.Service().List(ctx, "t-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("List returned %d keys, want 1", len(keys))
	}
	if keys[0].RevokedAt == nil {
		t.Fatalf("expected RevokedAt to be set after Revoke")
	}
}

func TestListByTenantReturnsAll(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	for i := range 3 {
		if _, _, err := m.Service().Issue(ctx, "t-1", "u-1", "bot", nil, 0); err != nil {
			t.Fatalf("Issue %d: %v", i, err)
		}
	}
	if _, _, err := m.Service().Issue(ctx, "t-2", "u-2", "bot", nil, 0); err != nil {
		t.Fatalf("Issue cross-tenant: %v", err)
	}
	keys, err := m.Service().List(ctx, "t-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 3 {
		t.Fatalf("List returned %d keys for t-1, want 3", len(keys))
	}
	for _, k := range keys {
		if k.TenantID != "t-1" {
			t.Fatalf("List leaked key from %q into t-1", k.TenantID)
		}
	}
}

func TestLastUsedAtUpdatesOnVerify(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	plaintext, issued, err := m.Service().Issue(ctx, "t-1", "u-1", "ci", nil, 0)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if issued.LastUsedAt != nil {
		t.Fatalf("LastUsedAt should be nil at issuance")
	}
	if _, err := m.Service().Verify(ctx, plaintext); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	keys, err := m.Service().List(ctx, "t-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("List returned %d keys, want 1", len(keys))
	}
	if keys[0].LastUsedAt == nil {
		t.Fatalf("LastUsedAt should be set after Verify")
	}
}

func TestAuthenticatorMiddlewareSetsPrincipal(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	plaintext, _, err := m.Service().Issue(ctx, "t-1", "u-1", "ci", []string{"read"}, 0)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	var captured identity.Principal
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = identity.PrincipalFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	server := m.Authenticator().Middleware()(next)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("middleware status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if captured.Subject != "u-1" {
		t.Fatalf("Subject = %q, want u-1", captured.Subject)
	}
	if captured.TenantID != "t-1" {
		t.Fatalf("TenantID = %q, want t-1", captured.TenantID)
	}
	if captured.AuthMethod != "api_key" {
		t.Fatalf("AuthMethod = %q, want api_key", captured.AuthMethod)
	}
	if !captured.HasScope("read") {
		t.Fatalf("HasScope(read) = false, want true: %+v", captured.Scopes)
	}
}

func TestAuthenticatorRejectsMissingBearer(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatalf("next handler should not run when bearer is missing")
		w.WriteHeader(http.StatusOK)
	})
	server := m.Authenticator().Middleware()(next)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("middleware status = %d, want 401", rec.Code)
	}
}

func TestAuthenticatorRejectsInvalidBearer(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatalf("next handler should not run when bearer is invalid")
	})
	server := m.Authenticator().Middleware()(next)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer pk_notavalidkey")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("middleware status = %d, want 401", rec.Code)
	}
}

func TestIssueEmitsAuditEvent(t *testing.T) {
	t.Parallel()
	emitter := &fakeAuditEmitter{}
	m := newModule(t, apikey.WithAuditEmitter(emitter))
	ctx := context.Background()
	_, issued, err := m.Service().Issue(ctx, "t-1", "u-1", "ci-bot", []string{"read"}, 0)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	calls := emitter.Calls()
	if len(calls) != 1 {
		t.Fatalf("emitter calls = %d, want 1: %+v", len(calls), calls)
	}
	c := calls[0]
	if c.Action != "apikey.issued" {
		t.Fatalf("action = %q, want apikey.issued", c.Action)
	}
	if c.Resource != "apikey:"+issued.ID {
		t.Fatalf("resource = %q, want apikey:%s", c.Resource, issued.ID)
	}
	if c.Details["tenant_id"] != "t-1" {
		t.Fatalf("details.tenant_id = %v, want t-1", c.Details["tenant_id"])
	}
	if c.Details["user_id"] != "u-1" {
		t.Fatalf("details.user_id = %v, want u-1", c.Details["user_id"])
	}
	if c.Details["name"] != "ci-bot" {
		t.Fatalf("details.name = %v, want ci-bot", c.Details["name"])
	}
	if c.Details["prefix"] != issued.Prefix {
		t.Fatalf("details.prefix = %v, want %q", c.Details["prefix"], issued.Prefix)
	}
}

func TestRevokeEmitsAuditEvent(t *testing.T) {
	t.Parallel()
	emitter := &fakeAuditEmitter{}
	m := newModule(t, apikey.WithAuditEmitter(emitter))
	ctx := context.Background()
	_, issued, err := m.Service().Issue(ctx, "t-1", "u-1", "ci-bot", nil, 0)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := m.Service().Revoke(ctx, issued.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	calls := emitter.Calls()
	if len(calls) != 2 {
		t.Fatalf("emitter calls = %d, want 2: %+v", len(calls), calls)
	}
	c := calls[1]
	if c.Action != "apikey.revoked" {
		t.Fatalf("action = %q, want apikey.revoked", c.Action)
	}
	if c.Resource != "apikey:"+issued.ID {
		t.Fatalf("resource = %q, want apikey:%s", c.Resource, issued.ID)
	}
	if c.Details["id"] != issued.ID {
		t.Fatalf("details.id = %v, want %q", c.Details["id"], issued.ID)
	}
	if c.Details["tenant_id"] != "t-1" {
		t.Fatalf("details.tenant_id = %v, want t-1", c.Details["tenant_id"])
	}
}

func TestNilAuditEmitterIsAllowed(t *testing.T) {
	t.Parallel()
	// No WithAuditEmitter — the service must not panic on Issue/Revoke.
	m := newModule(t)
	ctx := context.Background()
	_, issued, err := m.Service().Issue(ctx, "t-1", "u-1", "ci-bot", nil, 0)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := m.Service().Revoke(ctx, issued.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
}

func TestCompose(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	c := m.Compose()
	if c == nil {
		t.Fatalf("Compose() returned nil")
	}
	if c.Metadata().ID != apikey.ModuleID {
		t.Fatalf("metadata ID = %q, want %q", c.Metadata().ID, apikey.ModuleID)
	}
	if len(c.Provides()) != 2 {
		t.Fatalf("provides len = %d, want 2", len(c.Provides()))
	}
	deps := c.Dependencies()
	if len(deps) < 1 {
		t.Fatalf("dependencies len = %d, want >=1", len(deps))
	}
	for _, dep := range deps {
		if dep.Required {
			t.Fatalf("api_key_management should declare only optional deps; got required %s", dep.Port.Name)
		}
	}

	catalog := pkmodule.NewCatalog().
		Add(pkmodule.NewBundle("apikey-bundle", []pkmodule.Entry{
			{ID: apikey.ModuleID, New: func() pkmodule.Composable { return c }},
		}, []string{apikey.ModuleID})).
		MustBuild()
	if _, err := pkmodule.Compose(catalog); err != nil {
		t.Fatalf("Compose catalog: %v", err)
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
