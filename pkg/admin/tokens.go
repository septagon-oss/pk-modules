// Implements: REQ-004.
// Per: ADR-0022.
// Discipline: C-14.

package admin

// tokens.go owns the admin shell's design-token layer. Rather than hand-code
// colors and spacing, the shell declares a pk-design DTCG token set and renders
// it to a `:root { --pk-* }` CSS block via tokens.CSSVars — the same
// renderer-neutral pipeline the design system uses, so the OSS admin stays
// visually aligned with pk-design (ADR-0022) without pulling in a frontend
// build step. The hand-written rules in static/_admin.css reference these
// custom properties; a theme is a matter of swapping the values here.

import (
	"github.com/septagon-oss/pk-design/pkg/tokens"
)

// adminTokenSet is the admin's design tokens: color roles, a spacing scale, and
// radii, in pk-design DTCG form. Token paths render as CSS custom properties —
// e.g. "color.text.primary" -> "--pk-color-text-primary".
func adminTokenSet() tokens.Set {
	color := func(v string) tokens.Value { return tokens.Value(v) }
	return tokens.Set{
		Name: "pk",
		Values: map[string]tokens.Value{
			// Surfaces
			"color.surface.canvas":  color("#f6f7f9"),
			"color.surface.primary": color("#ffffff"),
			"color.surface.muted":   color("#f9fafb"),
			// Text
			"color.text.primary": color("#111827"),
			"color.text.muted":   color("#6b7280"),
			// Lines
			"color.border.default": color("#e5e7eb"),
			// Accent + status
			"color.accent.default": color("#4f46e5"),
			"color.accent.on":      color("#ffffff"),
			"color.status.ok":      color("#059669"),
			"color.status.okbg":    color("#ecfdf5"),
			// Spacing scale (4px base)
			"space.1": color("4px"),
			"space.2": color("8px"),
			"space.3": color("12px"),
			"space.4": color("16px"),
			"space.5": color("24px"),
			"space.6": color("32px"),
			// Radii
			"radius.sm":   color("6px"),
			"radius.md":   color("10px"),
			"radius.pill": color("999px"),
		},
		Types: map[string]tokens.Type{
			"color.surface.canvas":  tokens.TypeColor,
			"color.surface.primary": tokens.TypeColor,
			"color.surface.muted":   tokens.TypeColor,
			"color.text.primary":    tokens.TypeColor,
			"color.text.muted":      tokens.TypeColor,
			"color.border.default":  tokens.TypeColor,
			"color.accent.default":  tokens.TypeColor,
			"color.accent.on":       tokens.TypeColor,
			"color.status.ok":       tokens.TypeColor,
			"color.status.okbg":     tokens.TypeColor,
			"space.1":               tokens.TypeDimension,
			"space.2":               tokens.TypeDimension,
			"space.3":               tokens.TypeDimension,
			"space.4":               tokens.TypeDimension,
			"space.5":               tokens.TypeDimension,
			"space.6":               tokens.TypeDimension,
			"radius.sm":             tokens.TypeDimension,
			"radius.md":             tokens.TypeDimension,
			"radius.pill":           tokens.TypeDimension,
		},
	}
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
