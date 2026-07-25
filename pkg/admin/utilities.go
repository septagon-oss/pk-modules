// Implements: REQ-004.
// Per: ADR-0022.
// Discipline: C-14.

package admin

// utilities.go composes the design system's utility layer into the admin's
// stylesheet: the role variables that map tw's semantic colors onto the theme
// tokens, and one rule for every class in tw's enumerable universe. Module
// admin pages (portslib.AdminPage) can therefore render pk-ui components and
// be styled with no extra request and no build step — the same stylesheet the
// shell already serves carries the whole system.
//
// The bespoke rules in _admin.css keep working unchanged: utility class names
// and the shell's pk-* namespace do not overlap, and the utilities reference
// the same --pk-* token variables the shell renders.

import (
	"github.com/septagon-oss/styleengine"
	"github.com/septagon-oss/tw/emission"
)

// adminUtilityCSS renders the role variables plus the base utility rules,
// minified. Computed once at shell construction. Falls back to an empty
// string on error for the same reason adminTokenCSS does: the inputs are
// compile-time constants, so an error is a programmer mistake upstream and
// the bespoke rules still render a working console.
func adminUtilityCSS() string {
	base, err := emission.Base()
	if err != nil {
		return ""
	}
	css, err := emission.RoleVars().Merge(base).Render(styleengine.RenderOptions{Minify: true})
	if err != nil {
		return ""
	}
	return css
}
