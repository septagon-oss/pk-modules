package overlay

// Validates: REQ-SITE-001.
// Per: ADR-0032.
// Discipline: C-14.
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

func TestNormalizePublicLinkRejectsUnsafeLinks(t *testing.T) {
	cases := map[string]string{
		"/en/docs":                     "/docs",                        // internal: locale stripped
		"https://platformkit.dev/docs": "https://platformkit.dev/docs", // absolute https: kept
		"http://platformkit.dev":       "http://platformkit.dev",       // absolute http: kept
		"javascript:alert(1)":          "",                             // dangerous scheme: rejected
		"data:text/html,<script>":      "",                             // data: rejected
		"//evil.example/x":             "",                             // protocol-relative: rejected
		"///evil.example/x":            "",                             // triple-slash: rejected
		`/\evil.example/x`:             "",                             // backslash trick: rejected
		`\\evil.example\x`:             "",                             // backslash protocol-relative: rejected
		`\/evil.example/x`:             "",                             // mixed slash/backslash: rejected
	}
	for in, want := range cases {
		if got := NormalizePublicLink(in); got != want {
			t.Errorf("NormalizePublicLink(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAssetHelperRejectsProtocolRelative(t *testing.T) {
	asset, ok := FuncMap("/images/overlays/platformkit", nil)["asset"].(func(string) string)
	if !ok {
		t.Fatal("asset helper missing or wrong type")
	}
	for _, bad := range []string{"//evil.example/x.css", `/\evil.example/x.css`, `\\evil.example\x.css`} {
		if got := asset(bad); got != "" {
			t.Errorf("asset(%q) = %q, want empty (protocol-relative/backslash)", bad, got)
		}
	}
	if got := asset("logo.svg"); got != "/images/overlays/platformkit/logo.svg" {
		t.Errorf("asset(relative) = %q", got)
	}
}

func TestPublicAssetURLRejectsTraversalSlug(t *testing.T) {
	for _, slug := range []string{"../secret", "..", "../", "a/b", "/../x", ".", "%2e%2e", "%2f", `a\b`, "a b", ""} {
		if got := PublicAssetURL(slug, "homepage.css"); got != "" {
			t.Errorf("PublicAssetURL(%q, ...) = %q, want empty (slug must not escape base)", slug, got)
		}
	}
	// Percent-encoded and backslash traversal in the relative path are rejected
	// (literal "../" is safely collapsed by path.Clean and stays under the base).
	for _, rel := range []string{"%2e%2e/secret.css", "%2e%2e/%2e%2e/secret.css", "a/%2e%2e/b.css", "x%2fy.css", `..\secret.css`, `a\..\..\b.css`} {
		if got := PublicAssetURL("platformkit", rel); got != "" {
			t.Errorf("PublicAssetURL(platformkit, %q) = %q, want empty (encoded/backslash traversal)", rel, got)
		}
	}
	// Literal "../" is collapsed to stay under the base, not an escape.
	if got := PublicAssetURL("platformkit", "../secret.css"); got != "/assets/overlays/platformkit/secret.css" {
		t.Errorf("PublicAssetURL(literal ..) = %q, want collapsed under base", got)
	}
	// A valid slug with hyphen and underscore, and a nested asset path, are kept.
	if got := PublicAssetURL("comum-cowork_1", "homepage.css"); got != "/assets/overlays/comum-cowork_1/homepage.css" {
		t.Errorf("PublicAssetURL(valid slug) = %q", got)
	}
	if got := PublicAssetURL("platformkit", "partials/header.css"); got != "/assets/overlays/platformkit/partials/header.css" {
		t.Errorf("PublicAssetURL(nested path) = %q", got)
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

func mustWrite(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
