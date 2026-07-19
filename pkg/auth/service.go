// Implements: REQ-AUTH-010.
// Per: ADR-0009.
// Discipline: C-14.

package auth

// service.go owns the default AuthService implementation. Pro embeds
// *service to compose SSO-aware lookups, MFA challenges, rate limiting,
// and risk scoring without reimplementing the password flow.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/septagon-oss/pk-core/pkg/security/passhash"

	"github.com/septagon-oss/pk-modules/pkg/audit"
	"github.com/septagon-oss/pk-modules/pkg/user"
)

// Sentinel errors returned by AuthService. Callers should not branch on
// the underlying passhash error to avoid leaking whether the user exists.
var (
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	ErrSessionExpired     = errors.New("auth: session expired")
	ErrSessionRevoked     = errors.New("auth: session revoked")
	ErrNoCredentials      = errors.New("auth: credentials require email or username")
	ErrUserInactive       = errors.New("auth: user is inactive")
	ErrPolicyDenied       = errors.New("auth: login policy denied")
	// ErrInvalidRequest marks a malformed login request the caller must fix
	// (missing tenant_id, missing identifier, or missing password). Handlers
	// map it to HTTP 400. It is distinct from ErrInvalidCredentials so that a
	// well-formed-but-wrong attempt (401) is never confused with a malformed
	// request, and so neither path leaks whether a user exists.
	ErrInvalidRequest = errors.New("auth: invalid request")
)

// service is the default AuthService implementation.
type service struct {
	sessions   SessionStore
	users      user.UserBoundaryReader
	hasher     passhash.Hasher
	policy     LoginPolicy
	audit      audit.AuditEmitter
	sessionTTL time.Duration
	now        func() time.Time
}

func newService(
	sessions SessionStore,
	users user.UserBoundaryReader,
	hasher passhash.Hasher,
	policy LoginPolicy,
	emitter audit.AuditEmitter,
	ttl time.Duration,
) *service {
	return &service{
		sessions:   sessions,
		users:      users,
		hasher:     hasher,
		policy:     policy,
		audit:      emitter,
		sessionTTL: ttl,
		now:        time.Now,
	}
}

// Login validates the credentials, creates and persists a session, and
// emits "auth.login_success" or "auth.login_failure" via the audit
// emitter if configured.
func (s *service) Login(ctx context.Context, tenantID string, creds Credentials) (*Session, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, fmt.Errorf("%w: tenant_id is required", ErrInvalidRequest)
	}
	identifier := creds.Identifier()
	if identifier == "" {
		return nil, fmt.Errorf("%w: email or username is required", ErrInvalidRequest)
	}
	if creds.Password == "" {
		return nil, fmt.Errorf("%w: password is required", ErrInvalidRequest)
	}
	if err := s.policy.AllowLogin(ctx, tenantID, identifier); err != nil {
		s.emitFailure(ctx, tenantID, identifier, "policy_denied")
		return nil, fmt.Errorf("%w: %v", ErrPolicyDenied, err)
	}

	u, err := s.lookupUser(ctx, tenantID, creds)
	if err != nil {
		s.policy.RecordFailure(ctx, tenantID, identifier)
		s.emitFailure(ctx, tenantID, identifier, "user_not_found")
		return nil, ErrInvalidCredentials
	}
	if !u.Active {
		s.policy.RecordFailure(ctx, tenantID, identifier)
		s.emitFailure(ctx, tenantID, identifier, "user_inactive")
		return nil, ErrUserInactive
	}
	if u.PassHash == "" {
		s.policy.RecordFailure(ctx, tenantID, identifier)
		s.emitFailure(ctx, tenantID, identifier, "no_password_set")
		return nil, ErrInvalidCredentials
	}
	if err := s.hasher.Verify(creds.Password, u.PassHash); err != nil {
		s.policy.RecordFailure(ctx, tenantID, identifier)
		s.emitFailure(ctx, tenantID, identifier, "password_mismatch")
		return nil, ErrInvalidCredentials
	}

	sessionID, err := generateSessionID()
	if err != nil {
		return nil, fmt.Errorf("auth: generate session id: %w", err)
	}
	now := s.now().UTC()
	sess := &Session{
		ID:        sessionID,
		UserID:    u.ID,
		TenantID:  tenantID,
		IssuedAt:  now,
		ExpiresAt: now.Add(s.sessionTTL),
	}
	if err := sess.Validate(); err != nil {
		return nil, err
	}
	if err := s.sessions.Create(ctx, sess); err != nil {
		return nil, fmt.Errorf("auth: persist session: %w", err)
	}
	s.policy.RecordSuccess(ctx, tenantID, identifier)
	s.emitSuccess(ctx, tenantID, u.ID, identifier, sess.ID)
	return sess, nil
}

