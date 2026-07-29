// Validates: REQ-BRANDING-001.
// Per: ADR-0017.
// Discipline: C-14.

package branding_test

// palette_test.go validates WCAG-corrected palette derivation through the
// external test package: emitted custom-property names must match the default
// theme's exactly, and every emitted accent pair must be independently
// measurable at WCAG AA 4.5:1 via the canonical pk-design contrast helper.
// It also pins the documented reachability claim: the ink fallback stays
// unreachable while the current darkening constants hold.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-10 (shared builders return errors), C-14 (every Go file declares its purpose).

import (
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/pk-design/pkg/themes"
	"github.com/septagon-oss/pk-design/pkg/tokens"

	"github.com/septagon-oss/pk-modules/pkg/branding"
)

// cssVarValue extracts the value of one custom property from a :root block.
func cssVarValue(t *testing.T, css, name string) string {
	t.Helper()
	for _, line := range strings.Split(css, "\n") {
		line = strings.TrimSpace(line)
		if value, ok := strings.CutPrefix(line, name+": "); ok {
			return strings.TrimSuffix(value, ";")
		}
	}
	t.Fatalf("css is missing %s:\n%s", name, css)
	return ""
}

func TestDeriveCSSEmitsExactDefaultThemeVarNames(t *testing.T) {
	tests := []struct {
		name    string
		primary string
		fontKey string
		want    []string
		reject  []string
	}{
		{
			name:    "color only overrides the five color vars and no fonts",
			primary: "#14b8a6",
			want: []string{
				"--pk-color-accent-default",
				"--pk-color-accent-hover",
				"--pk-color-accent-on",
				"--pk-color-signal",
				"--pk-color-focus",
			},
			reject: []string{"--pk-font-"},
		},
		{
			name:    "font only overrides body and display and no colors",
			fontKey: "editorial",
			want:    []string{"--pk-font-body", "--pk-font-display"},
			reject:  []string{"--pk-color-"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			css, err := branding.DeriveCSS(tt.primary, tt.fontKey)
			if err != nil {
				t.Fatalf("DeriveCSS(%q, %q): %v", tt.primary, tt.fontKey, err)
			}
			for _, want := range tt.want {
				if !strings.Contains(css, want) {
					t.Errorf("css is missing %s:\n%s", want, css)
				}
			}
			for _, reject := range tt.reject {
				if strings.Contains(css, reject) {
					t.Errorf("css must not contain %s:\n%s", reject, css)
				}
			}
		})
	}
}

func TestDeriveCSSSignalAndFocusAreTheRawPrimary(t *testing.T) {
	css, err := branding.DeriveCSS("#14b8a6", "")
	if err != nil {
		t.Fatalf("DeriveCSS: %v", err)
	}
	if got := cssVarValue(t, css, "--pk-color-signal"); got != "#14b8a6" {
		t.Errorf("signal = %q, want raw primary #14b8a6", got)
	}
	if got := cssVarValue(t, css, "--pk-color-focus"); got != "#14b8a6" {
		t.Errorf("focus = %q, want raw primary #14b8a6", got)
	}
}

func TestDeriveCSSAccentPairMeetsWCAGAA(t *testing.T) {
	// #f5e642 is the correction property: a light primary must still yield an
	// accessible emitted pair, whichever branch of the policy produced it.
	for _, primary := range []string{"#14b8a6", "#f5e642"} {
		t.Run(primary, func(t *testing.T) {
			css, err := branding.DeriveCSS(primary, "")
			if err != nil {
				t.Fatalf("DeriveCSS(%q, \"\"): %v", primary, err)
			}
			accent := cssVarValue(t, css, "--pk-color-accent-default")
			on := cssVarValue(t, css, "--pk-color-accent-on")
			ratio, err := tokens.ContrastRatio(on, accent)
			if err != nil {
				t.Fatalf("ContrastRatio(%q, %q): %v", on, accent, err)
			}
			if ratio < tokens.WCAGAAContrast {
				t.Errorf("contrast(%s on %s) = %.3f, want >= %.1f", on, accent, ratio, tokens.WCAGAAContrast)
			}
		})
	}
}

