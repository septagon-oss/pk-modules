// Package auth implements the auth_management business module.
//
// doc.go owns the package overview. The module supplies a session-cookie
// login flow on top of user_management: it verifies passwords through the
// configured passhash.Hasher, issues cryptographically random session IDs,
// persists session metadata to a pluggable SessionStore, and emits audit
// events on success and failure.
//
// Design:
//   - Composable: catalog wires NewModule and consumes Compose() to declare
//     Provides/Requires.
//   - Chainable: pk-pro embeds *Module and extends Service() with SSO,
//     MFA, rate limiting, and risk scoring without changing the public OSS
//     contract.
//   - Store-agnostic: callers either supply their own SessionStore or use
//     the default sqlite store via WithSQLiteDSN.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0017 (composition through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package auth
