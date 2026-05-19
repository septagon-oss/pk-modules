package tenant

// entities.go owns the Tenant entity and the descriptor metadata used by
// generic CRUD handlers, admin pages, and the catalog.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"errors"
	"strings"
	"time"
)

// Tenant is the entity managed by the tenant_management module.
type Tenant struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// EntityName is the stable display name of the Tenant entity.
const EntityName = "Tenant"

// APIPath is the canonical HTTP base path for tenant CRUD.
const APIPath = "/api/v1/tenants"

// Validate enforces the small invariants required for storage. ID is
// caller-assigned (UUID, ULID, slug — anything stable); only Slug and Name
// are validated here.
func (t *Tenant) Validate() error {
	if t == nil {
		return errors.New("tenant: nil")
	}
	if strings.TrimSpace(t.Slug) == "" {
		return errors.New("tenant: slug is required")
	}
	if strings.TrimSpace(t.Name) == "" {
		return errors.New("tenant: name is required")
	}
	return nil
}
