// Package audit implements the audit_management business module.
//
// doc.go owns the package overview. The module supplies an append-only audit
// event log scoped to a tenant, with a read API for filtered queries and an
// emitter convenience for non-audit modules that want to emit events without
// holding a full AuditService reference.
//
// Design:
//   - Composable: catalog wires NewModule and consumes Compose() to declare
//     Provides/Requires.
//   - Chainable: pk-pro embeds *Module and extends Service() with retention
//     enforcement, partitioning, and forwarding to external SIEMs without
//     changing the public OSS contract.
//   - Store-agnostic: callers either supply their own store.Store or use the
//     default sqlite store via WithSQLiteDSN.
//
// # Trust boundary
//
// The HTTP handler in handler.go trusts the caller-supplied actor,
// tenant_id, and emitted_at fields when recording an event, and trusts the
// caller-supplied tenant_id/actor/action filters when querying. The
// package does not authenticate or authorize requests on its own.
//
// Hosts MUST gate /api/v1/audit-events behind admin_management or an
// equivalent authz middleware before exposing it publicly — otherwise any
// caller can forge events on behalf of an arbitrary tenant/actor or read
// events across tenants. Recommended layering:
//
//   - Mount the handler under an admin-only mux.
//   - Or wrap m.HTTPHandler() with the host's authn/authz middleware that
//     overrides r.URL.Query() "tenant_id" with the authenticated principal
//     before delegating.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0017 (composition through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package audit

// Implements: REQ-AUDIT-001.
// Per: ADR-0017.
// Discipline: C-14.
