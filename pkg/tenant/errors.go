// Package tenant — errors.go re-exports the tenant persistence sentinels as
// public API so consumer modules can classify tenant lookups (for example
// user_management validating a tenant_id) without importing the concrete
// tenant/store package — which would violate the no-cross-module-implementation
// -imports invariant (ADR-0009). Depend on this sentinel, never on tenant/store.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package tenant

import "github.com/septagon-oss/pk-modules/pkg/tenant/store"

// ErrNotFound is returned (or wrapped) by TenantService lookups when no tenant
// matches. It is the public form of the store-layer sentinel; compare against
// it with errors.Is rather than importing tenant/store.
var ErrNotFound = store.ErrNotFound
