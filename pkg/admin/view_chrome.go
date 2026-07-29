// Implements: REQ-004.
// Per: ADR-0017, ADR-0022, ADR-0029, ADR-0031.
// Discipline: C-14.

package admin

// view_chrome.go owns the console's document chrome as typed Go views: the
// layout that every admin page shares (head, header, sidebar, content frame,
// toast), rendered with gomponents instead of text/template. The migration
// draws one deliberate line, recorded here because the next reader will ask:
//
//   - Components — buttons, fields, tables, status pills, tags, pagination,
//     empty states — come from pk-ui ONLY: its renderers server-side, its
//     exported class surface (web.ButtonClasses, web.TableClasses, …) for
//     what _admin.js builds at runtime. The admin declares no component
//     styling of its own; its only tw lists are page layout (layoutClasses
//     below), which arranges components without ever styling one.
//   - The console's voice — ruled-paper canvas, the brand mark, the dark
//     field-navigation rail, the hero's editorial typography — remains
//     product chrome in _admin.css, aligned with the design system through
//     the same --pk-* tokens (ADR-0022) but not pretending to be a reusable
//     component.
//
// The class-name bridge: _admin.js builds table rows, status pills, and tags
// in the browser, and must style them with the SAME compiled class lists the
// renderers use. The layout therefore embeds a JSON map of compiled pk-ui
// class strings (id "pk-classnames"); the script assigns them wholesale and
// never stacks two lists onto one element, mirroring pk-ui's own variant
// discipline. Classes stay declared exactly once, in pk-ui.

import (
	"encoding/json"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/pk-ui/contracts"
	"github.com/septagon-oss/pk-ui/render/web"
	"github.com/septagon-oss/tw"
)

// layoutClasses are the admin's page-arrangement lists: spacing, wrapping,
// and grid flow. They are deliberately the ONLY tw lists the admin declares —
// everything with a component identity (buttons, fields, tables, pills,
// tags, pagination, empty states) comes from pk-ui, either through its
// renderers or through its exported class surface. Layout is arrangement,
// not identity; it has no variants and nothing to collide with.
var layoutClasses = struct {
	TagList     tw.ClassList
	FormGrid    tw.ClassList
	FieldWide   tw.ClassList
	FormStack   tw.ClassList
	ActionsRow  tw.ClassList
	SecretStack tw.ClassList
	SecretRow   tw.ClassList
}{
	TagList: tw.New().Display(tw.DisplayInlineFlex).Items(tw.ItemsCenter).Gap(tw.S1).FlexWrap(),
	FormGrid: tw.New().Display(tw.DisplayGrid).GridCols(1).Gap(tw.S5).MarginY(tw.S2).
		Breakpoint(tw.BreakpointMD, func(c tw.ClassList) tw.ClassList { return c.GridCols(2) }),
	FieldWide:   tw.New().ColSpanFull(),
	FormStack:   tw.New().Display(tw.DisplayFlex).FlexDir(tw.FlexCol).Gap(tw.S6).MarginY(tw.S4),
	ActionsRow:  tw.New().Display(tw.DisplayFlex).Items(tw.ItemsCenter).Gap(tw.S3),
	SecretStack: tw.New().Display(tw.DisplayFlex).FlexDir(tw.FlexCol).Gap(tw.S3).MarginY(tw.S4),
	SecretRow:   tw.New().Display(tw.DisplayFlex).Items(tw.ItemsCenter).Gap(tw.S3).FlexWrap(),
}

