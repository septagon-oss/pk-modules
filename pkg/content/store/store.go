// Package store defines the persistence contract for content_management.
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

// Content is the persisted shape of a content row. It mirrors the public
// content.Content entity but is duplicated here so the store layer does not
// import the parent package (which would create a cycle).
type Content struct {
	ID          string
	TenantID    string
	Kind        string
	Slug        string
	Title       string
	Body        string
	BodyFormat  string
	AuthorID    string
	PublishedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Store is the persistence contract for content_management. Implementations
// must be safe for concurrent use by multiple goroutines.
type Store interface {
	// Create persists a new content row. Implementations return ErrSlugTaken
	// when the (tenant, kind, slug) tuple is already in use and ErrDuplicate
	// when the ID collides.
	Create(ctx context.Context, c *Content) error
	// Get returns the row identified by (tenantID, id), or ErrNotFound if no
	// row with that ID exists in that tenant. The tenant predicate is
	// mandatory so a leaked or guessed ID cannot read across tenants.
	Get(ctx context.Context, tenantID, id string) (*Content, error)
	// GetBySlug returns the row identified by (tenant, kind, slug), or
	// ErrNotFound if none exists.
	GetBySlug(ctx context.Context, tenantID, kind, slug string) (*Content, error)
	// List returns rows for the tenant (optionally filtered by kind) ordered by
	// creation time descending, paged by limit and offset.
	List(ctx context.Context, tenantID, kind string, limit, offset int) ([]*Content, error)
	// Update overwrites the mutable fields of the row identified by
	// (c.TenantID, c.ID). It returns ErrNotFound when no row matches in that
	// tenant and ErrSlugTaken on a slug conflict. TenantID is not mutable via
	// Update — a row cannot be reassigned to another tenant.
	Update(ctx context.Context, c *Content) error
	// Delete removes the row identified by (tenantID, id), returning
	// ErrNotFound when no row matches in that tenant.
	Delete(ctx context.Context, tenantID, id string) error
	// SetPublished sets or clears (nil) the publication timestamp for the row
	// identified by (tenantID, id). It returns ErrNotFound when no row matches
	// in that tenant.
	SetPublished(ctx context.Context, tenantID, id string, at *time.Time) error
}

// Sentinel errors returned by Store implementations.
var (
	ErrNotFound  = errors.New("content: not found")
	ErrDuplicate = errors.New("content: duplicate id")
	ErrSlugTaken = errors.New("content: slug taken for tenant+kind")
)
