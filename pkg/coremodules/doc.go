// Implements: REQ-016.
// Per: ADR-0016.
// Discipline: C-14.

// Package coremodules is a TEACHING bundle: three stub modules (tenant, audit,
// content) with deliberately trivial interfaces, used to demonstrate the
// pk-core composition primitive — catalog → bundle → Compose — in isolation,
// with nothing else to distract from the dependency wiring.
//
// It is NOT the product. The stub interfaces here (TenantService.CurrentTenantID,
// AuditService.Record) are placeholders; they have no store, no HTTP surface,
// and no tenant isolation. Do not build a real app on this bundle.
//
// For a real application use the batteries-included starter, which composes the
// nine featureful modules (tenant, user, auth, api_key, audit, content,
// notification, admin, health) with SQLite stores, authenticated HTTP APIs, and
// enforced multi-tenancy:
//
//	github.com/septagon-oss/pk-apps/pkg/starterapp  // starterapp.Run(ctx, cfg)
//
// To add your own module to that starter, use starterapp.WithModules — see the
// examples/custommodule program in pk-apps and the "Add a module" guide in
// pk-docs. This package exists only so the DI mechanism can be shown minimally.
package coremodules
