package overlay

// overlay_test.go validates homepage overlay template loading, rendering, and
// helper behavior.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testPlan struct {
	MonthlyPrice int
}

type testView struct {
	Title string
	Plan  testPlan
}

func TestLoadTemplateSourceIncludesSortedPartials(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "homepage.template.html"), `{{ template "base" . }}`)
	mustWrite(t, filepath.Join(dir, "partials", "02_base.html"), `{{ define "base" }}{{ template "atom" . }} {{ .Title }}{{ end }}`)
	mustWrite(t, filepath.Join(dir, "partials", "01_atom.html"), `{{ define "atom" }}Atom{{ end }}`)

	source, err := LoadTemplateSource(dir, "en", "")
	if err != nil {
		t.Fatalf("LoadTemplateSource() error = %v", err)
	}
	if !strings.Contains(source, `{{ define "atom" }}`) || !strings.Contains(source, `{{ define "base" }}`) {
		t.Fatalf("expected partials in source, got %q", source)
	}
}

func TestLoadTemplateSourceRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "homepage.template.html"), `ok`)
	mustWrite(t, filepath.Join(dir, "..", "outside.html"), `outside`)

	if _, err := LoadTemplateSource(dir, "en", "../outside.html"); err == nil {
		t.Fatal("expected traversal template path to fail")
	}
}

func TestResolveAssetNamesRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "homepage.css"), `body {}`)
	mustWrite(t, filepath.Join(dir, "..", "outside.css"), `body { color: red }`)

	if _, err := ResolveAssetNames(dir, "en", []string{"../outside.css"}, "homepage.css"); err == nil {
		t.Fatal("expected traversal asset path to fail")
	}
}

func TestRenderFragmentUsesOverlayTemplateFuncs(t *testing.T) {
	html, err := RenderFragment(RenderInput{
		TemplateSource: `<a href="{{ link "/en/docs" }}">{{ .Title }}</a><img src="{{ asset "logo.svg" }}"><span>{{ price .Plan }}</span>`,
		View:           testView{Title: "PlatformKit", Plan: testPlan{MonthlyPrice: 1490}},
		AssetBase:      "/images/overlays/platformkit",
	})
	if err != nil {
		t.Fatalf("RenderFragment() error = %v", err)
	}
	for _, want := range []string{
		`href="/docs"`,
		`PlatformKit`,
		`src="/images/overlays/platformkit/logo.svg"`,
		`EUR 1490`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in %s", want, html)
		}
	}
}

func TestURLHelpersNormalizeAndRejectUnsafeSchemes(t *testing.T) {
	if got := ExternalURL("platformkit.dev/docs"); got != "https://platformkit.dev/docs" {
		t.Fatalf("ExternalURL() = %q", got)
	}
	if got := ExternalURL("javascript:alert(1)"); got != "" {
		t.Fatalf("ExternalURL() accepted unsafe scheme: %q", got)
	}
	if got := Mailto("hello@platformkit.dev"); got != "mailto:hello@platformkit.dev" {
		t.Fatalf("Mailto() = %q", got)
	}
	if got := Mailto("hello@platformkit.dev\r\nbcc:evil@example.com"); got != "" {
		t.Fatalf("Mailto() accepted header injection: %q", got)
	}
	if got := Tel("+351 211 000 000"); got != "tel:+351211000000" {
		t.Fatalf("Tel() = %q", got)
	}
	if got := Tel("javascript:alert(1)"); got != "" {
		t.Fatalf("Tel() accepted unsafe phone: %q", got)
	}
}

func TestPublicAssetURLAndBodyClass(t *testing.T) {
	if got := PublicAssetURL("platformkit", "homepage.css"); got != "/assets/overlays/platformkit/homepage.css" {
		t.Fatalf("PublicAssetURL() = %q", got)
	}
	if got := BodyClass("platformkit", "default_theme", "atlas"); got != "overlay-homepage-body overlay-homepage-platformkit overlay-theme-default-theme overlay-experience-atlas" {
		t.Fatalf("BodyClass() = %q", got)
	}
}

func TestDictRejectsInvalidPairs(t *testing.T) {
	if _, err := Dict("href"); err == nil {
		t.Fatal("expected odd pair count to fail")
	}
	if _, err := Dict(42, "/fleet"); err == nil {
		t.Fatal("expected non-string key to fail")
	}
}

