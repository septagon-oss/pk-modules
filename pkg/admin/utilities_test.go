// Validates: REQ-004.
// Per: ADR-0022.
// Discipline: C-14.

package admin_test

// utilities_test.go pins the served stylesheet's composition: theme tokens,
// role variables, utility rules, and the bespoke shell rules must all arrive
// in the one response the layout links. A module page rendering pk-ui
// components depends on every layer being there.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServedStylesheetCarriesAllFourLayers(t *testing.T) {
	t.Parallel()

	m := newModule(t)
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/static/_admin.css", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET _admin.css = %d", rec.Code)
	}
	css := rec.Body.String()

	for _, want := range []string{
		"--pk-color-accent-default:",        // layer 1: theme tokens
		"--pk-role-surface-brand:",          // layer 2: role variables
		".inline-flex{display:inline-flex}", // layer 3: utility rules (minified)
		"@keyframes pk-spin",                // layer 3: animation keyframes
		".pk-brand",                         // layer 4: bespoke shell rules
	} {
		if !strings.Contains(css, want) {
			t.Errorf("served stylesheet missing %q", want)
		}
	}

	// The utility layer must reference tokens through role indirection, so a
	// theme overlay can re-map every role without touching a single rule.
	if !strings.Contains(css, "var(--pk-role-") {
		t.Error("utility rules do not reference role variables")
	}
}

// TestLayoutEmbedsTheClassNameBridge pins the contract that lets _admin.js
// style runtime-built elements with the same compiled class lists the Go
// views use: the layout must embed the pk-classnames JSON, and the served
// stylesheet must carry the interactive variants those lists compose.
func TestLayoutEmbedsTheClassNameBridge(t *testing.T) {
	t.Parallel()

	m := newModule(t)
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="pk-classnames"`) {
		t.Error("layout does not embed the pk-classnames bridge")
	}
	for _, key := range []string{
		"statusPositive", "statusWarning", "statusDanger", "statusNeutral",
		"tag", "tagList", "row", "td", "tdPrimary", "cellNote",
		"rowActions", "tableAction", "dangerAction",
		"statusTextIdle", "statusTextError",
	} {
		if !strings.Contains(body, `"`+key+`"`) {
			t.Errorf("pk-classnames bridge missing %q", key)
		}
	}

	css := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(css, httptest.NewRequest(http.MethodGet, "/admin/static/_admin.css", nil))
	sheet := css.Body.String()
	for _, want := range []string{
		`.hover\:bg-surface-hover:hover`,       // tableAction hover
		`.focus-visible\:ring-2:focus-visible`, // focus ring variants
		`.hover\:bg-surface-brand-hover:hover`, // primary button hover
	} {
		if !strings.Contains(sheet, want) {
			t.Errorf("served stylesheet missing interactive variant %q", want)
		}
	}
}
