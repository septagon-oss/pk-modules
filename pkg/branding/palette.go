// Implements: REQ-BRANDING-001.
// Per: ADR-0017.
// Discipline: C-14.

// Package branding turns stored tenant branding choices into per-tenant theme
// overlays for the PlatformKit design stack.
package branding

// palette.go owns WCAG-corrected palette derivation: pure stdlib color math
// that maps one tenant primary color plus a curated font key onto a
// themes.LayerTenant token overlay. The overlay overrides only token paths
// that already exist in the pk-design default theme, so rendering it with
// tokens.CSSVars yields the same --pk-* custom properties the admin shell
// already consumes.
//
// Derivation policy:
//   - color.accent.default: the primary darkened toward black in 5% steps of
//     the original color until the 8-bit color actually emitted reaches WCAG
//     AA 4.5:1 contrast against white; color.accent.on is then white. Under
//     the current constants (darkenStep 5%, maxDarkenSteps 12) every primary
//     reaches this within 12 steps: the worst case, #ffffff, converges at
//     step 11 (#737373, 4.74:1). The ink fallback below is therefore
//     defensive only and unreachable today; it is kept so constant changes
//     fail safe. Should a change ever make maxDarkenSteps insufficient,
//     darkening continues past it until the emitted pair measures at least
//     4.5:1 against either white or ink (#111827), never silently emitting
//     an inaccessible pair. Contrast is always measured on the quantized hex
//     value that will be emitted, never on intermediate floats.
//   - color.accent.hover: accent.default darkened a further 8% when
//     accent.on is white, since a darker accent only gains contrast under
//     light text. On the defensive ink branch hover stays exactly at
//     accent.default: text there is dark, so darkening further would erode
//     the pair contrast the fallback guard just enforced.
//   - color.signal and color.focus: the raw primary, canonicalized to
//     lowercase #rrggbb.
//   - font.body and font.display: curated stacks only; the empty key means
//     "no font override" and unknown keys are rejected so callers get one
//     validation gate.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-10 (shared builders return errors), C-14 (every Go file declares its purpose).

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/septagon-oss/pk-design/pkg/themes"
	"github.com/septagon-oss/pk-design/pkg/tokens"
)

const (
	// layerID is the stable identity of the tenant branding overlay layer.
	layerID = "tenant-branding"
	// tokenNamespace matches themes.Normalize's default token set name so the
	// overlay emits --pk-* custom properties exactly like the default theme.
	tokenNamespace = "pk"
	// darkenStep is the per-step darkening fraction of the original primary.
	darkenStep = 0.05
	// maxDarkenSteps is the step threshold past which the ink fallback
	// becomes eligible; correction deliberately keeps darkening beyond it
	// until either accent.on candidate reaches AA (see correctedAccent).
	maxDarkenSteps = 12
	// hoverDarken is the extra darkening that derives the hover accent on
	// the white-text branch; the ink branch keeps hover at accent.default.
	hoverDarken = 0.08
	// inkHex is the defensive accent.on fallback for primaries that cannot
	// reach AA contrast against white within maxDarkenSteps. Unreachable
	// while darkenStep = 5% and maxDarkenSteps = 12 hold (the worst case,
	// #ffffff, reaches AA against white at step 11); kept so constant
	// changes fail safe, guarded at derivation time by correctedAccent and
	// at test time by TestInkFallbackStaysUnreachableUnderCurrentConstants.
	inkHex = "#111827"
	// whiteHex is the accent.on color whenever the corrected accent reaches
	// AA contrast against white.
	whiteHex = "#ffffff"
)

