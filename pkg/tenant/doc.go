// Package tenant implements the tenant_management business module.
//
// doc.go owns the package overview. The module supplies tenant CRUD,
// tenant-context propagation, and the isolation contract that downstream
// stores honor when scoping data to the active tenant.
//
// Design:
//   - Composable: catalog wires NewModule and consumes Compose() to declare
//     Provides/Requires.
//   - Chainable: pk-pro embeds *Module and extends Service() with SSO, RLS,
//     billing-tier-aware quotas without changing the public OSS contract.
//   - Store-agnostic: callers either supply their own store.Store or use the
//     default sqlite store via WithSQLiteDSN.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0017 (composition through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package tenant
