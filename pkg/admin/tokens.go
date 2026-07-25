// Implements: REQ-004.
// Per: ADR-0022.
// Discipline: C-14.

package admin

// tokens.go owns the admin shell's design-token layer. The values come from
// pk-design's canonical theme (themes.Default()), rendered to a
// `:root { --pk-* }` CSS block via tokens.CSSVars — the same renderer-neutral
// pipeline every other consumer of the design system uses, so the OSS admin
// stays visually aligned with pk-design (ADR-0022) without pulling in a
// frontend build step. The hand-written rules in static/_admin.css reference
// these custom properties; theming is layering another theme over the default
// in pk-design, not editing this file.

import (
	"github.com/septagon-oss/pk-design/pkg/themes"
	"github.com/septagon-oss/pk-design/pkg/tokens"
)

// adminTokenSet is the admin's design tokens: pk-design's canonical theme —
// color roles, type stacks, a spacing scale, and radii. Token paths render as
// CSS custom properties, e.g. "color.text.primary" -> "--pk-color-text-primary".
func adminTokenSet() tokens.Set {
	return themes.Default().Tokens
}

// adminTokenCSS renders the token set to a ":root { --pk-* }" block. Computed
// once at shell construction; served ahead of the static rules.
func adminTokenCSS() string {
	css, err := tokens.CSSVars(adminTokenSet())
	if err != nil {
		// The set is a compile-time constant; a render error means a malformed
		// token above. Fall back to no custom properties rather than panic —
		// the static rules carry sane fallbacks.
		return ":root {}\n"
	}
	return css
}
