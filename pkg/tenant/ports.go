package tenant

// ports.go owns the public interfaces other modules consume. Downstream
// modules should depend on these interfaces — never on *Module or the
// concrete store implementations.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import "context"

// TenantService is the Pro-consumable port. Other modules depend on this
// (not on the concrete Module type) for tenant context resolution.
type TenantService interface {
	Get(ctx context.Context, id string) (*Tenant, error)
	GetBySlug(ctx context.Context, slug string) (*Tenant, error)
	List(ctx context.Context) ([]*Tenant, error)
	Create(ctx context.Context, t *Tenant) error
	Update(ctx context.Context, t *Tenant) error
	Delete(ctx context.Context, id string) error
}

// TenantContextProvider exposes the active tenant for the current request.
type TenantContextProvider interface {
	CurrentTenantID(ctx context.Context) (string, bool)
	ContextWithTenant(ctx context.Context, tenantID string) context.Context
}

// TenantIsolationEnforcer is the contract every persistence layer should
// honor — Pro stores implement this to scope queries to the current tenant.
type TenantIsolationEnforcer interface {
	EnforceTenant(ctx context.Context, query string) (string, error)
}
