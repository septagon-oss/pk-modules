// Package store defines the persistence contract for branding_management.
//
// store.go owns the Store interface, the Record row shape, and the shared
// sentinel error. Concrete implementations live in subpackages such as
// store/sqlite.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package store

// Implements: REQ-BRANDING-001.
// Per: ADR-0017.
// Discipline: C-14.
import (
	"context"
	"errors"
	"time"
)

// ErrNotFound reports that a tenant has no branding record.
var ErrNotFound = errors.New("branding: not found")

// Record is one tenant's stored branding. LogoData holds the raw uploaded
// bytes; the parent module derives a servable LogoURL from them rather than
// persisting a URL directly. Record mirrors the public branding profile
// shape but is duplicated here so the store layer does not import the parent
// package (which would create a cycle).
type Record struct {
	TenantID         string
	DisplayName      string
	LogoData         []byte
	LogoContentType  string
	LogoAlt          string
	PrimaryColor     string
	FontKey          string
	SetupCompletedAt *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Store persists branding records keyed by tenant. Implementations must be
// safe for concurrent use by multiple goroutines.
type Store interface {
	// Get returns the branding record for the given tenant, or ErrNotFound if
	// none exists yet.
	Get(ctx context.Context, tenantID string) (*Record, error)
	// Upsert creates or replaces the branding record for r.TenantID.
	// Timestamps are assigned by the implementation, not the caller: CreatedAt
	// is stamped on first insert and preserved on every later call for the
	// same tenant; UpdatedAt always advances to now.
	Upsert(ctx context.Context, r *Record) error
}