func TestRenderFragment_SeptagonEditorialFixture(t *testing.T) {
	// Resolve the Septagon template path relative to this test's working
	// directory. The pk-modules repo lives at
	// septagon-oss-workspace/pk-modules; the template lives at
	// septagon-clients/septagon/apps/complete-saas/site/homepage.template.html.
	tmplPath := filepath.Join("..", "..", "..", "..", "..", "septagon-clients", "septagon", "apps", "complete-saas", "site", "homepage.template.html")
	if envPath := os.Getenv("SEPTAGON_OVERLAY_TEMPLATE"); envPath != "" {
		tmplPath = envPath
	}
	tmpl, err := os.ReadFile(tmplPath)
	if err != nil {
		t.Skipf("septagon overlay template not co-located: %v", err)
	}

	view := map[string]any{
		"Branding": map[string]string{"LogoAlt": "Septagon"},
		"Navbar": map[string]any{
			"Links":      []map[string]string{{"Title": "Manifesto", "Href": "#manifesto"}},
			"JoinUsText": "Start a project",
			"JoinUsHref": "#contact",
		},
		"Hero": map[string]string{
			"Title":         "We build the",
			"Subtitle":      "Studio for the unfair version.",
			"PrimaryLink":   "#contact",
			"PrimaryCTA":    "Start a project",
			"SecondaryLink": "#dispatches",
			"SecondaryCTA":  "See dispatches",
		},
		"Kinetic": map[string]any{"Words": []string{"exact", "evident"}},
		"Marquee": map[string]any{"Signal": []string{"platform engineering", "ai systems"}},
		"Sections": []map[string]any{
			{"ID": "system-design", "Title": "Design", "Subtitle": "Model the system."},
			{"ID": "execution", "Title": "Execute", "Subtitle": "Ship with control."},
			{"ID": "governance", "Title": "Govern", "Subtitle": "Keep control as it scales."},
		},
		"Field": []map[string]any{
			{"N": "01", "Title": "Platform engineering", "Body": "Contracts.", "Tags": []string{"Go"}, "Focus": false},
			{"N": "03", "Title": "Healthtech", "Body": "EHR.", "Tags": []string{"FHIR"}, "Focus": true},
		},
		"Dispatches": []map[string]any{
			{"Year": "2024", "Vertical": "Clinical", "Title": "EDC", "Body": "Body.", "Tags": []string{"Go"}, "Kpi": "9 sites", "Accent": "molten"},
		},
		"Phases": []map[string]any{
			{"N": "01", "Title": "Frame", "Body": "Map the system.", "Out": "Architecture index"},
			{"N": "02", "Title": "Build", "Body": "Embed engineers.", "Out": "Shipping cadence"},
		},
		"Contact": map[string]string{
			"Description": "Send us the hard one.",
			"Email":       "hello@septagon.dev",
		},
		"Footer": map[string]string{"Location": "Lisbon", "Address": "Remote"},
		"Social": []map[string]string{},
	}

	out, err := RenderFragment(RenderInput{
		TemplateSource: string(tmpl),
		View:           view,
		AssetBase:      "/assets/overlays/septagon",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	// Plate framing must be present.
	for _, want := range []string{
		"Plate 00 / Hero",
		"Plate I / Manifesto",
		"Plate II / Field manual",
		"Plate IV / Dispatches",
		"Plate V / Operating model",
		"Plate VII / Contact",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output missing plate framing %q", want)
		}
	}

	// Motion CSS vars must be referenced (not hardcoded literals).
	for _, want := range []string{
		"var(--motion-duration-marquee)",
		"var(--motion-duration-kinetic-cycle)",
		"var(--motion-duration-status-blink)",
		"var(--motion-duration-typewriter-out)",
		"var(--motion-duration-caret-blink)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output missing motion var %q", want)
		}
	}

	// Hardcoded animation durations must NOT have crept in.
	for _, badPattern := range []string{
		"animation-duration:60s",
		"caret-blink_0.85s",
	} {
		if strings.Contains(out, badPattern) {
			t.Errorf("rendered output contains hardcoded motion literal %q — should be a token var", badPattern)
		}
	}

	// Content keys must surface.
	if !strings.Contains(out, "hello@septagon.dev") {
		t.Error("contact email missing from rendered output")
	}
	if !strings.Contains(out, "Healthtech") {
		t.Error("focus discipline missing from rendered output")
	}
}

func mustWrite(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
