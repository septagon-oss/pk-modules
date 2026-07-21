// Package store defines the persistence contract for api_key_management.
//
// store.go owns the row shape and shared sentinel errors. Concrete
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

// APIKey is the persisted shape of an API key row. It mirrors the public
// apikey.APIKey entity but is duplicated here so the store layer does not
// import the parent package (which would create a cycle). Scopes is the
// JSON-encoded representation persisted as a TEXT column.
type APIKey struct {
	ID         string
	TenantID   string
	UserID     string
	Name       string
	Prefix     string
	Hash       string
	Scopes     string
	LastUsedAt *time.Time
	RevokedAt  *time.Time
	ExpiresAt  *time.Time
	CreatedAt  time.Time
}

// Store is the persistence contract for api_key_management. Implementations
// must be safe for concurrent use by multiple goroutines.
type Store interface {
	// Create persists a new API key. Implementations return ErrDuplicate when
	// the key ID is already in use.
	Create(ctx context.Context, k *APIKey) error
	// Get returns the key identified by (tenantID, id), or ErrNotFound if no key
	// with that ID exists in that tenant. Mandatory tenant predicate: a
	// management caller cannot read another tenant's key by ID.
	Get(ctx context.Context, tenantID, id string) (*APIKey, error)
	// GetByPrefix returns every key sharing the given prefix (revoked or not).
	// This lookup is intentionally GLOBAL (not tenant-scoped): it is the
	// authentication path, where the presented key selects its own tenant.
	// Callers verify the bcrypt hash before trusting a candidate and read the
	// tenant from the matched row.
	GetByPrefix(ctx context.Context, prefix string) ([]*APIKey, error)
	// List returns every key for the tenant ordered by creation time descending.
	List(ctx context.Context, tenantID string) ([]*APIKey, error)
	// Revoke marks the key identified by (tenantID, id) revoked. It returns
	// ErrNotFound when no key exists in that tenant and is a no-op when the key
	// is already revoked.
	Revoke(ctx context.Context, tenantID, id string) error
	// UpdateLastUsed records the most recent use timestamp for the key
	// identified by (tenantID, id).
	UpdateLastUsed(ctx context.Context, tenantID, id string, at time.Time) error
}

// Sentinel errors returned by Store implementations.
var (
	ErrNotFound  = errors.New("apikey: not found")
	ErrDuplicate = errors.New("apikey: duplicate id")
)