func TestDeriveCSSFontStacksMatchTheCuratedMap(t *testing.T) {
	css, err := branding.DeriveCSS("", "editorial")
	if err != nil {
		t.Fatalf("DeriveCSS: %v", err)
	}
	wantStack := `"Iowan Old Style", "Palatino Linotype", Palatino, Georgia, serif`
	if got := cssVarValue(t, css, "--pk-font-body"); got != wantStack {
		t.Errorf("font body = %q, want %q", got, wantStack)
	}
	if got := cssVarValue(t, css, "--pk-font-display"); got != wantStack {
		t.Errorf("font display = %q, want %q", got, wantStack)
	}
}

func TestDeriveCSSNoOverlayReturnsEmpty(t *testing.T) {
	css, err := branding.DeriveCSS("", "")
	if err != nil {
		t.Fatalf("DeriveCSS(\"\", \"\"): %v", err)
	}
	if css != "" {
		t.Errorf("css = %q, want empty string when nothing is overridden", css)
	}
}

func TestDeriveCSSRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		primary string
		fontKey string
	}{
		{name: "missing hash", primary: "14b8a6"},
		{name: "non-hex digits", primary: "#zzz"},
		{name: "four digits", primary: "#1234"},
		{name: "unknown font key", fontKey: "comic"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := branding.DeriveCSS(tt.primary, tt.fontKey); err == nil {
				t.Errorf("DeriveCSS(%q, %q) = nil error, want error", tt.primary, tt.fontKey)
			}
		})
	}
}

func TestDeriveLayerProducesATenantTokenLayer(t *testing.T) {
	layer, ok, err := branding.DeriveLayer("#14b8a6", "plex")
	if err != nil {
		t.Fatalf("DeriveLayer: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true when overrides exist")
	}
	if layer.Kind != themes.LayerTenant {
		t.Errorf("Kind = %q, want %q", layer.Kind, themes.LayerTenant)
	}
	if strings.TrimSpace(layer.ID) == "" {
		t.Error("ID is empty, want a stable layer identity")
	}
	css, err := tokens.CSSVars(layer.Tokens)
	if err != nil {
		t.Fatalf("CSSVars(layer.Tokens): %v", err)
	}
	for _, want := range []string{"--pk-color-accent-default", "--pk-font-body"} {
		if !strings.Contains(css, want) {
			t.Errorf("layer css is missing %s:\n%s", want, css)
		}
	}
}

// TestInkFallbackStaysUnreachableUnderCurrentConstants sweeps a coarse RGB
// grid (16-step stride plus 255 on every channel, including the worst case
// #ffffff) and asserts the white branch of the accent policy wins for every
// primary. palette.go documents the ink fallback as unreachable while
// darkenStep = 5% and maxDarkenSteps = 12 hold; if a constant change ever
// flips that, this test fails so the constants and the policy documentation
// get revisited together instead of the behavior shifting silently.
func TestInkFallbackStaysUnreachableUnderCurrentConstants(t *testing.T) {
	channelValues := make([]int, 0, 17)
	for v := 0; v < 256; v += 16 {
		channelValues = append(channelValues, v)
	}
	channelValues = append(channelValues, 255)
	for _, r := range channelValues {
		for _, g := range channelValues {
			for _, b := range channelValues {
				primary := fmt.Sprintf("#%02x%02x%02x", r, g, b)
				layer, ok, err := branding.DeriveLayer(primary, "")
				if err != nil || !ok {
					t.Fatalf("DeriveLayer(%q, \"\") = ok=%v, err=%v", primary, ok, err)
				}
				token, found, err := layer.Tokens.Lookup("color.accent.on")
				if err != nil || !found {
					t.Fatalf("Lookup(color.accent.on) for %q: found=%v, err=%v", primary, found, err)
				}
				if token.Value != "#ffffff" {
					t.Fatalf("ink fallback became reachable: accent.on = %v for primary %s; "+
						"did darkenStep or maxDarkenSteps change? Revisit the reachability notes in palette.go",
						token.Value, primary)
				}
			}
		}
	}
}

func TestDeriveLayerNoOverlayReportsNotOK(t *testing.T) {
	layer, ok, err := branding.DeriveLayer("", "")
	if err != nil {
		t.Fatalf("DeriveLayer(\"\", \"\"): %v", err)
	}
	if ok {
		t.Errorf("ok = true, want false when nothing is overridden (layer %+v)", layer)
	}
}
