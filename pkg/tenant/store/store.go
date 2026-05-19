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

// Store is the persistence contract for tenant_management.
type Store interface {
	Create(ctx context.Context, t *Tenant) error
	Get(ctx context.Context, id string) (*Tenant, error)
	GetBySlug(ctx context.Context, slug string) (*Tenant, error)
	List(ctx context.Context) ([]*Tenant, error)
	Update(ctx context.Context, t *Tenant) error
	Delete(ctx context.Context, id string) error
}

// Sentinel errors returned by Store implementations.
var (
	ErrNotFound      = errors.New("tenant: not found")
	ErrDuplicateSlug = errors.New("tenant: duplicate slug")
)
