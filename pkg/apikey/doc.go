// Package apikey implements the api_key_management business module.
//
// doc.go owns the package overview. The module supplies issuance,
// verification, listing, and revocation of long-lived API keys, plus an
// HTTP middleware that attaches an identity.Principal to the request
// context when a valid `Authorization: Bearer <key>` header is present.
//
// Design:
//   - Composable: catalog wires NewModule and consumes Compose() to declare
//     Provides/Requires.
//   - Chainable: pk-pro embeds *Module and extends Service() with rotation
//     hooks, finer-grained scopes, and per-key telemetry without changing
//     the public OSS contract.
//   - Store-agnostic: callers either supply their own Store or use the
//     default sqlite store via WithSQLiteDSN.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0017 (composition through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package apikey
