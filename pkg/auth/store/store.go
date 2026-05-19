// Package store defines the persistence contract for auth_management.
//
// store.go owns the row-shape and shared sentinel errors. Concrete
// implementations live in subpackages such as store/sqlite.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package store

import (
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

// Sentinel errors returned by Store implementations.
var (
	ErrNotFound  = errors.New("auth: session not found")
	ErrDuplicate = errors.New("auth: duplicate session id")
)