// fontStacks is the curated font catalog. "plex" is the explicit default: its
// stack equals the default theme's font.body, and "editorial" equals the
// default font.display serif stack.
var fontStacks = map[string]struct{ body, display string }{
	"editorial": {body: `"Iowan Old Style", "Palatino Linotype", Palatino, Georgia, serif`, display: `"Iowan Old Style", "Palatino Linotype", Palatino, Georgia, serif`},
	"grotesk":   {body: `"Helvetica Neue", Helvetica, Arial, sans-serif`, display: `"Helvetica Neue", Helvetica, Arial, sans-serif`},
	"plex":      {body: `"IBM Plex Sans", Aptos, "Helvetica Neue", sans-serif`, display: `"IBM Plex Sans", Aptos, "Helvetica Neue", sans-serif`},
}

// DeriveLayer builds the tenant token overlay for one primary color and font
// key. Either input may be empty to skip its half of the overlay; when both
// are empty there is nothing to override and DeriveLayer reports ok=false
// with a zero layer. The returned layer overrides only token paths defined by
// the default theme: color.accent.{default,hover,on}, color.signal,
// color.focus, font.body, and font.display.
func DeriveLayer(primaryHex, fontKey string) (themes.TokenLayer, bool, error) {
	primaryHex = strings.TrimSpace(primaryHex)
	fontKey = strings.TrimSpace(fontKey)
	if primaryHex == "" && fontKey == "" {
		return themes.TokenLayer{}, false, nil
	}

	values := map[string]tokens.Value{}
	types := map[string]tokens.Type{}
	if primaryHex != "" {
		r, g, b, err := parseHex(primaryHex)
		if err != nil {
			return themes.TokenLayer{}, false, err
		}
		accentR, accentG, accentB, on := correctedAccent(r, g, b)
		// Hover darkens the accent only under white text, where darker means
		// more contrast. On the (defensive) ink branch the text is dark, so
		// extra darkening would reduce the hover/on ratio below the pair
		// correctedAccent just guaranteed; hover stays at accent.default.
		hoverR, hoverG, hoverB := accentR, accentG, accentB
		if on == whiteHex {
			hoverR, hoverG, hoverB = darken(accentR, accentG, accentB, hoverDarken)
		}
		primary := toHex(r, g, b)
		for path, value := range map[string]string{
			"color.accent.default": toHex(accentR, accentG, accentB),
			"color.accent.hover":   toHex(hoverR, hoverG, hoverB),
			"color.accent.on":      on,
			"color.signal":         primary,
			"color.focus":          primary,
		} {
			values[path] = value
			types[path] = tokens.TypeColor
		}
	}
	if fontKey != "" {
		stack, ok := fontStacks[fontKey]
		if !ok {
			return themes.TokenLayer{}, false, fmt.Errorf("branding: unknown font key %q", fontKey)
		}
		values["font.body"] = stack.body
		values["font.display"] = stack.display
		types["font.body"] = tokens.TypeFontFamily
		types["font.display"] = tokens.TypeFontFamily
	}

	return themes.TokenLayer{
		ID:   layerID,
		Kind: themes.LayerTenant,
		Tokens: tokens.Set{
			Name:   tokenNamespace,
			Values: values,
			Types:  types,
		},
	}, true, nil
}

// DeriveCSS renders the tenant overlay as a ready-to-serve :root block of
// --pk-* custom properties. It returns the empty string when both inputs are
// empty, so callers can skip emitting a stylesheet entirely.
func DeriveCSS(primaryHex, fontKey string) (string, error) {
	layer, ok, err := DeriveLayer(primaryHex, fontKey)
	if err != nil || !ok {
		return "", err
	}
	return tokens.CSSVars(layer.Tokens)
}

// inkLuminance is the WCAG relative luminance of inkHex, precomputed so the
// fallback guard in correctedAccent measures against the exact ink color.
var inkLuminance = func() float64 {
	r, g, b, err := parseHex(inkHex)
	if err != nil {
		panic(fmt.Sprintf("branding: inkHex invariant broken: %v", err))
	}
	return relativeLuminance(r, g, b)
}()

