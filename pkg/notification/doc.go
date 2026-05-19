// Package notification implements the notification_management business module.
//
// doc.go owns the package overview. The module owns the persistence and
// dispatch lifecycle for in-app notifications, supports pluggable additional
// channels (mail/SMS/push live in pk-pro), and exposes a subscription surface
// so users can opt into categories per channel.
//
// Design:
//   - Composable: catalog wires NewModule and consumes Compose() to declare
//     Provides/Requires.
//   - Chainable: pk-pro embeds *Module and adds richer channels (mail/SMS/
//     push), digesting, and templated rendering without changing the public
//     OSS contract.
//   - Store-agnostic: callers either supply their own store.Store or use the
//     default sqlite store via WithSQLiteDSN.
//   - The OSS surface ships a single built-in in-app channel; the default
//     persists notifications to the store so GetByUser can read them back.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0017 (composition through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package notification
