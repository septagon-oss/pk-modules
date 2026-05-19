// Package adminstatic embeds the admin shell's static assets.
//
// embed.go owns the //go:embed declaration that turns ./_admin.css and any
// future static files into an io/fs filesystem the admin handler serves
// under /admin/static/.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package adminstatic

import (
	"embed"
	"io/fs"
)

//go:embed _admin.css
var assets embed.FS

// FS returns the embedded static asset filesystem rooted at the admin static
// directory.
func FS() fs.FS { return assets }
