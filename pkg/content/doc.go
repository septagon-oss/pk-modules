// Package content implements the content_management business module.
//
// doc.go owns the package overview. The module supplies tenant-scoped CRUD
// for content items (pages, posts, snippets) with a publishing lifecycle and
// a slug-based read port that adjacent modules (notification templating,
// public site renderers) can depend on without importing the full service.
//
// Design:
//   - Composable: catalog wires NewModule and consumes Compose() to declare
//     Provides/Requires.
//   - Chainable: pk-pro embeds *Module and extends Service() with workflow
//     approvals, scheduled publishing, and revision history without changing
//     the public OSS contract.
//   - Store-agnostic: callers either supply their own store.Store or use the
//     default sqlite store via WithSQLiteDSN.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0017 (composition through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package content
