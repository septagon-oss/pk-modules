// Implements: REQ-004.
// Per: ADR-0017, ADR-0022, ADR-0029, ADR-0031.
// Discipline: C-14.

package admin

// view_list.go renders the resource list page. The DOM here is a contract
// with _admin.js — every element the script binds keeps its exact id,
// data-* attribute, and structure — so the migration changes the styling
// substrate, not the behavior: component styling now comes from tw class
// lists declared below (the same layer pk-ui composes), replacing the
// .pk-btn/.pk-table family that _admin.css used to own. The page head keeps
// its editorial classes per the chrome line drawn in view_chrome.go.

import (
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/tw"
)

// listClasses are the component styles this page composes. Declared once,
// exported into the served stylesheet via viewClassLists, and mirrored by
// nothing: deleting one here deletes it everywhere.
var listClasses = struct {
	Btn        tw.ClassList
	BtnPrimary tw.ClassList
	Toolbar    tw.ClassList
	Search     tw.ClassList
	SearchBox  tw.ClassList
	Status     tw.ClassList
	TableShell tw.ClassList
	Table      tw.ClassList
	Th         tw.ClassList
	Pagination tw.ClassList
	PageLabel  tw.ClassList
}{
	Btn:        btnStructure.Merge(btnSecondaryColors),
	BtnPrimary: btnStructure.Merge(btnPrimaryColors),
	Toolbar: tw.New().Display(tw.DisplayFlex).Items(tw.ItemsCenter).Gap(tw.S3).
		FlexWrap().MarginY(tw.S4),
	Search: tw.New().Display(tw.DisplayInlineFlex).Items(tw.ItemsCenter).Gap(tw.S2).
		Rounded(tw.RadiusMD).Border(tw.Border1).BorderColor(tw.BorderPrimary).
		Bg(tw.SurfacePrimary).PaddingX(tw.S3).Flex1().MaxWScaled(tw.MaxWMD).
		TextColor(tw.FgMuted),
	SearchBox: tw.New().Width(tw.SFull).Border(tw.Border0).Bg(tw.ColorTransparent).
		TextColor(tw.FgPrimary).PaddingY(tw.S2).FontSize(tw.TextSM).
		On(tw.StatePlaceholder, func(c tw.ClassList) tw.ClassList { return c.TextColor(tw.FgPlaceholder) }),
	Status: tw.New().FontSize(tw.TextSM).TextColor(tw.FgMuted).MarginY(tw.S2),
	TableShell: tw.New().Width(tw.SFull).Overflow(tw.OverflowAuto).
		Rounded(tw.RadiusLG).Border(tw.Border1).BorderColor(tw.BorderPrimary).
		Bg(tw.SurfacePrimary),
	Table: tw.New().Width(tw.SFull).FontSize(tw.TextSM).TextColor(tw.FgPrimary),
	Th: tw.New().PaddingX(tw.S4).PaddingY(tw.S3).TextAlign(tw.TextLeft).
		FontSize(tw.TextXS).FontWeight(tw.FontSemibold).Uppercase().
		Tracking(tw.TrackingWider).TextColor(tw.FgMuted).
		BorderBottom(tw.Border1).BorderColor(tw.BorderPrimary).Bg(tw.SurfaceSecondary),
	Pagination: tw.New().Display(tw.DisplayFlex).Items(tw.ItemsCenter).Gap(tw.S3).
		MarginY(tw.S5),
	PageLabel: tw.New().FontSize(tw.TextSM).TextColor(tw.FgMuted),
}

// Button variants never overlap on a property: the structure fragment carries
// geometry and states, and each color set is complete. Merging two lists that
// both set background-color would leave the winner to stylesheet order — the
// classic utility-CSS collision — so variants are whole lists by construction.
var (
	btnStructure = tw.New().Display(tw.DisplayInlineFlex).Items(tw.ItemsCenter).Justify(tw.JustifyCenter).
			Gap(tw.S2).Rounded(tw.RadiusMD).Border(tw.Border1).
			PaddingX(tw.S4).PaddingY(tw.S2).FontSize(tw.TextSM).FontWeight(tw.FontSemibold).
			NoUnderline().Cursor(tw.CursorPointer).Transition(tw.TransitionColors).
			On(tw.StateFocusVisible, func(c tw.ClassList) tw.ClassList {
			return c.Ring(tw.Ring2).RingColor(tw.RingFocus).RingOffset(tw.RingOffset2)
		}).
		On(tw.StateDisabled, func(c tw.ClassList) tw.ClassList {
			return c.Cursor(tw.CursorNotAllowed).Opacity(tw.Opacity50)
		})
	btnSecondaryColors = tw.New().Bg(tw.SurfacePrimary).TextColor(tw.FgPrimary).BorderColor(tw.BorderPrimary).
				On(tw.StateHover, func(c tw.ClassList) tw.ClassList { return c.Bg(tw.SurfaceHover) })
	btnPrimaryColors = tw.New().Bg(tw.SurfaceBrand).TextColor(tw.FgOnBrand).BorderColor(tw.ColorTransparent).
				On(tw.StateHover, func(c tw.ClassList) tw.ClassList { return c.Bg(tw.SurfaceBrandHover) })
)

