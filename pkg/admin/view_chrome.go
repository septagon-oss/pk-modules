// Implements: REQ-004.
// Per: ADR-0017, ADR-0022, ADR-0029, ADR-0031.
// Discipline: C-14.

package admin

// view_chrome.go owns the console's document chrome as typed Go views: the
// layout that every admin page shares (head, header, sidebar, content frame,
// toast), rendered with gomponents instead of text/template. The migration
// draws one deliberate line, recorded here because the next reader will ask:
//
//   - System components — buttons, fields, tables, status, pagination, empty
//     states — come from pk-ui and tw. They are the same implementation any
//     module admin page composes, which is the point: one component system.
//   - The console's voice — ruled-paper canvas, the brand mark, the dark
//     field-navigation rail, the hero's editorial typography — remains
//     product chrome in _admin.css, aligned with the design system through
//     the same --pk-* tokens (ADR-0022) but not pretending to be a reusable
//     component.
//
// The class-name bridge: _admin.js builds table rows, status pills, and tags
// in the browser, and must style them with the SAME compiled class lists the
// Go views use. The layout therefore embeds a JSON map of compiled class
// strings (id "pk-classnames"); the script reads it, with the legacy pk-*
// names as fallback. Classes stay declared exactly once, in Go.

import (
	"encoding/json"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/tw"
)

// runtimeClasses are the compiled class lists _admin.js applies to elements it
// creates at runtime. Declared with tw so the served utility layer always
// carries their rules; TestServedStylesheetCoversRuntimeClasses pins that.
var runtimeClasses = struct {
	StatusPill     tw.ClassList
	StatusPositive tw.ClassList
	StatusWarning  tw.ClassList
	StatusDanger   tw.ClassList
	StatusNeutral  tw.ClassList
	Tag            tw.ClassList
	TagList        tw.ClassList
	RowActions     tw.ClassList
	TableAction    tw.ClassList
	PrimaryCell    tw.ClassList
	InlineError    tw.ClassList
	NoActions      tw.ClassList
}{
	StatusPill: tw.New().Display(tw.DisplayInlineFlex).Items(tw.ItemsCenter).
		Rounded(tw.RadiusFull).PaddingX(tw.S2_5).PaddingY(tw.S0_5).
		FontSize(tw.TextXS).FontWeight(tw.FontMedium),
	StatusPositive: tw.New().Bg(tw.SurfaceSuccessSoft).TextColor(tw.FgSuccess),
	StatusWarning:  tw.New().Bg(tw.SurfaceWarningSoft).TextColor(tw.FgWarning),
	StatusDanger:   tw.New().Bg(tw.SurfaceDangerSoft).TextColor(tw.FgDanger),
	StatusNeutral:  tw.New().Bg(tw.SurfaceTertiary).TextColor(tw.FgSecondary),
	Tag: tw.New().Display(tw.DisplayInlineFlex).Items(tw.ItemsCenter).
		Rounded(tw.RadiusMD).Border(tw.Border1).BorderColor(tw.BorderPrimary).
		Bg(tw.SurfacePrimary).TextColor(tw.FgSecondary).
		PaddingX(tw.S2).PaddingY(tw.S0_5).FontSize(tw.TextXS),
	TagList:    tw.New().Display(tw.DisplayInlineFlex).Items(tw.ItemsCenter).Gap(tw.S1).FlexWrap(),
	RowActions: tw.New().Display(tw.DisplayFlex).Items(tw.ItemsCenter).Gap(tw.S2).Justify(tw.JustifyEnd),
	TableAction: tw.New().Display(tw.DisplayInlineFlex).Items(tw.ItemsCenter).
		Rounded(tw.RadiusMD).Border(tw.Border1).BorderColor(tw.BorderPrimary).
		Bg(tw.SurfacePrimary).TextColor(tw.FgSecondary).
		PaddingX(tw.S2_5).PaddingY(tw.S1).FontSize(tw.TextXS).FontWeight(tw.FontMedium).
		Cursor(tw.CursorPointer).Transition(tw.TransitionColors).
		On(tw.StateHover, func(c tw.ClassList) tw.ClassList { return c.Bg(tw.SurfaceHover).TextColor(tw.FgPrimary) }).
		On(tw.StateFocusVisible, func(c tw.ClassList) tw.ClassList {
			return c.Ring(tw.Ring2).RingColor(tw.RingFocus).RingOffset(tw.RingOffset1)
		}),
	PrimaryCell: tw.New().FontWeight(tw.FontSemibold).TextColor(tw.FgPrimary),
	InlineError: tw.New().TextColor(tw.FgDanger),
	NoActions:   tw.New().TextColor(tw.FgMuted).FontSize(tw.TextXS),
}

