// Implements: REQ-004.
// Per: ADR-0017, ADR-0022, ADR-0029.
// Discipline: C-14.

package admin

// view_home.go renders the overview page. The hero, the stat strip, and the
// module index are the console's editorial voice — deliberate chrome per the
// line drawn in view_chrome.go — so their markup and classes carry over from
// the retired template byte for byte, and _admin.css keeps owning their art
// direction. What changed is the substrate: typed Go instead of text/template,
// so a broken field is a compile error instead of a blank page.

import (
	"fmt"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

func homeView(data homeData) g.Node {
	return layout(data.shellView, g.Group([]g.Node{
		h.Section(h.Class("pk-hero"),
			h.Div(
				h.P(h.Class("pk-eyebrow"), g.Text("Composed application")),
				h.H1(g.Text("Know what is running."), h.Br(), h.Em(g.Text("Operate what matters."))),
				h.P(h.Class("pk-lede"), g.Text("A focused view of the resources and actions exposed by this PlatformKit instance.")),
			),
			h.Div(h.Class("pk-runtime-stamp"), g.Attr("aria-label", "Runtime status operational"),
				h.Span(h.Class("pk-status-light"), g.Attr("aria-hidden", "true")),
				h.Div(h.Strong(g.Text("Operational")), h.Small(g.Text("Runtime responding"))),
			),
		),
		h.Dl(h.Class("pk-stats"), g.Attr("aria-label", "Admin surface summary"),
			stat("Managed areas", data.Stats.Areas),
			stat("Collections", data.Stats.Collections),
			stat("Available actions", data.Stats.Actions),
		),
		h.Section(h.Class("pk-section"),
			h.Div(h.Class("pk-section-head"),
				h.Div(
					h.P(h.Class("pk-eyebrow"), g.Text("Management index")),
					h.H2(g.Text("Application areas")),
				),
				h.Span(h.Class("pk-section-note"), g.Text("Generated from registered module contracts")),
			),
			h.Div(h.Class("pk-module-index"), moduleIndex(data)),
		),
	}))
}

func stat(label string, value int) g.Node {
	return h.Div(h.Class("pk-stat"),
		h.Dt(g.Text(label)),
		h.Dd(g.Text(fmt.Sprintf("%d", value))),
	)
}

func moduleIndex(data homeData) g.Node {
	if len(data.Modules) == 0 {
		return h.Div(h.Class("pk-empty-state"),
			h.Span(h.Class("pk-empty-glyph"), g.Attr("aria-hidden", "true"), g.Text("＋")),
			h.H3(g.Text("No management areas yet")),
			h.P(g.Text("Register a schema-aware resource from a module to make it available here.")),
		)
	}
	rows := make([]g.Node, 0, len(data.Modules))
	for index, module := range data.Modules {
		links := make([]g.Node, 0, len(module.Resources)+len(module.Pages))
		for _, resource := range module.Resources {
			links = append(links, h.A(h.Href(data.BasePath+"/"+resource.ModuleID+"/"+resource.EntityName),
				h.Span(g.Text(resource.PluralLabel)),
				h.Span(g.Attr("aria-hidden", "true"), g.Text("↗")),
			))
		}
		for _, page := range module.Pages {
			links = append(links, h.A(h.Href(page.Path),
				h.Span(g.Text(page.Title)),
				h.Span(g.Attr("aria-hidden", "true"), g.Text("↗")),
			))
		}
		rows = append(rows, h.Article(h.Class("pk-module-row"),
			h.Span(h.Class("pk-module-number"), g.Attr("aria-hidden", "true"), g.Text(fmt.Sprintf("%02d", index+1))),
			h.Div(h.Class("pk-module-copy"),
				h.H3(g.Text(module.DisplayName)),
				g.If(module.Description != "", h.P(g.Text(module.Description))),
			),
			h.Div(append([]g.Node{h.Class("pk-module-links")}, links...)...),
		))
	}
	return g.Group(rows)
}
