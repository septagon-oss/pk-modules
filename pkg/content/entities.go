package content

// entities.go owns the Content entity, body-format constants, and descriptor
// metadata used by handlers, the store layer, and admin pages.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"errors"
	"strings"
	"time"
)

// Content is the entity managed by the content_management module. Records
// are tenant-scoped; the (tenant_id, kind, slug) tuple is unique per row.
type Content struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	Kind        string     `json:"kind"`
	Slug        string     `json:"slug"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	BodyFormat  string     `json:"body_format"`
	AuthorID    string     `json:"author_id"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Kind values accepted by the OSS content module.
const (
	KindPage    = "page"
	KindPost    = "post"
	KindSnippet = "snippet"
)

// BodyFormat values accepted by the OSS content module.
const (
	BodyFormatMarkdown = "markdown"
	BodyFormatHTML     = "html"
	BodyFormatText     = "text"
)

// EntityName is the stable display name of the Content entity.
const EntityName = "Content"

// APIPath is the canonical HTTP base path for content CRUD.
const APIPath = "/api/v1/content"

// Validate enforces the small invariants required for storage. Defaults are
// applied by the service layer before write.
func (c *Content) Validate() error {
	if c == nil {
		return errors.New("content: nil")
	}
	if strings.TrimSpace(c.TenantID) == "" {
		return errors.New("content: tenant_id is required")
	}
	if strings.TrimSpace(c.Kind) == "" {
		return errors.New("content: kind is required")
	}
	if strings.TrimSpace(c.Slug) == "" {
		return errors.New("content: slug is required")
	}
	if strings.TrimSpace(c.Title) == "" {
		return errors.New("content: title is required")
	}
	switch c.BodyFormat {
	case "", BodyFormatMarkdown, BodyFormatHTML, BodyFormatText:
	default:
		return errors.New("content: body_format must be markdown, html, or text")
	}
	return nil
}
