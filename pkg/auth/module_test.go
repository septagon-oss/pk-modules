// Validates: REQ-AUTH-001.
// Per: ADR-0017.
// Discipline: C-14.

package auth_test

// module_test.go validates the auth_management module against its public
// API. Tests live in auth_test to ensure the OSS contract is exercised the
// way callers see it.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/septagon-oss/pk-modules/pkg/portslib"
	"time"

	pkmodule "github.com/septagon-oss/pk-core/pkg/module"
	"github.com/septagon-oss/pk-core/pkg/security/identity"
	"github.com/septagon-oss/pk-core/pkg/security/passhash"

	"github.com/septagon-oss/pk-modules/pkg/auth"
	"github.com/septagon-oss/pk-modules/pkg/user"

	_ "modernc.org/sqlite"
)

func sqliteDSN(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return "file:" + filepath.Join(dir, "auth.db") + "?_pragma=journal_mode(WAL)"
}

// fakeUserReader is an in-memory user.UserBoundaryReader used by the auth
// tests. It allows us to control PassHash, Active, and lookup behaviors
// without spinning up the full user_management module.
type fakeUserReader struct {
	mu         sync.Mutex
	byID       map[string]*user.User
	byEmail    map[string]*user.User // tenantID + "|" + email
	byUsername map[string]*user.User // tenantID + "|" + username
}

func newFakeUserReader() *fakeUserReader {
	return &fakeUserReader{
		byID:       map[string]*user.User{},
		byEmail:    map[string]*user.User{},
		byUsername: map[string]*user.User{},
	}
}

func (f *fakeUserReader) add(u *user.User) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byID[u.ID] = u
	if u.Email != "" {
		f.byEmail[u.TenantID+"|"+u.Email] = u
	}
	if u.Username != "" {
		f.byUsername[u.TenantID+"|"+u.Username] = u
	}
}

func (f *fakeUserReader) Get(_ context.Context, tenantID, id string) (*user.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.byID[id]; ok && u.TenantID == tenantID {
		return u, nil
	}
	return nil, errors.New("user not found")
}

func (f *fakeUserReader) GetByEmail(_ context.Context, tenantID, email string) (*user.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.byEmail[tenantID+"|"+email]; ok {
		return u, nil
	}
	return nil, errors.New("user not found")
}

func (f *fakeUserReader) GetByUsername(_ context.Context, tenantID, username string) (*user.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.byUsername[tenantID+"|"+username]; ok {
		return u, nil
	}
	return nil, errors.New("user not found")
}

// makeUserWithPassword constructs a fake user.User with PassHash set to the
// bcrypt hash of plaintext. The hasher cost is intentionally the bcrypt
// minimum to keep tests fast.
func makeUserWithPassword(t *testing.T, h passhash.Hasher, tenant, id, email, plaintext string, active bool) *user.User {
	t.Helper()
	encoded, err := h.Hash(plaintext)
	if err != nil {
		t.Fatalf("hasher.Hash: %v", err)
	}
	return &user.User{
		ID:       id,
		TenantID: tenant,
		Email:    email,
		Username: id,
		PassHash: encoded,
		Active:   active,
	}
}

func newHasher(t *testing.T) passhash.Hasher {
	t.Helper()
	h, err := passhash.NewBcrypt(passhash.MinCost)
	if err != nil {
		t.Fatalf("NewBcrypt: %v", err)
	}
	return h
}

func newModule(t *testing.T, opts ...auth.Option) (*auth.Module, *fakeUserReader, passhash.Hasher) {
	t.Helper()
	reader := newFakeUserReader()
	hasher := newHasher(t)
	base := []auth.Option{
		auth.WithSQLiteDSN(sqliteDSN(t)),
		auth.WithUserReader(reader),
		auth.WithHasher(hasher),
		auth.WithLoginPolicy(&recordingPolicy{}),
	}
	base = append(base, opts...)
	m, err := auth.NewModule(base...)
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}
	return m, reader, hasher
}

