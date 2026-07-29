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
// Two review fixes worth calling out explicitly:
//
//   - The Skip button carries formnovalidate. Without it, a first-login
//     visitor who clicks Skip on the untouched (and therefore genuinely
//     empty) form gets blocked by the display_name field's own `required`
//     constraint — the exact form Skip exists to let them leave without
//     filling in.
//   - Brand color is a radio pair (color_mode=default|custom), not a color
//     input plus a "clear" checkbox. An HTML <input type="color"> can never
//     submit "empty" — even one nobody touched still posts its browser
//     default, #000000 — so a bare color input plus an unchecked-by-default
//     checkbox would silently persist black on an untouched first-login
//     form. Pairing it with a radio that defaults to "default palette"
//     means the submitted primary_color is only trusted when color_mode is
//     explicitly "custom" (see handler.go's handleSave); "default" (the
//     initial state) always clears the palette, whatever the browser put in
//     the color input.
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
// this module's category — the same one tenant/admin.go uses (see
// adminSidebarOrder below).
const adminSidebarLabel = "Workspace"

// adminPagePathSuffix is appended to the normalized admin base path
// (WithAdminBasePath) to build the page's absolute route, e.g.
// "/admin" + "/branding" = "/admin/branding". handler.go's redirectSaved and
// redirectError share this exact constant for their own "<adminBasePath>
// + .../branding" targets, so the page's route and the form's redirect
// destination can never drift apart into two hand-typed "/branding"
// literals.
const adminPagePathSuffix = "/branding"

// adminSidebarOrder only matters in a host that does NOT also compose
// tenant_management. tenant/admin.go registers its own SidebarSection under
// this exact same Label ("Workspace") at Order 10, and shell.go's
// SidebarSections merges every section sharing a Label into one group
// anchored at its EARLIEST member's Order (see that method's doc comment) —
// so in the common case (tenant_management present) the merged "Workspace"
// group sits wherever tenant put it, at Order 10, regardless of what
// branding declares here. 95 only decides where the group lands when
// branding is the *sole* module registering "Workspace" — a plausible
// standalone-module composition, and the reason this is deliberately a
// high number: branding is a settings surface, not a managed entity list,
// and reads naturally last among any other sole members of that group.
const adminSidebarOrder = 95

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

// fallbackLogoAlt is the <img> alt text used when a tenant has a logo but no
// declared LogoAlt — an empty alt attribute on a meaningful (non-decorative)
// image fails basic accessibility review, so the preview always describes
// itself even before the operator fills in real alt text.
const fallbackLogoAlt = "Current logo"

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
	// LogoAlt is the raw stored value: it prefills the logo_alt text input
	// exactly, including when empty, so the operator sees what is actually on
	// record rather than a synthesized placeholder.
	LogoAlt string
	// LogoAltDisplay is what the <img> alt attribute renders: LogoAlt, or
	// fallbackLogoAlt when it is empty, so the preview is never announced to
	// assistive tech as unlabeled.
	LogoAltDisplay string
	Saved          bool
	HasError       bool
	ErrorMsg       string
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

	logoAltDisplay := profile.LogoAlt
	if logoAltDisplay == "" {
		logoAltDisplay = fallbackLogoAlt
	}

	data := brandingPageData{
		BasePath:       h.adminBasePath,
		APIPath:        apiPath,
		SetupComplete:  profile.SetupComplete,
		DisplayName:    profile.DisplayName,
		HasColor:       profile.PrimaryColor != "",
		PrimaryColor:   profile.PrimaryColor,
		FontOptions:    buildFontOptions(profile.FontKey),
		HasLogo:        profile.LogoURL != "",
		LogoURL:        profile.LogoURL,
		LogoAlt:        profile.LogoAlt,
		LogoAltDisplay: logoAltDisplay,
		Saved:          r.URL.Query().Get("saved") == "1",
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
// form itself is plain semantic HTML (each row a <p> for the browser's
// default block rhythm, one <fieldset> for the brand-color radio pair),
// inheriting body/typography/focus styles from the same stylesheet rather
// than forcing an ill-fitting component class onto a control _admin.css
// never described one for. "pk-logo-preview" on the <img> is backed by the
// documented hook rule Task 7 added to _admin.css.
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
{{if .SetupComplete}}<h1>Branding</h1>
<p class="pk-lede">Your workspace's name, logo, and colors — changes apply immediately.</p>{{else}}<h1>Set up your workspace</h1>
<p class="pk-lede">Name your workspace, add your logo and brand color — or skip and do this any time from Admin → Branding.</p>{{end}}
</div>
</header>

{{if .Saved}}<p role="status">Saved.</p>{{end}}
{{if .HasError}}<p role="alert">Couldn't save: {{.ErrorMsg}}</p>{{end}}

<form method="post" action="{{.APIPath}}" enctype="multipart/form-data">
<p>
<label for="display_name">Workspace name</label>
<input type="text" id="display_name" name="display_name" value="{{.DisplayName}}" required maxlength="120">
</p>

<fieldset>
<legend>Brand color</legend>
<p>
<label><input type="radio" name="color_mode" value="default"{{if not .HasColor}} checked{{end}}> Default palette</label>
</p>
<p>
<label><input type="radio" name="color_mode" value="custom"{{if .HasColor}} checked{{end}}> Custom color</label>
<input type="color" id="primary_color" name="primary_color" aria-label="Custom color value"{{if .HasColor}} value="{{.PrimaryColor}}"{{end}}>
</p>
</fieldset>

<p>
<label for="font_key">Font</label>
<select id="font_key" name="font_key" aria-describedby="font-help">
{{range .FontOptions}}<option value="{{.Value}}"{{if .Selected}} selected{{end}}>{{.Label}}</option>
{{end}}</select>
</p>
<p id="font-help">Editorial is a serif; Grotesk and Plex are sans-serifs.</p>

<p>
<label for="logo">Logo</label>
<input type="file" id="logo" name="logo" accept="image/png,image/jpeg,image/webp,image/svg+xml" aria-describedby="logo-help">
</p>
<p id="logo-help">PNG, JPEG, WebP, or SVG up to 1 MiB.</p>

{{if .HasLogo}}
<p><img src="{{.LogoURL}}" alt="{{.LogoAltDisplay}}" width="64" class="pk-logo-preview"></p>
{{end}}
<p>
<label for="logo_alt">Logo alt text</label>
<input type="text" id="logo_alt" name="logo_alt" value="{{.LogoAlt}}" maxlength="120">
</p>

<p>
<button type="submit" name="action" value="save">Save</button>
{{if not .SetupComplete}}<button type="submit" name="action" value="skip" formnovalidate>Skip for now</button>{{end}}
</p>
</form>
</div>
</main>
</body>
</html>
`
