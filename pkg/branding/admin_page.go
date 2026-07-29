// Implements: REQ-BRANDING-001.
// Per: ADR-0017.
// Discipline: C-14.

package branding

// admin_page.go owns the Branding admin page: first-login setup and ongoing
// settings rendered as ONE surface (Task 6), registered into the host admin
// shell by registerAdmin (module.go). The module cannot import pkg/admin's
// component library — cross-module Go imports are the exact thing ADR-0009
// (ports-only communication) and pk-guard's importboundary analyzer forbid —
// so this page is a self-contained html/template document, styled entirely
// by classes already served at "<adminBasePath>/static/_admin.css" (the same
// stylesheet the shell links into its own chrome). It carries no inline
// <style> and no <script>: the shell's ServeHTTP sets a CSP of
// "style-src 'self'; script-src 'self'" on every response it serves,
// including custom pages registered via portslib.AdminPage, so an inline
// style block would simply be dropped by the browser.
//
// Copy is adaptive on portslib.BrandingProfile.SetupComplete: a tenant with
// no branding record yet sees first-login setup copy plus a "Skip for now"
// action; a tenant with a completed record sees plain settings copy and no
// skip button. Both paths post the same multipart form to handler.go's
// POST /api/v1/branding, which redirects back here with ?saved=1 or
// ?error=<message> — this file only has to render those two query states,
// never hold flash state itself.
//
// Every value the template interpolates — including ErrorMsg, which carries
// attacker-controlled text straight from the query string — goes through a
// plain {{.Field}} substitution. html/template contextually auto-escapes
// every one of them; there is no template.HTML/template.JS/template.URL
// escape hatch anywhere in this file, which is what keeps a crafted
// ?error=<script>... value inert in the rendered page.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"

	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

// adminPageTitle is both the portslib.AdminPage.Title the shell shows in its
// module summaries and the sidebar item's label — the page and its one nav
// entry share a name by design.
const adminPageTitle = "Branding"

// adminSidebarLabel is the sidebar group this page's single entry lives
// under. Every OSS reference module's own admin.go groups by a *category*,
// not a module name (see shell.go's SidebarSections doc); "Workspace" is
// this module's category.
const adminSidebarLabel = "Workspace"

// adminPagePathSuffix is appended to the normalized admin base path
// (WithAdminBasePath) to build the page's absolute route, e.g.
// "/admin" + "/branding" = "/admin/branding".
const adminPagePathSuffix = "/branding"

// adminSidebarOrder is deliberately high so Workspace lands after the
// entity-backed sections every other reference module registers (tenant 10,
// user 20, apikey 30, notification 50, health 80, audit 90) — branding is a
// settings surface, not a managed entity list, and reads naturally last.
const adminSidebarOrder = 90

// fontOptions is the curated font catalog the <select> offers, in display
// order. Values must match palette.go's fontStacks keys exactly; the empty
// value is "no override" (the default theme stack) and is deliberately
// first.
var fontOptions = []struct{ Value, Label string }{
	{"", "Default"},
	{"editorial", "Editorial"},
	{"grotesk", "Grotesk"},
	{"plex", "Plex"},
}

// brandingPageTemplate is parsed once at package init; a malformed template
// is a programmer error that should fail fast at process start, matching
// template.Must's contract.
var brandingPageTemplate = template.Must(template.New("branding-admin-page").Parse(brandingPageHTML))

// brandingPageData is the template's complete view model.
type brandingPageData struct {
	BasePath      string
	APIPath       string
	SetupComplete bool
	DisplayName   string
	HasColor      bool
	PrimaryColor  string
	FontOptions   []fontOptionView
	HasLogo       bool
	LogoURL       string
	LogoAlt       string
	Saved         bool
	HasError      bool
	ErrorMsg      string
}

// fontOptionView is one <option> the font <select> renders.
type fontOptionView struct {
	Value    string
	Label    string
	Selected bool
}

// adminPageHandler renders the Branding admin page for the module's own
// service and admin base path. It is a small, unexported adapter so
// registerAdmin can hand the shell a bound http.HandlerFunc without the page
// needing package-level mutable state.
type adminPageHandler struct {
	svc           *Service
	adminBasePath string
}

