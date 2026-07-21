// Package migrations exposes the embedded api_key_management migration
// files as an io/fs.FS so application-level runners can replay them.
//
// embed.go owns the //go:embed declaration. Migrations are append-only;
// never edit an existing file — always add a new one with a higher sequence
// number.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package migrations

// Implements: REQ-APIKEY-001.
// Per: ADR-0017.
// Discipline: C-14.
import "embed"

//go:embed *.sql
var fs embed.FS

// FS returns the embedded migrations filesystem.
func FS() embed.FS { return fs }
