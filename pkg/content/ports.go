package content

// ports.go owns the public interfaces other modules consume. Downstream
// modules should depend on these interfaces — never on *Module or the
// concrete store implementations.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import "context"

// ContentService is the full read/write port. Admin handlers and authoring
// flows depend on this surface for CRUD plus publish/unpublish lifecycle.
type ContentService interface {
	Create(ctx context.Context, c *Content) error
	Get(ctx context.Context, tenantID, id string) (*Content, error)
	GetBySlug(ctx context.Context, tenantID, kind, slug string) (*Content, error)
	List(ctx context.Context, tenantID string, kind string, limit, offset int) ([]*Content, error)
	Update(ctx context.Context, c *Content) error
	Delete(ctx context.Context, tenantID, id string) error
	Publish(ctx context.Context, tenantID, id string) error
	Unpublish(ctx context.Context, tenantID, id string) error
}

// ContentReader is the read-only port for non-CMS modules (e.g., notification
// templating, public site renderers) that need to resolve content by ID or
// slug without holding a full ContentService reference.
type ContentReader interface {
	Get(ctx context.Context, tenantID, id string) (*Content, error)
	GetBySlug(ctx context.Context, tenantID, kind, slug string) (*Content, error)
}

// ContentPublisher is the lifecycle-only port exposed for admin publishing
// workflows that should not be able to mutate the body.
type ContentPublisher interface {
	Publish(ctx context.Context, tenantID, id string) error
	Unpublish(ctx context.Context, tenantID, id string) error
}
