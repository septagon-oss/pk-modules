package content

// service.go owns the default ContentService, ContentReader, and
// ContentPublisher implementations. Pro embeds *service to add scheduled
// publishing, revision history, and approval workflows.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/septagon-oss/pk-modules/pkg/audit"
	"github.com/septagon-oss/pk-modules/pkg/content/store"
)

// service is the default ContentService + ContentReader + ContentPublisher
// implementation.
type service struct {
	store store.Store
	audit audit.AuditEmitter
}

func newService(s store.Store, em audit.AuditEmitter) *service {
	return &service{store: s, audit: em}
}

// Create persists a new content row. The service assigns CreatedAt/UpdatedAt
// when zero, defaults BodyFormat to markdown, and generates an ID when the
// caller leaves Content.ID blank.
func (s *service) Create(ctx context.Context, c *Content) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.BodyFormat == "" {
		c.BodyFormat = BodyFormatMarkdown
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	if c.UpdatedAt.IsZero() {
		c.UpdatedAt = c.CreatedAt
	}
	if strings.TrimSpace(c.ID) == "" {
		c.ID = generateID(c.CreatedAt, c.TenantID, c.Slug)
	}
	if err := s.store.Create(ctx, toStore(c)); err != nil {
		return err
	}
	s.emit(ctx, c, "content.created", nil)
	return nil
}

// Get returns a content row by ID.
func (s *service) Get(ctx context.Context, id string) (*Content, error) {
	row, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return fromStore(row), nil
}

// GetBySlug returns a content row by (tenant_id, kind, slug).
func (s *service) GetBySlug(ctx context.Context, tenantID, kind, slug string) (*Content, error) {
	row, err := s.store.GetBySlug(ctx, tenantID, kind, slug)
	if err != nil {
		return nil, err
	}
	return fromStore(row), nil
}

// List returns content rows for the given tenant (and optionally a kind).
func (s *service) List(ctx context.Context, tenantID, kind string, limit, offset int) ([]*Content, error) {
	rows, err := s.store.List(ctx, tenantID, kind, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]*Content, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromStore(r))
	}
	return out, nil
}

// Update replaces the mutable fields of an existing row. CreatedAt is
// preserved by the store layer.
func (s *service) Update(ctx context.Context, c *Content) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(c.ID) == "" {
		return errors.New("content: update requires ID")
	}
	if c.BodyFormat == "" {
		c.BodyFormat = BodyFormatMarkdown
	}
	existing, err := s.store.Get(ctx, c.ID)
	if err != nil {
		return err
	}
	c.CreatedAt = existing.CreatedAt
	if err := s.store.Update(ctx, toStore(c)); err != nil {
		return err
	}
	s.emit(ctx, c, "content.updated", nil)
	return nil
}

// Delete removes a content row by ID.
func (s *service) Delete(ctx context.Context, id string) error {
	existing, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	s.emit(ctx, fromStore(existing), "content.deleted", nil)
	return nil
}

// Publish flips the published_at marker to now and emits a content.published
// audit event when an emitter is wired.
func (s *service) Publish(ctx context.Context, id string) error {
	existing, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := s.store.SetPublished(ctx, id, &now); err != nil {
		return err
	}
	published := fromStore(existing)
	published.PublishedAt = &now
	s.emit(ctx, published, "content.published", map[string]any{"published_at": now})
	return nil
}

// Unpublish clears published_at and emits a content.unpublished audit event
// when an emitter is wired.
func (s *service) Unpublish(ctx context.Context, id string) error {
	existing, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.store.SetPublished(ctx, id, nil); err != nil {
		return err
	}
	c := fromStore(existing)
	c.PublishedAt = nil
	s.emit(ctx, c, "content.unpublished", nil)
	return nil
}

// emit fans the event to the optional audit emitter. Errors are dropped so
// the audit pipeline can never block the primary write path.
func (s *service) emit(ctx context.Context, c *Content, action string, extra map[string]any) {
	if s.audit == nil || c == nil {
		return
	}
	details := map[string]any{
		"kind": c.Kind,
		"slug": c.Slug,
	}
	for k, v := range extra {
		details[k] = v
	}
	_ = s.audit.Emit(ctx, action, "content:"+c.ID, details)
}

// generateID synthesizes a deterministic-ish content ID without bringing in a
// UUID dependency. The format is `<unix-nano>-<tenant>-<slug>` truncated for
// readability — Pro replaces this with ULID.
func generateID(createdAt time.Time, tenantID, slug string) string {
	return fmt.Sprintf("%d-%s-%s", createdAt.UTC().UnixNano(), shorten(tenantID), shorten(slug))
}

func shorten(s string) string {
	const max = 16
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func toStore(c *Content) *store.Content {
	var published *time.Time
	if c.PublishedAt != nil {
		t := *c.PublishedAt
		published = &t
	}
	return &store.Content{
		ID:          c.ID,
		TenantID:    c.TenantID,
		Kind:        c.Kind,
		Slug:        c.Slug,
		Title:       c.Title,
		Body:        c.Body,
		BodyFormat:  c.BodyFormat,
		AuthorID:    c.AuthorID,
		PublishedAt: published,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
	}
}

func fromStore(r *store.Content) *Content {
	if r == nil {
		return nil
	}
	var published *time.Time
	if r.PublishedAt != nil {
		t := *r.PublishedAt
		published = &t
	}
	return &Content{
		ID:          r.ID,
		TenantID:    r.TenantID,
		Kind:        r.Kind,
		Slug:        r.Slug,
		Title:       r.Title,
		Body:        r.Body,
		BodyFormat:  r.BodyFormat,
		AuthorID:    r.AuthorID,
		PublishedAt: published,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}