// classNamesJSON renders the bridge payload. Keys are the vocabulary
// _admin.js consumes; values are compiled class strings.
func classNamesJSON() string {
	payload := map[string]string{
		"statusPill":     runtimeClasses.StatusPill.Compile(),
		"statusPositive": runtimeClasses.StatusPositive.Compile(),
		"statusWarning":  runtimeClasses.StatusWarning.Compile(),
		"statusDanger":   runtimeClasses.StatusDanger.Compile(),
		"statusNeutral":  runtimeClasses.StatusNeutral.Compile(),
		"tag":            runtimeClasses.Tag.Compile(),
		"tagList":        runtimeClasses.TagList.Compile(),
		"rowActions":     runtimeClasses.RowActions.Compile(),
		"tableAction":    runtimeClasses.TableAction.Compile(),
		"primaryCell":    runtimeClasses.PrimaryCell.Compile(),
		"inlineStatusError": runtimeClasses.InlineError.Compile() +
			" " + "pk-inline-status-error", // keeps the aria/status hook greppable
		"noActions": runtimeClasses.NoActions.Compile(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		// The payload is a map of compiled constants; failure here is a
		// programmer error, and an empty object keeps the legacy fallback path.
		return "{}"
	}
	return string(raw)
}

// viewClassLists returns every tw ClassList the Go views and the runtime
// bridge compose, so the served stylesheet can carry their rules — including
// the hover:/focus-visible: variants Base() alone does not pre-generate.
func viewClassLists() []tw.ClassList {
	return []tw.ClassList{
		runtimeClasses.StatusPill, runtimeClasses.StatusPositive,
		runtimeClasses.StatusWarning, runtimeClasses.StatusDanger,
		runtimeClasses.StatusNeutral, runtimeClasses.Tag, runtimeClasses.TagList,
		runtimeClasses.RowActions, runtimeClasses.TableAction,
		runtimeClasses.PrimaryCell, runtimeClasses.InlineError,
		runtimeClasses.NoActions,
		listClasses.Btn, listClasses.BtnPrimary, listClasses.Search,
		listClasses.SearchBox, listClasses.Table, listClasses.Th,
		formClasses.Input, formClasses.Status, formClasses.Legend,
		formClasses.Grid,
	}
}

// layout renders the shared document. The chrome markup and class names are
// byte-compatible with the previous template so the shell's identity CSS and
// every existing test assertion keep holding; only the content area changes
// per page.
func layout(view shellView, content g.Node) g.Node {
	return h.Doctype(h.HTML(h.Lang("en"),
		h.Head(
			h.Meta(h.Charset("utf-8")),
			h.Meta(h.Name("viewport"), h.Content("width=device-width,initial-scale=1,viewport-fit=cover")),
			h.Meta(h.Name("color-scheme"), h.Content("light")),
			h.TitleEl(g.Text(view.PageTitle+" · "+view.Title)),
			h.Link(h.Rel("stylesheet"), h.Href(view.BasePath+"/static/_admin.css")),
			h.Script(h.Type("application/json"), h.ID("pk-classnames"), g.Raw(classNamesJSON())),
			h.Script(g.Attr("defer"), h.Src(view.BasePath+"/static/_admin.js")),
		),
		h.Body(
			h.A(h.Class("pk-skip-link"), h.Href("#main-content"), g.Text("Skip to content")),
			h.Header(h.Class("pk-admin-header"),
				h.A(h.Class("pk-brand"), h.Href(view.BasePath), g.Attr("aria-label", view.Title+" overview"),
					h.Span(h.Class("pk-brand-mark"), g.Attr("aria-hidden", "true"), h.Span(), h.Span(), h.Span()),
					h.Span(
						h.Strong(g.Text(view.Title)),
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
							sidebarSections(view),
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
						sidebarSections(view),
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

// sidebarSections renders the registered navigation rail.
func sidebarSections(view shellView) g.Node {
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
		sections = append(sections, h.Section(h.Class("pk-nav-section"), g.Attr("aria-labelledby", "nav-"+section.ModuleID),
			h.H2(h.ID("nav-"+section.ModuleID), g.Text(section.Label)),
			h.Ul(items...),
		))
	}
	return g.Group(sections)
}