func TestNewModuleDefaults(t *testing.T) {
	t.Parallel()
	m, _, _ := newModule(t)
	if m.Service() == nil {
		t.Fatalf("Service() is nil")
	}
	if m.HTTPHandler() == nil {
		t.Fatalf("HTTPHandler() is nil")
	}
	if m.Sessions() == nil {
		t.Fatalf("Sessions() is nil")
	}
	if m.Hasher() == nil {
		t.Fatalf("Hasher() is nil")
	}
	if m.SessionTTL() <= 0 {
		t.Fatalf("SessionTTL() = %s, want > 0", m.SessionTTL())
	}
}

func TestNewModuleRequiresSessionStore(t *testing.T) {
	t.Parallel()
	_, err := auth.NewModule(auth.WithUserReader(newFakeUserReader()), auth.WithLoginPolicy(&recordingPolicy{}))
	if err == nil {
		t.Fatalf("NewModule() with no session store should error")
	}
}

func TestNewModuleRequiresUserReader(t *testing.T) {
	t.Parallel()
	_, err := auth.NewModule(auth.WithSQLiteDSN(sqliteDSN(t)), auth.WithLoginPolicy(&recordingPolicy{}))
	if err == nil {
		t.Fatalf("NewModule() with no user reader should error")
	}
	if !strings.Contains(err.Error(), "user reader") {
		t.Fatalf("err = %v, want it to mention 'user reader'", err)
	}
}

