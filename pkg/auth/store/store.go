// Package store defines the persistence contract for auth_management.
//
// store.go owns the row-shape and shared sentinel errors. Concrete
// implementations live in subpackages such as store/sqlite.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package store

// Implements: REQ-AUTH-001.
// Per: ADR-0028.
// Discipline: C-14.
import (
	"context"
	"errors"
	"time"
)

// Session is the persisted shape of a session row. It mirrors the public
// auth.Session entity but is duplicated here so the store layer does not
// import the parent package (which would create a cycle).
type Session struct {
	ID        string
	UserID    string
	TenantID  string
	IssuedAt  time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

// Store is the persistence contract for auth_management sessions.
// Implementations must be safe for concurrent use by multiple goroutines.
//
// Sessions are addressed by their opaque id, which is itself the bearer
// secret: holding it IS the authorization, and the row carries the tenant it
// belongs to. That is why these operations take no tenant argument — a caller
// has no authenticated tenant until this very lookup resolves the session that
// establishes one.
type Store interface {
	// Create persists a new session, returning ErrDuplicate on an id collision.
	Create(ctx context.Context, s *Session) error
	// Get returns the session with that id, or ErrNotFound.
	Get(ctx context.Context, id string) (*Session, error)
	// Revoke marks the session revoked. Revoking an already-revoked session is
	// a no-op, not an error; an unknown id returns ErrNotFound.
	Revoke(ctx context.Context, id string) error
	// RevokeByUser revokes every live session belonging to a user — the
	// "sign out everywhere" operation.
	RevokeByUser(ctx context.Context, userID string) error
}

// Sentinel errors returned by Store implementations.
var (
	ErrNotFound  = errors.New("auth: session not found")
	ErrDuplicate = errors.New("auth: duplicate session id")
)