// correctedAccent darkens the primary until the emitted 8-bit color reaches
// WCAG AA contrast against white, per the policy in the file header. It
// returns the corrected accent channels plus the accent.on hex to pair with.
// The ink branch is defensive only: it is unreachable under the current
// constants (see inkHex), and if a constant change ever makes it reachable
// it still refuses to return a pair below AA, darkening further until white
// or ink measures at least 4.5:1 against the emitted accent.
func correctedAccent(r, g, b float64) (float64, float64, float64, string) {
	whiteLum := relativeLuminance(1, 1, 1)
	// Terminates: darken's factor floors at 0 once step*darkenStep reaches 1
	// (step 20 today), and black measures 21:1 against white.
	for step := 0; ; step++ {
		ar, ag, ab := darken(r, g, b, float64(step)*darkenStep)
		ar, ag, ab = quantize(ar), quantize(ag), quantize(ab)
		accentLum := relativeLuminance(ar, ag, ab)
		if contrastRatio(accentLum, whiteLum) >= tokens.WCAGAAContrast {
			return ar, ag, ab, whiteHex
		}
		if step >= maxDarkenSteps && contrastRatio(accentLum, inkLuminance) >= tokens.WCAGAAContrast {
			return ar, ag, ab, inkHex
		}
	}
}

// parseHex decodes #rgb and #rrggbb colors into channels in [0, 1]. All other
// forms, including alpha-bearing #rgba/#rrggbbaa, are rejected.
func parseHex(s string) (r, g, b float64, err error) {
	if !strings.HasPrefix(s, "#") {
		return 0, 0, 0, fmt.Errorf("branding: color %q must start with #", s)
	}
	digits := s[1:]
	switch len(digits) {
	case 3:
		digits = strings.Repeat(digits[0:1], 2) + strings.Repeat(digits[1:2], 2) + strings.Repeat(digits[2:3], 2)
	case 6:
		// Already canonical.
	default:
		return 0, 0, 0, fmt.Errorf("branding: color %q must have 3 or 6 hex digits", s)
	}
	var channels [3]float64
	for i := range channels {
		value, parseErr := strconv.ParseUint(digits[2*i:2*i+2], 16, 8)
		if parseErr != nil {
			return 0, 0, 0, fmt.Errorf("branding: color %q is not hexadecimal", s)
		}
		channels[i] = float64(value) / 255
	}
	return channels[0], channels[1], channels[2], nil
}

// toHex encodes channels in [0, 1] as a lowercase #rrggbb color.
func toHex(r, g, b float64) string {
	return fmt.Sprintf("#%02x%02x%02x", channelByte(r), channelByte(g), channelByte(b))
}

func channelByte(c float64) uint8 {
	return uint8(math.Round(clamp01(c) * 255))
}

// quantize snaps a channel onto the 8-bit grid so contrast is measured on the
// exact color that toHex will emit.
func quantize(c float64) float64 {
	return float64(channelByte(c)) / 255
}

func clamp01(c float64) float64 {
	return math.Min(math.Max(c, 0), 1)
}

// relativeLuminance implements WCAG 2.x relative luminance over sRGB channels
// in [0, 1]: channels are linearized, then weighted 0.2126/0.7152/0.0722.
func relativeLuminance(r, g, b float64) float64 {
	linear := func(c float64) float64 {
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*linear(r) + 0.7152*linear(g) + 0.0722*linear(b)
}

// contrastRatio implements the WCAG contrast ratio between two relative
// luminances: (lighter + 0.05) / (darker + 0.05).
func contrastRatio(l1, l2 float64) float64 {
	lighter := math.Max(l1, l2)
	darker := math.Min(l1, l2)
	return (lighter + 0.05) / (darker + 0.05)
}

// darken moves the color toward black by the given fraction of its original
// channel values (amount 0.05 = 5% darker; amounts are not compounded).
func darken(r, g, b, amount float64) (float64, float64, float64) {
	factor := clamp01(1 - amount)
	return r * factor, g * factor, b * factor
}
