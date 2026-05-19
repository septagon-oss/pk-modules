package auth

// entities.go owns the Session entity, the Credentials value type, and the
// descriptor metadata shared by handlers, the store layer, and admin pages.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"errors"
	"strings"
	"time"
)

// Session is the persisted authentication artifact returned by Login and
// consumed by ValidateSession. Sessions are revoked by setting RevokedAt and
// expire automatically once the wall clock passes ExpiresAt.
type Session struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	TenantID  string     `json:"tenant_id"`
	IssuedAt  time.Time  `json:"issued_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// Credentials carries the inputs to Login. Exactly one of Email and Username
// must be set; Password is always required. The struct is value-typed so
// callers cannot accidentally retain a pointer to credential plaintext.
type Credentials struct {
	Email    string
	Username string
	Password string
}

// EntityName is the stable display name of the Session entity.
const EntityName = "Session"

// APIPath is the canonical HTTP base path for the session surface.
const APIPath = "/api/v1/auth/sessions"

// Validate enforces the small invariants required for storage.
func (s *Session) Validate() error {
	if s == nil {
		return errors.New("auth: nil session")
	}
	if strings.TrimSpace(s.ID) == "" {
		return errors.New("auth: session id is required")
	}
	if strings.TrimSpace(s.UserID) == "" {
		return errors.New("auth: user_id is required")
	}
	if strings.TrimSpace(s.TenantID) == "" {
		return errors.New("auth: tenant_id is required")
	}
	if s.ExpiresAt.IsZero() {
		return errors.New("auth: expires_at is required")
	}
	return nil
}

// Identifier returns whichever of Email or Username the credentials carry.
// Returns an empty string when neither is set.
func (c Credentials) Identifier() string {
	if v := strings.TrimSpace(c.Email); v != "" {
		return v
	}
	return strings.TrimSpace(c.Username)
}
