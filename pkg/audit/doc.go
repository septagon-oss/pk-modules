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
// ADR: ADR-0009 (ports-only module communication), ADR-0017 (composition through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package audit
