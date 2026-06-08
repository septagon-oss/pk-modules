// Package coremodules provides the smallest OSS module pack.
package coremodules

// bundle.go owns the minimal public module bundle used by OSS examples and
// downstream PlatformKit distributions.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0017 (composition through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import "github.com/septagon-oss/pk-core/pkg/module"

// TenantService is the minimal tenant contract exposed by the tenant module.
type TenantService interface {
	CurrentTenantID() string
}

// AuditService is the minimal audit contract consumed by other modules.
type AuditService interface {
	Record(message string) error
}

// Bundle returns the smallest public PlatformKit module pack.
func Bundle() module.Bundle {
	return module.NewBundle("platformkit.modules.core", []module.Entry{
		{ID: "tenant", New: NewTenant},
		{ID: "audit", New: NewAudit},
		{ID: "content", New: NewContent},
	}, []string{"tenant", "audit", "content"})
}

// NewTenant constructs the tenant module.
func NewTenant() module.Composable {
	return module.Must(
		module.Metadata{ID: "tenant", Name: "Tenant", Description: "Tenant context"},
		module.WithProvides(module.Provide[TenantService]("0.0.0")),
	)
}

// NewAudit constructs the audit module.
func NewAudit() module.Composable {
	return module.Must(
		module.Metadata{ID: "audit", Name: "Audit", Description: "Audit event recording"},
		module.WithProvides(module.Provide[AuditService]("0.0.0")),
		module.WithDependencies(module.Require[TenantService](module.DependencySpec{
			Version:           "0.0.0",
			Purpose:           "scope audit records by tenant",
			PreferredProvider: "tenant",
		})),
	)
}

// NewContent constructs the content module.
func NewContent() module.Composable {
	return module.Must(
		module.Metadata{ID: "content", Name: "Content", Description: "Public content"},
		module.WithDependencies(
			module.Require[TenantService](module.DependencySpec{
				Version:           "0.0.0",
				Purpose:           "scope content by tenant",
				PreferredProvider: "tenant",
			}),
			module.Require[AuditService](module.DependencySpec{
				Version:           "0.0.0",
				Purpose:           "audit content publication",
				PreferredProvider: "audit",
			}),
		),
	)
}
