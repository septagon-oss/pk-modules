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

// Store is the persistence contract for user_management.
type Store interface {
	Create(ctx context.Context, u *User) error
	Get(ctx context.Context, id string) (*User, error)
	GetByEmail(ctx context.Context, tenantID, email string) (*User, error)
	GetByUsername(ctx context.Context, tenantID, username string) (*User, error)
	List(ctx context.Context, tenantID string, limit, offset int) ([]*User, error)
	Update(ctx context.Context, u *User) error
	UpdatePassHash(ctx context.Context, id, passHash string, updatedAt time.Time) error
	Delete(ctx context.Context, id string) error
}

// Sentinel errors returned by Store implementations.
var (
	ErrNotFound          = errors.New("user: not found")
	ErrDuplicateEmail    = errors.New("user: duplicate email for tenant")
	ErrDuplicateUsername = errors.New("user: duplicate username for tenant")
)