func TestLoginWithCorrectPassword(t *testing.T) {
	t.Parallel()
	m, reader, hasher := newModule(t)
	reader.add(makeUserWithPassword(t, hasher, "t-1", "u-1", "alice@example.test", "supersecret", true))

	sess, err := m.Service().Login(context.Background(), "t-1", auth.Credentials{
		Email:    "alice@example.test",
		Password: "supersecret",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if sess == nil {
		t.Fatalf("Login returned nil session")
	}
	if sess.ID == "" {
		t.Fatalf("Login did not assign session ID")
	}
	if sess.UserID != "u-1" || sess.TenantID != "t-1" {
		t.Fatalf("Login returned wrong user/tenant: %+v", sess)
	}
	if !sess.ExpiresAt.After(sess.IssuedAt) {
		t.Fatalf("expires_at not after issued_at: %+v", sess)
	}
}

func TestAuthLoginWithUsernameSucceeds(t *testing.T) {
	t.Parallel()
	m, reader, hasher := newModule(t)
	u := makeUserWithPassword(t, hasher, "t-1", "u-1", "alice@example.test", "supersecret", true)
	u.Username = "alice"
	reader.add(u)

	sess, err := m.Service().Login(context.Background(), "t-1", auth.Credentials{
		Username: "alice",
		Password: "supersecret",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if sess == nil {
		t.Fatalf("Login returned nil session")
	}
	if sess.UserID != "u-1" || sess.TenantID != "t-1" {
		t.Fatalf("Login returned wrong user/tenant: %+v", sess)
	}
}

func TestAuthLoginWithUnknownUsernameFails(t *testing.T) {
	t.Parallel()
	m, _, _ := newModule(t)

	_, err := m.Service().Login(context.Background(), "t-1", auth.Credentials{
		Username: "ghost",
		Password: "supersecret",
	})
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("Login err = %v, want ErrInvalidCredentials", err)
	}
}

func TestLoginWithWrongPasswordFails(t *testing.T) {
	t.Parallel()
	m, reader, hasher := newModule(t)
	reader.add(makeUserWithPassword(t, hasher, "t-1", "u-1", "alice@example.test", "supersecret", true))

	_, err := m.Service().Login(context.Background(), "t-1", auth.Credentials{
		Email:    "alice@example.test",
		Password: "WRONGPASS",
	})
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("Login err = %v, want ErrInvalidCredentials", err)
	}
}

func TestLoginWithUnknownUserFails(t *testing.T) {
	t.Parallel()
	m, _, _ := newModule(t)

	_, err := m.Service().Login(context.Background(), "t-1", auth.Credentials{
		Email:    "ghost@example.test",
		Password: "any",
	})
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("Login err = %v, want ErrInvalidCredentials", err)
	}
}

func TestLoginInactiveUserFails(t *testing.T) {
	t.Parallel()
	m, reader, hasher := newModule(t)
	reader.add(makeUserWithPassword(t, hasher, "t-1", "u-1", "alice@example.test", "supersecret", false))

	_, err := m.Service().Login(context.Background(), "t-1", auth.Credentials{
		Email:    "alice@example.test",
		Password: "supersecret",
	})
	if !errors.Is(err, auth.ErrUserInactive) {
		t.Fatalf("Login err = %v, want ErrUserInactive", err)
	}
}

func TestValidateSessionValid(t *testing.T) {
	t.Parallel()
	m, reader, hasher := newModule(t)
	reader.add(makeUserWithPassword(t, hasher, "t-1", "u-1", "alice@example.test", "supersecret", true))

	sess, err := m.Service().Login(context.Background(), "t-1", auth.Credentials{
		Email:    "alice@example.test",
		Password: "supersecret",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	got, err := m.Service().ValidateSession(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	if got.ID != sess.ID || got.UserID != sess.UserID {
		t.Fatalf("ValidateSession returned %+v, want %+v", got, sess)
	}
}

func TestValidateExpiredSessionFails(t *testing.T) {
	t.Parallel()
	m, reader, hasher := newModule(t, auth.WithSessionTTL(1*time.Millisecond))
	reader.add(makeUserWithPassword(t, hasher, "t-1", "u-1", "alice@example.test", "supersecret", true))

	sess, err := m.Service().Login(context.Background(), "t-1", auth.Credentials{
		Email:    "alice@example.test",
		Password: "supersecret",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	// Sleep past the TTL.
	time.Sleep(10 * time.Millisecond)
	_, err = m.Service().ValidateSession(context.Background(), sess.ID)
	if !errors.Is(err, auth.ErrSessionExpired) {
		t.Fatalf("ValidateSession err = %v, want ErrSessionExpired", err)
	}
}

func TestValidateRevokedSessionFails(t *testing.T) {
	t.Parallel()
	m, reader, hasher := newModule(t)
	reader.add(makeUserWithPassword(t, hasher, "t-1", "u-1", "alice@example.test", "supersecret", true))

	sess, err := m.Service().Login(context.Background(), "t-1", auth.Credentials{
		Email:    "alice@example.test",
		Password: "supersecret",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if err := m.Service().Logout(context.Background(), sess.ID); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	_, err = m.Service().ValidateSession(context.Background(), sess.ID)
	if !errors.Is(err, auth.ErrSessionRevoked) {
		t.Fatalf("ValidateSession err = %v, want ErrSessionRevoked", err)
	}
}

// TestSessionHandlerSelfOwnership is the v0.2.1 regression for the session
// oracle / forced-logout finding: GET and DELETE on /api/v1/auth/sessions/{id}
// must be limited to the session's owner. An anonymous caller is 401; an
// authenticated caller asking about someone else's session gets 404; only the
// owner sees or revokes it.
func TestSessionHandlerSelfOwnership(t *testing.T) {
	t.Parallel()
	m, reader, hasher := newModule(t)
	reader.add(makeUserWithPassword(t, hasher, "t-1", "u-1", "alice@example.test", "pw-alice-123", true))
	reader.add(makeUserWithPassword(t, hasher, "t-1", "u-2", "bob@example.test", "pw-bob-12345", true))
	ctx := context.Background()

	victim, err := m.Service().Login(ctx, "t-1", auth.Credentials{Email: "alice@example.test", Password: "pw-alice-123"})
	if err != nil {
		t.Fatalf("login alice: %v", err)
	}

	do := func(method, subject string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, auth.APIPath+"/"+encodeID(t, victim.ID), nil)
		if subject != "" {
			req = req.WithContext(identity.ContextWithPrincipal(req.Context(),
				identity.Principal{Subject: subject, TenantID: "t-1", AuthMethod: "session"}))
		}
		rec := httptest.NewRecorder()
		m.HTTPHandler().ServeHTTP(rec, req)
		return rec
	}

	// GET: anonymous → 401; another user → 404; owner → 200.
	if rec := do(http.MethodGet, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous validate = %d, want 401", rec.Code)
	}
	if rec := do(http.MethodGet, "u-2"); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-user validate = %d, want 404 (session oracle must be closed)", rec.Code)
	}
	if rec := do(http.MethodGet, "u-1"); rec.Code != http.StatusOK {
		t.Fatalf("owner validate = %d, want 200", rec.Code)
	}

	// DELETE: anonymous → 401; another user → 404 and the session survives.
	if rec := do(http.MethodDelete, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous logout = %d, want 401", rec.Code)
	}
	if rec := do(http.MethodDelete, "u-2"); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-user logout = %d, want 404 (forced logout must be blocked)", rec.Code)
	}
	if _, err := m.Service().ValidateSession(ctx, victim.ID); err != nil {
		t.Fatalf("victim session was revoked by a non-owner: %v", err)
	}
	// Owner can revoke.
	if rec := do(http.MethodDelete, "u-1"); rec.Code != http.StatusNoContent {
		t.Fatalf("owner logout = %d, want 204", rec.Code)
	}
}

func TestLogoutRevokes(t *testing.T) {
	t.Parallel()
	m, reader, hasher := newModule(t)
	reader.add(makeUserWithPassword(t, hasher, "t-1", "u-1", "alice@example.test", "supersecret", true))
	ctx := context.Background()

	sess, err := m.Service().Login(ctx, "t-1", auth.Credentials{
		Email:    "alice@example.test",
		Password: "supersecret",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if err := m.Service().Logout(ctx, sess.ID); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	// Idempotency: a second logout should not return an error.
	if err := m.Service().Logout(ctx, sess.ID); err != nil {
		t.Fatalf("Logout (second call) returned err: %v", err)
	}
}

func TestInvalidateAllSessionsRevokesAll(t *testing.T) {
	t.Parallel()
	m, reader, hasher := newModule(t)
	reader.add(makeUserWithPassword(t, hasher, "t-1", "u-1", "alice@example.test", "supersecret", true))
	ctx := context.Background()

	// Create several sessions for the same user.
	sessions := make([]*auth.Session, 0, 3)
	for i := range 3 {
		s, err := m.Service().Login(ctx, "t-1", auth.Credentials{
			Email:    "alice@example.test",
			Password: "supersecret",
		})
		if err != nil {
			t.Fatalf("Login %d: %v", i, err)
		}
		sessions = append(sessions, s)
	}
	if err := m.Service().InvalidateAllSessions(ctx, "u-1"); err != nil {
		t.Fatalf("InvalidateAllSessions: %v", err)
	}
	for i, s := range sessions {
		if _, err := m.Service().ValidateSession(ctx, s.ID); !errors.Is(err, auth.ErrSessionRevoked) {
			t.Fatalf("session %d still valid after InvalidateAllSessions: err=%v", i, err)
		}
	}
}

func TestSessionTTLEnforced(t *testing.T) {
	t.Parallel()
	ttl := 30 * time.Minute
	m, reader, hasher := newModule(t, auth.WithSessionTTL(ttl))
	reader.add(makeUserWithPassword(t, hasher, "t-1", "u-1", "alice@example.test", "supersecret", true))

	sess, err := m.Service().Login(context.Background(), "t-1", auth.Credentials{
		Email:    "alice@example.test",
		Password: "supersecret",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	delta := sess.ExpiresAt.Sub(sess.IssuedAt)
	if delta < ttl-time.Second || delta > ttl+time.Second {
		t.Fatalf("session lifetime = %s, want ~%s", delta, ttl)
	}
}

// recordingPolicy is a LoginPolicy that records every call so the test can
// assert RecordSuccess/RecordFailure semantics.
type recordingPolicy struct {
	allowErr  error
	successes int
	failures  int
}

func (p *recordingPolicy) AllowLogin(context.Context, string, string) error {
	return p.allowErr
}

func (p *recordingPolicy) RecordSuccess(context.Context, string, string) { p.successes++ }
func (p *recordingPolicy) RecordFailure(context.Context, string, string) { p.failures++ }

func TestNewModuleRequiresLoginPolicy(t *testing.T) {
	t.Parallel()
	_, err := auth.NewModule(
		auth.WithSQLiteDSN(sqliteDSN(t)),
		auth.WithUserReader(newFakeUserReader()),
		auth.WithHasher(newHasher(t)),
	)
	if err == nil {
		t.Fatal("NewModule must fail closed without a login policy")
	}
	if !strings.Contains(err.Error(), "login policy") {
		t.Fatalf("err = %v, want it to mention 'login policy'", err)
	}
}

func TestPolicyRecordsHits(t *testing.T) {
	t.Parallel()
	policy := &recordingPolicy{}
	m, reader, hasher := newModule(t, auth.WithLoginPolicy(policy))
	reader.add(makeUserWithPassword(t, hasher, "t-1", "u-1", "alice@example.test", "supersecret", true))

	if _, err := m.Service().Login(context.Background(), "t-1", auth.Credentials{
		Email:    "alice@example.test",
		Password: "WRONG",
	}); err == nil {
		t.Fatalf("Login expected to fail")
	}
	if policy.failures == 0 {
		t.Fatalf("policy.failures = 0, want >=1")
	}
	if _, err := m.Service().Login(context.Background(), "t-1", auth.Credentials{
		Email:    "alice@example.test",
		Password: "supersecret",
	}); err != nil {
		t.Fatalf("Login expected to succeed: %v", err)
	}
	if policy.successes == 0 {
		t.Fatalf("policy.successes = 0, want >=1")
	}
}

func TestPolicyDeniedBlocksLogin(t *testing.T) {
	t.Parallel()
	policy := &recordingPolicy{allowErr: fmt.Errorf("locked")}
	m, reader, hasher := newModule(t, auth.WithLoginPolicy(policy))
	reader.add(makeUserWithPassword(t, hasher, "t-1", "u-1", "alice@example.test", "supersecret", true))

	_, err := m.Service().Login(context.Background(), "t-1", auth.Credentials{
		Email:    "alice@example.test",
		Password: "supersecret",
	})
	if !errors.Is(err, auth.ErrPolicyDenied) {
		t.Fatalf("Login err = %v, want ErrPolicyDenied", err)
	}
}

func TestCompose(t *testing.T) {
	t.Parallel()
	m, _, _ := newModule(t)
	c := m.Compose()
	if c == nil {
		t.Fatalf("Compose() returned nil")
	}
	if c.Metadata().ID != auth.ModuleID {
		t.Fatalf("metadata ID = %q, want %q", c.Metadata().ID, auth.ModuleID)
	}
	if len(c.Provides()) != 1 {
		t.Fatalf("provides len = %d, want 1", len(c.Provides()))
	}
	deps := c.Dependencies()
	if len(deps) < 1 {
		t.Fatalf("dependencies len = %d, want >= 1", len(deps))
	}
	foundRequired := false
	for _, dep := range deps {
		if dep.Required {
			foundRequired = true
		}
	}
	if !foundRequired {
		t.Fatalf("auth_management should declare at least one required dependency (UserBoundaryReader)")
	}
}

func TestMigrationsEmbedded(t *testing.T) {
	t.Parallel()
	m, _, _ := newModule(t)
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

func TestComposeViaCatalog(t *testing.T) {
	t.Parallel()
	m, _, _ := newModule(t)
	c := m.Compose()
	// Smoke test that the Composable round-trips through the catalog without
	// blowing up the dependency validator (UserBoundaryReader is required so
	// we also stub it via a sibling Composable).
	catalog := pkmodule.NewCatalog().
		Add(pkmodule.NewBundle("auth-bundle", []pkmodule.Entry{
			{ID: auth.ModuleID, New: func() pkmodule.Composable { return c }},
			{ID: "user_management", New: func() pkmodule.Composable {
				return pkmodule.Must(
					pkmodule.Metadata{ID: "user_management", Version: "0.0.0"},
					pkmodule.WithProvides(pkmodule.Provide[user.UserBoundaryReader]("0.0.0")),
				)
			}},
		}, []string{auth.ModuleID, "user_management"})).
		MustBuild()
	if _, err := pkmodule.Compose(catalog); err != nil {
		t.Fatalf("Compose catalog: %v", err)
	}
}

// encodeID renders an entity id as the canonical opaque path segment the
// handlers require, which is the same form pk-client puts on the wire.
func encodeID(t *testing.T, id string) string {
	t.Helper()
	segment, ok := portslib.EncodeEntityID(id)
	if !ok {
		t.Fatalf("encode entity id %q", id)
	}
	return segment
}