// Logout revokes the supplied session ID. It is idempotent: revoking an
// already-revoked session returns nil.
func (s *service) Logout(ctx context.Context, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return errors.New("auth: session_id is required")
	}
	if err := s.sessions.Revoke(ctx, sessionID); err != nil {
		return err
	}
	if s.audit != nil {
		_ = s.audit.Emit(ctx, "auth.logout", "session:"+sessionID, map[string]any{
			"session_id": sessionID,
		})
	}
	return nil
}

// ValidateSession looks up sessionID and confirms it is neither revoked nor
// expired. Returns the live Session on success.
func (s *service) ValidateSession(ctx context.Context, sessionID string) (*Session, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("auth: session_id is required")
	}
	sess, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sess.RevokedAt != nil {
		return nil, ErrSessionRevoked
	}
	if !sess.ExpiresAt.IsZero() && s.now().UTC().After(sess.ExpiresAt) {
		return nil, ErrSessionExpired
	}
	return sess, nil
}

// InvalidateAllSessions revokes every live session belonging to userID.
func (s *service) InvalidateAllSessions(ctx context.Context, userID string) error {
	if strings.TrimSpace(userID) == "" {
		return errors.New("auth: user_id is required")
	}
	if err := s.sessions.RevokeByUser(ctx, userID); err != nil {
		return err
	}
	if s.audit != nil {
		_ = s.audit.Emit(ctx, "auth.invalidate_all", "user:"+userID, map[string]any{
			"user_id": userID,
		})
	}
	return nil
}

// lookupUser resolves a user by the credential carrier. Both email and
// username are supported in v0.1.0; Pro replaces lookupUser to layer SSO,
// federated identity, and risk-aware lookups.
func (s *service) lookupUser(ctx context.Context, tenantID string, creds Credentials) (*user.User, error) {
	if s.users == nil {
		return nil, errors.New("auth: no user reader configured")
	}
	switch {
	case strings.TrimSpace(creds.Email) != "":
		return s.users.GetByEmail(ctx, tenantID, creds.Email)
	case strings.TrimSpace(creds.Username) != "":
		return s.users.GetByUsername(ctx, tenantID, creds.Username)
	default:
		return nil, ErrNoCredentials
	}
}

func (s *service) emitSuccess(ctx context.Context, tenantID, userID, identifier, sessionID string) {
	if s.audit == nil {
		return
	}
	_ = s.audit.Emit(ctx, "auth.login_success", "user:"+userID, map[string]any{
		"tenant_id":  tenantID,
		"user_id":    userID,
		"identifier": identifier,
		"session_id": sessionID,
	})
}

func (s *service) emitFailure(ctx context.Context, tenantID, identifier, reason string) {
	if s.audit == nil {
		return
	}
	_ = s.audit.Emit(ctx, "auth.login_failure", "tenant:"+tenantID, map[string]any{
		"tenant_id":  tenantID,
		"identifier": identifier,
		"reason":     reason,
	})
}

// generateSessionID returns a 256-bit cryptographically random ID encoded
// with base64-url and no padding. Total length is 43 chars.
func generateSessionID() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}
