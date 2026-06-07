// Package store defines the persistence contract for user_management.
//
// store.go owns the Store interface and shared sentinel errors. Concrete
// implementations live in subpackages such as store/sqlite.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package store

import (
	"context"
	"errors"
	"time"
)

// User is the persisted shape of a user. It mirrors the public user.User
// entity but is duplicated here so the store layer does not import the parent
// package (which would create a cycle).
type User struct {
	ID          string
	TenantID    string
	Email       string
	Username    string
	PassHash    string
	DisplayName string
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Store is the persistence contract for user_management. Implementations must
// be safe for concurrent use by multiple goroutines.
type Store interface {
	// Create persists a new user. Implementations return ErrDuplicateEmail or
	// ErrDuplicateUsername when tenant-scoped uniqueness is violated.
	Create(ctx context.Context, u *User) error
	// Get returns the user with the given ID, or ErrNotFound if none exists.
	Get(ctx context.Context, id string) (*User, error)
	// GetByEmail returns the user identified by (tenant, email), or ErrNotFound
	// if none exists.
	GetByEmail(ctx context.Context, tenantID, email string) (*User, error)
	// GetByUsername returns the user identified by (tenant, username), or
	// ErrNotFound if none exists.
	GetByUsername(ctx context.Context, tenantID, username string) (*User, error)
	// List returns users for the tenant ordered by username, paged by limit and
	// offset.
	List(ctx context.Context, tenantID string, limit, offset int) ([]*User, error)
	// Update overwrites a user's mutable fields except the password hash. It
	// returns ErrNotFound when no user matches and a duplicate sentinel on a
	// uniqueness conflict.
	Update(ctx context.Context, u *User) error
	// UpdatePassHash rewrites the stored password hash, returning ErrNotFound
	// when no user matches.
	UpdatePassHash(ctx context.Context, id, passHash string, updatedAt time.Time) error
	// Delete removes the user with the given ID, returning ErrNotFound when no
	// user matches.
	Delete(ctx context.Context, id string) error
}

// Sentinel errors returned by Store implementations.
var (
	ErrNotFound          = errors.New("user: not found")
	ErrDuplicateEmail    = errors.New("user: duplicate email for tenant")
	ErrDuplicateUsername = errors.New("user: duplicate username for tenant")
	// ErrUniqueConstraintViolation is returned when a UNIQUE constraint
	// trips but the error message cannot be classified into a specific
	// column (e.g., a vendor-specific message format we have not yet
	// taught classifyUniqueError to parse). Callers should treat it as a
	// generic conflict.
	ErrUniqueConstraintViolation = errors.New("user: unique constraint violation")
)