// classNamesJSON renders the bridge payload _admin.js styles runtime-built
// elements with. Every component string is a COMPLETE pk-ui class list —
// the script assigns them, it never stacks two lists that could contest a
// property (the same variant discipline pk-ui enforces internally). Keys
// are the script's vocabulary; values are compiled pk-ui lists, so the
// design system stays declared exactly once, in pk-ui.
func classNamesJSON() string {
	table := web.TableClasses()
	payload := map[string]string{
		"statusPositive": web.BadgeClasses("success").Compile(),
		"statusWarning":  web.BadgeClasses("warning").Compile(),
		"statusDanger":   web.BadgeClasses("error").Compile(),
		"statusNeutral":  web.BadgeClasses("default").Compile(),

		"tag":     web.TagClasses(false).Compile(),
		"tagList": layoutClasses.TagList.Compile(),

		"row":       table.Row.Compile(),
		"td":        table.Td.Compile(),
		"tdPrimary": table.TdPrimary.Compile(),
		"cellNote":  table.CellNote.Compile(),

		"rowActions":   table.ActionsCell.Compile(),
		"tableAction":  web.ButtonClasses("secondary", "xs").Compile(),
		"dangerAction": web.ButtonClasses("error", "xs").Compile(),

		"statusTextIdle":  web.TextClasses("muted", "sm", "").Compile(),
		"statusTextError": web.TextClasses("danger", "sm", "").Compile(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		// The payload is a map of compiled constants; failure here is a
		// programmer error, and an empty object degrades to unstyled rows
		// rather than a broken console.
		return "{}"
	}
	return string(raw)
}

// viewClassLists returns the admin-declared lists the served stylesheet must
// carry beyond web.ClassLists(): only the layout lists above. Every
// component rule already flows from pk-ui's registry.
func viewClassLists() []tw.ClassList {
	return []tw.ClassList{
		layoutClasses.TagList, layoutClasses.FormGrid,
		layoutClasses.FieldWide, layoutClasses.FormStack, layoutClasses.ActionsRow,
		layoutClasses.SecretStack, layoutClasses.SecretRow,
	}
}

// contractProps builds the ComponentProps for an element _admin.js binds by
// id, optionally with extra contract attributes.
func contractProps(id string, attrs map[string]string) contracts.ComponentProps {
	return contracts.ComponentProps{ID: id, Attrs: attrs}
}

// hiddenProps builds the ComponentProps for a panel the script reveals.
func hiddenProps(id string) contracts.ComponentProps {
	return contracts.ComponentProps{ID: id, Hidden: true}
}

// effectiveTitle is the branded workspace name when the tenant set one, else
// the configured shell title. One decision point for the <title> tag, the
// header wordmark, and the brand link's aria-label.
func effectiveTitle(view shellView) string {
	if view.DisplayName != "" {
		return view.DisplayName
	}
	return view.Title
}

// brandVisual is the mark inside the brand link: the tenant's uploaded logo
// when branding resolved one, else the stock 3-span pk-brand-mark. One node
// replaces the other — the chrome never renders both.
func brandVisual(view shellView) g.Node {
	if view.LogoURL != "" {
		return h.Img(h.Class("pk-brand-logo"), h.Src(view.LogoURL), h.Alt(view.LogoAlt))
	}
	return h.Span(h.Class("pk-brand-mark"), g.Attr("aria-hidden", "true"), h.Span(), h.Span(), h.Span())
}

// faviconHref points the tab icon at the tenant logo when branding resolved
// one, else at the embedded default mark.
func faviconHref(view shellView) string {
	if view.LogoURL != "" {
		return view.LogoURL
	}
	return view.BasePath + "/static/favicon.svg"
}

// layout renders the shared document. The unbranded chrome markup and class
// names stay byte-compatible with the previous template (plus the default
// favicon link) so the shell's identity CSS and every existing test assertion
// keep holding; tenant branding swaps the title, mark, favicon, and adds the
// per-tenant stylesheet after _admin.css so overrides win the cascade.
func layout(view shellView, content g.Node) g.Node {
	title := effectiveTitle(view)
	return h.Doctype(h.HTML(h.Lang("en"),
		h.Head(
			h.Meta(h.Charset("utf-8")),
			h.Meta(h.Name("viewport"), h.Content("width=device-width,initial-scale=1,viewport-fit=cover")),
			h.Meta(h.Name("color-scheme"), h.Content("light")),
			h.TitleEl(g.Text(view.PageTitle+" · "+title)),
			h.Link(h.Rel("icon"), h.Href(faviconHref(view))),
			h.Link(h.Rel("stylesheet"), h.Href(view.BasePath+"/static/_admin.css")),
			g.If(view.HasBrandingCSS,
				h.Link(h.Rel("stylesheet"), h.Href(view.BasePath+"/static/_branding.css")),
			),
			h.Script(h.Type("application/json"), h.ID("pk-classnames"), g.Raw(classNamesJSON())),
			h.Script(g.Attr("defer"), h.Src(view.BasePath+"/static/_admin.js")),
		),
		h.Body(
			h.A(h.Class("pk-skip-link"), h.Href("#main-content"), g.Text("Skip to content")),
			h.Header(h.Class("pk-admin-header"),
				h.A(h.Class("pk-brand"), h.Href(view.BasePath), g.Attr("aria-label", title+" overview"),
					brandVisual(view),
					h.Span(
						h.Strong(g.Text(title)),
						h.Small(g.Text("Operator workspace")),
					),
				),
				h.Div(h.Class("pk-header-tools"),
					g.If(view.TenantID != "",
						h.Div(h.Class("pk-identity"), h.Title("Signed in as "+view.Subject+" for tenant "+view.TenantID),
							h.Span(h.Class("pk-presence"), g.Attr("aria-hidden", "true")),
							h.Span(h.Class("pk-identity-copy"),
								h.Strong(g.Text(view.Subject)),
								h.Small(g.Text(view.TenantID)),
							),
						),
					),
					h.Form(h.Method("post"), h.Action(view.BasePath+"/logout"),
						h.Button(h.Class("pk-icon-btn"), h.Type("submit"), g.Attr("aria-label", "Sign out"), g.Text("Sign out")),
					),
					h.Details(h.Class("pk-mobile-nav"),
						h.Summary(g.Attr("aria-label", "Open navigation"), g.Text("Menu")),
						h.Nav(g.Attr("aria-label", "Mobile navigation"),
							overviewLink(view),
							sidebarSections(view, "mnav"),
						),
					),
				),
			),
			h.Div(h.Class("pk-admin-layout"),
				h.Aside(h.Class("pk-admin-sidebar"), g.Attr("aria-label", "Primary navigation"),
					h.Nav(
						h.A(h.Class("pk-overview-link"), h.Href(view.BasePath), currentPage(view.CurrentPath == view.BasePath),
							h.Span(g.Attr("aria-hidden", "true"), g.Text("⌂")), g.Text(" Overview"),
						),
						sidebarSections(view, "nav"),
					),
					h.Div(h.Class("pk-sidebar-foot"),
						h.Span(g.Text("PlatformKit OSS")),
						h.A(h.Href("/healthz"), g.Text("System health ↗")),
					),
				),
				h.Main(h.Class("pk-admin-content"), h.ID("main-content"), h.TabIndex("-1"),
					h.Div(h.Class("pk-content-frame"), content),
				),
			),
			h.Div(h.Class("pk-toast"), h.ID("pk-toast"), h.Role("status"), g.Attr("aria-live", "polite"), g.Attr("hidden")),
		),
	))
}

// overviewLink is the mobile-nav variant of the overview entry.
func overviewLink(view shellView) g.Node {
	return h.A(h.Class("pk-overview-link"), h.Href(view.BasePath),
		currentPage(view.CurrentPath == view.BasePath), g.Text("Overview"))
}

// currentPage marks the active navigation entry for assistive tech.
func currentPage(active bool) g.Node {
	if !active {
		return nil
	}
	return g.Attr("aria-current", "page")
}

// sidebarSections renders the registered navigation rail. idPrefix scopes the
// heading ids to the rendering surface: the chrome renders this rail twice
// (desktop aside and mobile drawer), and without a per-surface prefix every
// heading id appeared twice in the document — invalid HTML that leaves
// aria-labelledby pointing at an ambiguous target.
func sidebarSections(view shellView, idPrefix string) g.Node {
	if len(view.Sidebar) == 0 {
		return h.P(h.Class("pk-nav-empty"), g.Text("No management areas registered."))
	}
	var sections []g.Node
	for _, section := range view.Sidebar {
		items := make([]g.Node, 0, len(section.Items))
		for _, item := range section.Items {
			items = append(items, h.Li(
				h.A(h.Href(item.Path), currentPage(view.CurrentPath == item.Path),
					h.Span(g.Text(item.Label)),
					h.Span(h.Class("pk-nav-arrow"), g.Attr("aria-hidden", "true"), g.Text("›")),
				),
			))
		}
		// The heading id derives from the label, not a ModuleID: a rendered
		// group is a category that may span several modules (SidebarSections
		// merges same-label declarations), and labels are unique post-merge
		// while a module registering two differently-labelled sections would
		// repeat its id.
		headingID := idPrefix + "-" + navSlug(section.Label)
		sections = append(sections, h.Section(h.Class("pk-nav-section"), g.Attr("aria-labelledby", headingID),
			h.H2(h.ID(headingID), g.Text(section.Label)),
			h.Ul(items...),
		))
	}
	return g.Group(sections)
}

// navSlug reduces a group label to a stable DOM id fragment: lowercase
// alphanumerics with single hyphens ("API keys" -> "api-keys"). Labels are
// unique after SidebarSections merges by label, so the derived ids are too.
func navSlug(label string) string {
	var b strings.Builder
	b.Grow(len(label))
	pendingHyphen := false
	for _, r := range strings.ToLower(label) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			if pendingHyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingHyphen = false
			b.WriteRune(r)
		default:
			pendingHyphen = true
		}
	}
	if b.Len() == 0 {
		return "section"
	}
	return b.String()
}
