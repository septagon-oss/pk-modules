package tenant

// Implements: REQ-TENANT-001.
// Per: ADR-0017.
// Discipline: C-14.
// service.go owns the default TenantService and TenantContextProvider
// implementations. Pro can embed *service to compose SSO-aware lookups, RLS
// scoping, and billing-tier-aware quotas without reimplementing CRUD.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"errors"
	"strings"

	"github.com/septagon-oss/pk-modules/pkg/tenant/store"
)

// service is the default TenantService implementation. It is intentionally a
// thin layer over store.Store so business rules live in one place.
type service struct {
	store store.Store
}

func newService(s store.Store) *service { return &service{store: s} }

// Get returns a tenant by ID.
func (s *service) Get(ctx context.Context, id string) (*Tenant, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("tenant: id is required")
	}
	row, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return fromStore(row), nil
}

// GetBySlug returns a tenant by slug.
func (s *service) GetBySlug(ctx context.Context, slug string) (*Tenant, error) {
	if strings.TrimSpace(slug) == "" {
		return nil, errors.New("tenant: slug is required")
	}
	row, err := s.store.GetBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	return fromStore(row), nil
}

// List returns every tenant.
func (s *service) List(ctx context.Context) ([]*Tenant, error) {
	rows, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*Tenant, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromStore(r))
	}
	return out, nil
}

// Create inserts a new tenant. If t.ID is empty, the slug is used as the ID
// to keep the OSS surface dependency-free.
func (s *service) Create(ctx context.Context, t *Tenant) error {
	if err := t.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(t.ID) == "" {
		t.ID = t.Slug
	}
	row := toStore(t)
	if err := s.store.Create(ctx, row); err != nil {
		return err
	}
	// Reflect store-applied defaults (timestamps) back onto the caller.
	t.CreatedAt = row.CreatedAt
	t.UpdatedAt = row.UpdatedAt
	return nil
}

// Update overwrites a tenant.
func (s *service) Update(ctx context.Context, t *Tenant) error {
	if err := t.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(t.ID) == "" {
		return errors.New("tenant: id is required for update")
	}
	row := toStore(t)
	if err := s.store.Update(ctx, row); err != nil {
		return err
	}
	t.UpdatedAt = row.UpdatedAt
	return nil
}

// Delete removes a tenant by ID.
func (s *service) Delete(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("tenant: id is required")
	}
	return s.store.Delete(ctx, id)
}

// contextKey is the unexported type for stashing the tenant ID on a context.
type contextKey struct{}

var tenantContextKey contextKey

// contextProvider is the default TenantContextProvider.
type contextProvider struct{}

func (contextProvider) CurrentTenantID(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(tenantContextKey).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

func (contextProvider) ContextWithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantContextKey, tenantID)
}

// toStore converts the public Tenant into the storage representation.
func toStore(t *Tenant) *store.Tenant {
	return &store.Tenant{
		ID:        t.ID,
		Slug:      t.Slug,
		Name:      t.Name,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}

// fromStore converts a storage row into the public Tenant.
func fromStore(r *store.Tenant) *Tenant {
	if r == nil {
		return nil
	}
	return &Tenant{
		ID:        r.ID,
		Slug:      r.Slug,
		Name:      r.Name,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}
