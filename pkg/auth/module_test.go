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
	"path/filepath"
	"sync"
	"testing"
	"time"

	pkmodule "github.com/septagon-oss/pk-core/pkg/module"
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
	mu    sync.Mutex
	byID  map[string]*user.User
	byKey map[string]*user.User // tenantID + "|" + email
}

func newFakeUserReader() *fakeUserReader {
	return &fakeUserReader{
		byID:  map[string]*user.User{},
		byKey: map[string]*user.User{},
	}
}

func (f *fakeUserReader) add(u *user.User) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byID[u.ID] = u
	f.byKey[u.TenantID+"|"+u.Email] = u
}

func (f *fakeUserReader) Get(_ context.Context, id string) (*user.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.byID[id]; ok {
		return u, nil
	}
	return nil, errors.New("user not found")
}

func (f *fakeUserReader) GetByEmail(_ context.Context, tenantID, email string) (*user.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.byKey[tenantID+"|"+email]; ok {
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
	_, err := auth.NewModule(auth.WithUserReader(newFakeUserReader()))
	if err == nil {
		t.Fatalf("NewModule() with no session store should error")
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
	for i := 0; i < 3; i++ {
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

func TestPermissivePolicyAllowsAlways(t *testing.T) {
	t.Parallel()
	p := auth.PermissiveLoginPolicy()
	if err := p.AllowLogin(context.Background(), "t-1", "alice@example.test"); err != nil {
		t.Fatalf("PermissiveLoginPolicy.AllowLogin returned %v", err)
	}
	// Record calls should be inert and never panic.
	p.RecordFailure(context.Background(), "t-1", "alice@example.test")
	p.RecordSuccess(context.Background(), "t-1", "alice@example.test")
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
				return pkmodule.Must(pkmodule.Metadata{ID: "user_management", Version: "0.0.0"},
					pkmodule.WithProvides(pkmodule.Provide[user.UserBoundaryReader]("0.0.0")),
				)
			}},
		}, []string{auth.ModuleID, "user_management"})).
		MustBuild()
	if _, err := pkmodule.Compose(catalog); err != nil {
		t.Fatalf("Compose catalog: %v", err)
	}
}
