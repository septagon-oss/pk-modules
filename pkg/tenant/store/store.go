// Package store defines the persistence contract for tenant_management.
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

// Tenant is the persisted shape of a tenant. It mirrors the public
// tenant.Tenant entity but is duplicated here so the store layer does not
// import the parent package (which would create a cycle).
type Tenant struct {
	ID        string
	Slug      string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Store is the persistence contract for tenant_management. Implementations
// must be safe for concurrent use by multiple goroutines.
type Store interface {
	// Create persists a new tenant. Implementations return ErrDuplicateSlug
	// when the slug is already in use.
	Create(ctx context.Context, t *Tenant) error
	// Get returns the tenant with the given ID, or ErrNotFound if none exists.
	Get(ctx context.Context, id string) (*Tenant, error)
	// GetBySlug returns the tenant with the given slug, or ErrNotFound if none
	// exists.
	GetBySlug(ctx context.Context, slug string) (*Tenant, error)
	// List returns every tenant ordered by slug.
	List(ctx context.Context) ([]*Tenant, error)
	// Update overwrites a tenant's mutable fields. It returns ErrNotFound when
	// no tenant matches and ErrDuplicateSlug on a slug conflict.
	Update(ctx context.Context, t *Tenant) error
	// Delete removes the tenant with the given ID, returning ErrNotFound when
	// no tenant matches.
	Delete(ctx context.Context, id string) error
}

// Sentinel errors returned by Store implementations.
var (
	ErrNotFound      = errors.New("tenant: not found")
	ErrDuplicateSlug = errors.New("tenant: duplicate slug")
)
