// Implements: REQ-AUTH-001.
// Per: ADR-0009.
// Discipline: C-14.

package auth

// ports.go owns the public interfaces other modules consume. Downstream
// modules should depend on these interfaces — never on *Module or the
// concrete store implementations.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import "context"

// AuthService is the public port other modules use to drive the login
// lifecycle.
type AuthService interface {
	Login(ctx context.Context, tenantID string, creds Credentials) (*Session, error)
	Logout(ctx context.Context, sessionID string) error
	ValidateSession(ctx context.Context, sessionID string) (*Session, error)
	InvalidateAllSessions(ctx context.Context, userID string) error
}

// SessionStore is the persistence contract for sessions. Implementations
// must be safe for concurrent use.
type SessionStore interface {
	Create(ctx context.Context, s *Session) error
	Get(ctx context.Context, id string) (*Session, error)
	Revoke(ctx context.Context, id string) error
	RevokeByUser(ctx context.Context, userID string) error
}

// LoginPolicy is the mandatory policy hook auth_management consults before
// issuing a session and after each attempt. Hosts provide rate limiting,
// lockout, and risk scoring through this boundary.
type LoginPolicy interface {
	AllowLogin(ctx context.Context, tenantID, identifier string) error
	RecordFailure(ctx context.Context, tenantID, identifier string)
	RecordSuccess(ctx context.Context, tenantID, identifier string)
}
