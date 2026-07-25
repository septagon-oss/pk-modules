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
