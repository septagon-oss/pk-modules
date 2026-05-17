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