func entityListView(data entityListData) g.Node {
	resource := data.Resource
	listPath := data.BasePath + "/" + resource.ModuleID + "/" + resource.EntityName
	hasActions := resource.CanEdit || resource.CanDelete || len(resource.Actions) > 0

	headCells := make([]g.Node, 0, len(resource.Columns)+1)
	for _, column := range resource.Columns {
		headCells = append(headCells, h.Th(h.Class(listClasses.Th.Compile()), g.Attr("scope", "col"), g.Text(column.Label)))
	}
	if hasActions {
		headCells = append(headCells, h.Th(h.Class(listClasses.Th.Compile()), g.Attr("scope", "col"),
			h.Span(h.Class("pk-visually-hidden"), g.Text("Actions"))))
	}

	return layout(data.shellView, h.Section(
		h.Class("pk-resource-page"),
		g.Attr("data-pk-page", "resource-list"),
		g.Attr("data-resource-config", data.ResourceConfig),
		g.Attr("data-list-path", listPath),

		h.Header(h.Class("pk-page-head"),
			h.Div(
				h.A(h.Class("pk-backlink"), h.Href(data.BasePath), g.Text("← Overview")),
				h.P(h.Class("pk-eyebrow"), g.Text(resource.ModuleID)),
				h.H1(g.Text(resource.PluralLabel)),
				g.If(resource.Description != "", h.P(h.Class("pk-lede"), g.Text(resource.Description))),
			),
			g.If(resource.CanCreate,
				h.A(h.Class(listClasses.BtnPrimary.Compile()), h.Href(listPath+"/new"),
					g.Text("New "+resource.SingularLabel)),
			),
		),

		h.Div(h.Class(listClasses.Toolbar.Compile()), h.Role("search"),
			h.Label(h.Class(listClasses.Search.Compile()),
				h.Span(h.Class("pk-visually-hidden"), g.Text("Search "+resource.PluralLabel)),
				h.Span(g.Attr("aria-hidden", "true"), g.Text("⌕")),
				h.Input(h.Class(listClasses.SearchBox.Compile()), h.ID("pk-resource-search"),
					h.Type("search"), h.Placeholder("Filter this page…"), h.AutoComplete("off")),
			),
			h.Button(h.Class(listClasses.Btn.Compile()), h.ID("pk-resource-refresh"), h.Type("button"), g.Text("Refresh")),
		),

		h.Div(h.Class(listClasses.Status.Compile()+" pk-inline-status"), h.ID("pk-resource-status"),
			h.Role("status"), g.Attr("aria-live", "polite"), g.Text("Loading "+resource.PluralLabel+"…")),

		h.Div(h.Class(listClasses.TableShell.Compile()), h.TabIndex("0"), g.Attr("aria-label", resource.PluralLabel+" table"),
			h.Table(h.Class(listClasses.Table.Compile()), h.ID("pk-resource-table"),
				h.THead(h.Tr(headCells...)),
				h.TBody(),
			),
		),

		h.Div(h.Class("pk-empty-state pk-empty-state-compact"), h.ID("pk-resource-empty"), g.Attr("hidden"),
			h.Span(h.Class("pk-empty-glyph"), g.Attr("aria-hidden", "true"), g.Text("∅")),
			h.H2(g.Text("No "+resource.PluralLabel+" found")),
			h.P(h.ID("pk-resource-empty-copy"), g.Text("There is nothing to show on this page yet.")),
		),

		h.Footer(h.Class(listClasses.Pagination.Compile()), g.Attr("aria-label", "Pagination"),
			h.Button(h.Class(listClasses.Btn.Compile()), h.ID("pk-page-prev"), h.Type("button"), g.Text("← Previous")),
			h.Span(h.Class(listClasses.PageLabel.Compile()), h.ID("pk-page-label"), g.Text("Page 1")),
			h.Button(h.Class(listClasses.Btn.Compile()), h.ID("pk-page-next"), h.Type("button"), g.Text("Next →")),
		),
		h.NoScript(h.P(h.Class(runtimeClasses.InlineError.Compile()), g.Text("JavaScript is required for live resource management."))),
	))
}
