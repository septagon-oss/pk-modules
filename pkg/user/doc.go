// Package user implements the user_management business module.
//
// doc.go owns the package overview. The module supplies user CRUD scoped to a
// tenant, password hashing via the pluggable passhash.Hasher contract, and the
// admin shell surface for managing users.
//
// Design:
//   - Composable: catalog wires NewModule and consumes Compose() to declare
//     Provides/Requires.
//   - Chainable: pk-pro embeds *Module and extends Service() with SSO, RLS,
//     and richer RBAC without changing the public OSS contract.
//   - Store-agnostic: callers either supply their own store.Store or use the
//     default sqlite store via WithSQLiteDSN.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0017 (composition through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package user

// Implements: REQ-USER-001.
// Per: ADR-0017.
// Discipline: C-14.