// Render implements portslib.AdminPage.Render. The admin shell's ServeHTTP
// owns routing and headers up to this call (see pkg/admin/shell.go
// ServeHTTP -> findPage -> page.Render(w, r)); from here this handler owns
// the entire HTTP response, exactly like the shell's own page renderers, so
// it writes a complete document rather than a fragment.
func (h *adminPageHandler) Render(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := portslib.RequestActor(w, r)
	if !ok {
		return
	}

	profile, err := h.svc.ResolveBranding(r.Context(), tenantID)
	if err != nil {
		http.Error(w, "branding admin page: could not resolve tenant branding", http.StatusInternalServerError)
		return
	}

	data := brandingPageData{
		BasePath:      h.adminBasePath,
		APIPath:       apiPath,
		SetupComplete: profile.SetupComplete,
		DisplayName:   profile.DisplayName,
		HasColor:      profile.PrimaryColor != "",
		PrimaryColor:  profile.PrimaryColor,
		FontOptions:   buildFontOptions(profile.FontKey),
		HasLogo:       profile.LogoURL != "",
		LogoURL:       profile.LogoURL,
		LogoAlt:       profile.LogoAlt,
		Saved:         r.URL.Query().Get("saved") == "1",
	}
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		data.HasError = true
		data.ErrorMsg = errMsg
	}

	// Buffer first so a template execution failure becomes a clean 500, never
	// a torn/partial page — the same discipline pkg/admin's own Shell.render
	// follows.
	var buf bytes.Buffer
	if err := brandingPageTemplate.Execute(&buf, data); err != nil {
		http.Error(w, fmt.Sprintf("branding admin page: render: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	// justified: headers/status are already sent, so a client-disconnect write error is non-actionable here.
	_, _ = w.Write(buf.Bytes())
}

// buildFontOptions renders fontOptions with the profile's current font key
// marked Selected.
func buildFontOptions(selected string) []fontOptionView {
	out := make([]fontOptionView, 0, len(fontOptions))
	for _, opt := range fontOptions {
		out = append(out, fontOptionView{Value: opt.Value, Label: opt.Label, Selected: opt.Value == selected})
	}
	return out
}

// brandingPageHTML is the page's complete document. Structure only —
// component styling is deliberately minimal (see the file header on the CSP
// constraint): pk-page-head/pk-eyebrow/pk-lede/pk-backlink are chrome
// classes already served by _admin.css that read sensibly standalone; the
// form itself is plain semantic HTML, inheriting body/typography/focus
// styles from the same stylesheet rather than forcing an ill-fitting
// component class onto a control _admin.css never described one for.
const brandingPageHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<title>Branding · PlatformKit Admin</title>
<link rel="stylesheet" href="{{.BasePath}}/static/_admin.css">
</head>
<body>
<main class="pk-admin-content">
<div class="pk-content-frame">
<a class="pk-backlink" href="{{.BasePath}}">← Admin</a>
<header class="pk-page-head">
<div>
<p class="pk-eyebrow">Workspace</p>
{{if .SetupComplete}}<h1>Branding</h1>{{else}}<h1>Set up your workspace</h1>
<p class="pk-lede">Name your workspace, add your logo and brand color — or skip and do this any time from Admin → Branding.</p>{{end}}
</div>
</header>

{{if .Saved}}<p>Saved.</p>{{end}}
{{if .HasError}}<p>{{.ErrorMsg}}</p>{{end}}

<form method="post" action="{{.APIPath}}" enctype="multipart/form-data">
<div>
<label for="display_name">Workspace name</label>
<input type="text" id="display_name" name="display_name" value="{{.DisplayName}}" required maxlength="120">
</div>

<div>
<label for="primary_color">Brand color</label>
<input type="color" id="primary_color" name="primary_color"{{if .HasColor}} value="{{.PrimaryColor}}"{{end}}>
</div>
<div>
<label for="clear_color"><input type="checkbox" id="clear_color" name="clear_color" value="on"> Use the default palette instead</label>
</div>

<div>
<label for="font_key">Font</label>
<select id="font_key" name="font_key">
{{range .FontOptions}}<option value="{{.Value}}"{{if .Selected}} selected{{end}}>{{.Label}}</option>
{{end}}</select>
</div>

<div>
<label for="logo">Logo</label>
<input type="file" id="logo" name="logo" accept="image/png,image/jpeg,image/webp,image/svg+xml">
</div>

{{if .HasLogo}}
<div>
<img src="{{.LogoURL}}" alt="{{.LogoAlt}}" class="logo-preview">
<label for="logo_alt">Logo alt text</label>
<input type="text" id="logo_alt" name="logo_alt" value="{{.LogoAlt}}">
</div>
{{end}}

<div>
<button type="submit" name="action" value="save">Save</button>
{{if not .SetupComplete}}<button type="submit" name="action" value="skip">Skip for now</button>{{end}}
</div>
</form>
</div>
</main>
</body>
</html>
`
