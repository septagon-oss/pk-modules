// Validates: REQ-ADMIN-001.
// Per: ADR-0017.
// Discipline: C-14.

package admin_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pkmodule "github.com/septagon-oss/pk-core/pkg/module"
	"github.com/septagon-oss/pk-modules/pkg/admin"
	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

// Compile-time: Shell satisfies portslib.AdminRegistrar.
var _ portslib.AdminRegistrar = (*admin.Shell)(nil)

func newModule(t *testing.T, opts ...admin.Option) *admin.Module {
	t.Helper()
	m, err := admin.NewModule(opts...)
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}
	return m
}

func testResource() portslib.AdminResource {
	return portslib.AdminResource{
		ModuleID:      "user_management",
		EntityName:    "User",
		SingularLabel: "user",
		PluralLabel:   "Users",
		APIPath:       "/api/v1/users",
		CanCreate:     true,
		CanEdit:       true,
		Columns:       []portslib.AdminColumn{{Key: "email", Label: "Email", Primary: true}},
		Fields:        []portslib.AdminField{{Key: "email", Label: "Email", Kind: portslib.AdminFieldEmail, Required: true}},
	}
}

func TestNewModuleDefaults(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	if m.Title() != "PlatformKit Admin" {
		t.Fatalf("title = %q, want %q", m.Title(), "PlatformKit Admin")
	}
	if m.BasePath() != "/admin" {
		t.Fatalf("basePath = %q, want %q", m.BasePath(), "/admin")
	}
}

func TestWithTitleAndBasePath(t *testing.T) {
	t.Parallel()
	m := newModule(t, admin.WithTitle("Workspace"), admin.WithBasePath("/console"))
	if m.Title() != "Workspace" {
		t.Fatalf("title = %q", m.Title())
	}
	if m.BasePath() != "/console" {
		t.Fatalf("basePath = %q", m.BasePath())
	}
}

func TestRegisterResourceStoresEntry(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	if err := m.Registrar().RegisterResource(testResource()); err != nil {
		t.Fatalf("register: %v", err)
	}
}

func TestRegisterPageStoresEntry(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	p := portslib.AdminPage{
		ModuleID: "audit_management",
		Path:     "/admin/audit/log",
		Title:    "Audit Log",
		Render:   func(w http.ResponseWriter, r *http.Request) {},
	}
	if err := m.Registrar().RegisterPage(p); err != nil {
		t.Fatalf("register: %v", err)
	}
}

func TestRegisterSidebarSection(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	s := portslib.SidebarSection{
		ModuleID: "user_management",
		Label:    "Users",
		Order:    1,
		Items:    []portslib.SidebarItem{{Path: "/user_management/User", Label: "Users"}},
	}
	if err := m.Registrar().RegisterSidebarSection(s); err != nil {
		t.Fatalf("register: %v", err)
	}
}

func TestRegisterDuplicateResourceReturnsError(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	if err := m.Registrar().RegisterResource(testResource()); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := m.Registrar().RegisterResource(testResource()); err == nil {
		t.Fatal("expected error on duplicate")
	}
}

func TestHTTPHandlerServesHome(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "PlatformKit Admin") {
		t.Fatalf("body missing title: %s", rec.Body.String())
	}
}

func TestHTTPHandlerServesEntityList(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	if err := m.Registrar().RegisterResource(testResource()); err != nil {
		t.Fatalf("register: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/admin/user_management/User", nil)
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "User") {
		t.Fatalf("body missing entity name: %s", rec.Body.String())
	}
}

func TestHTTPHandlerServesEntityFormNew(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	if err := m.Registrar().RegisterResource(testResource()); err != nil {
		t.Fatalf("register: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/admin/user_management/User/new", nil)
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `name="email"`) {
		t.Fatalf("form missing schema field: %s", body)
	}
	if strings.Contains(body, "payload (JSON)") || strings.Contains(body, `name="payload"`) {
		t.Fatal("form regressed to a raw JSON payload editor")
	}
}

func TestPasswordFieldUsesUTF8ByteLimit(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	resource := testResource()
	resource.Fields = append(resource.Fields, portslib.AdminField{
		Key: "password", Label: "Password", Kind: portslib.AdminFieldPassword, Max: 72,
	})
	if err := m.Registrar().RegisterResource(resource); err != nil {
		t.Fatalf("register: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/admin/user_management/User/new", nil)
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `name="password"`) ||
		!strings.Contains(body, `data-max-utf8-bytes="72"`) {
		t.Fatalf("password field is missing its UTF-8 byte limit: %s", body)
	}
	if strings.Contains(body, `maxlength="72"`) {
		t.Fatal("password byte limit was rendered as a character-count maxlength")
	}
}

func TestHTTPHandlerServesStaticCSS(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	req := httptest.NewRequest(http.MethodGet, "/admin/static/_admin.css", nil)
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "css") {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestHTTPHandlerServesAdminJavaScript(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	req := httptest.NewRequest(http.MethodGet, "/admin/static/_admin.js", nil)
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "javascript") {
		t.Fatalf("Content-Type = %q", got)
	}
	if !strings.Contains(rec.Body.String(), "pk-resource-form") {
		t.Fatal("admin behavior asset is missing the typed form controller")
	}
	if !strings.Contains(rec.Body.String(), "new TextEncoder()") ||
		!strings.Contains(rec.Body.String(), "setCustomValidity") {
		t.Fatal("admin behavior asset is missing UTF-8 byte-limit validation")
	}
	if !strings.Contains(rec.Body.String(), `cell.dataset.label = column.label`) ||
		!strings.Contains(rec.Body.String(), `actions.dataset.label = "Actions"`) {
		t.Fatal("admin behavior asset is missing responsive table labels")
	}
}

func TestAdminShellSetsBrowserSecurityHeaders(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "frame-ancestors 'none'") ||
		!strings.Contains(got, "script-src 'self'") {
		t.Fatalf("Content-Security-Policy = %q", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q, want no-referrer", got)
	}
}

func TestRegisterResourceRejectsRawUnknownShape(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	resource := testResource()
	resource.Columns = nil
	if err := m.Registrar().RegisterResource(resource); err == nil {
		t.Fatal("RegisterResource accepted a descriptor without readable columns")
	}
}

func TestRegisterResourceRejectsInvalidRowCondition(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	resource := testResource()
	resource.EditWhen = &portslib.AdminRowCondition{
		Field: "status", Operator: portslib.AdminConditionEquals,
	}
	if err := m.Registrar().RegisterResource(resource); err == nil {
		t.Fatal("RegisterResource accepted an equals condition without a value")
	}
}

func TestEntityListRendersDeclaredColumnsAndAccessibleControls(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	if err := m.Registrar().RegisterResource(testResource()); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/user_management/User", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`scope="col">Email`,
		`type="search"`,
		`role="status"`,
		`aria-label="Pagination"`,
		`data-resource-config=`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("entity list missing %q", want)
		}
	}
	if strings.Contains(body, "JSON.stringify(row)") {
		t.Fatal("template exposes raw row JSON instead of declared columns")
	}
}

func TestAdminCSSCarriesResponsiveAndAccessibilityGuards(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/static/_admin.css", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"@media (max-width: 960px)",
		"min-height: 44px",
		":focus-visible",
		"prefers-reduced-motion",
		"overflow-x: hidden",
		"grid-template-columns: minmax(92px, .34fr) minmax(0, 1fr)",
		".pk-row-actions .pk-table-action",
		`content: "Exit"`,
		"max-width: min(185px, calc(100vw - 200px))",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("responsive stylesheet missing %q", want)
		}
	}
}

func TestHTTPHandlerRejectsPost(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	req := httptest.NewRequest(http.MethodPost, "/admin", nil)
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestHTTPHandlerNotFound(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	req := httptest.NewRequest(http.MethodGet, "/admin/no/such/route/here/extra", nil)
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestSidebarSectionsSortedByOrder(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	if err := m.Registrar().RegisterSidebarSection(portslib.SidebarSection{ModuleID: "b", Label: "BetaSection", Order: 2}); err != nil {
		t.Fatalf("register b: %v", err)
	}
	if err := m.Registrar().RegisterSidebarSection(portslib.SidebarSection{ModuleID: "a", Label: "AlphaSection", Order: 1}); err != nil {
		t.Fatalf("register a: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, req)
	body := rec.Body.String()
	aIdx := strings.Index(body, "AlphaSection")
	bIdx := strings.Index(body, "BetaSection")
	if aIdx == -1 || bIdx == -1 {
		t.Fatalf("sidebar labels not rendered; alpha=%d beta=%d body=%s", aIdx, bIdx, body)
	}
	if aIdx > bIdx {
		t.Fatalf("sidebar order wrong; AlphaSection at %d, BetaSection at %d", aIdx, bIdx)
	}
}

func TestCustomPagePrecedesEntity(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	called := false
	page := portslib.AdminPage{
		ModuleID: "tenant_management",
		Path:     "/admin/tenants",
		Title:    "Tenants",
		Render: func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("tenant page"))
		},
	}
	if err := m.Registrar().RegisterPage(page); err != nil {
		t.Fatalf("register page: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/admin/tenants", nil)
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, req)
	if !called {
		t.Fatal("custom page render not invoked")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

// countingResponseWriter records every WriteHeader call so the test can
// assert that the Shell never double-commits the response (the symptom of
// the "superfluous response.WriteHeader call" warning the buffer-first
// render() fix addresses).
type countingResponseWriter struct {
	header        http.Header
	body          []byte
	statusWrites  []int
	wroteImplicit bool
}

func newCountingResponseWriter() *countingResponseWriter {
	return &countingResponseWriter{header: http.Header{}}
}

func (c *countingResponseWriter) Header() http.Header { return c.header }

func (c *countingResponseWriter) WriteHeader(code int) {
	c.statusWrites = append(c.statusWrites, code)
}

func (c *countingResponseWriter) Write(p []byte) (int, error) {
	if len(c.statusWrites) == 0 {
		// Mirror net/http.ResponseWriter semantics: a Write before
		// WriteHeader implicitly commits 200.
		c.statusWrites = append(c.statusWrites, http.StatusOK)
		c.wroteImplicit = true
	}
	c.body = append(c.body, p...)
	return len(p), nil
}

func TestRenderDoesNotDoubleWriteHeader(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	if err := m.Registrar().RegisterResource(testResource()); err != nil {
		t.Fatalf("register: %v", err)
	}
	cases := []struct {
		name string
		url  string
	}{
		{"home", "/admin"},
		{"entity list", "/admin/user_management/User"},
		{"entity form new", "/admin/user_management/User/new"},
		{"entity form edit", "/admin/user_management/User/some-id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			w := newCountingResponseWriter()
			m.HTTPHandler().ServeHTTP(w, req)
			if len(w.statusWrites) != 1 {
				t.Fatalf("WriteHeader call count = %d (writes=%v); want exactly 1 (superfluous WriteHeader regression)",
					len(w.statusWrites), w.statusWrites)
			}
			if w.wroteImplicit {
				t.Fatalf("render() relied on the implicit 200 from Write — must call WriteHeader explicitly so it is auditable")
			}
			if w.statusWrites[0] != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.statusWrites[0])
			}
			if !strings.Contains(string(w.body), "Admin") && !strings.Contains(string(w.body), "User") {
				t.Fatalf("body looks empty/wrong: %q", string(w.body))
			}
		})
	}
}

// TestSidebarLinkResolvesToRegisteredRoute is the regression guard for the
// "/admin/admin/tenants" papercut: the sidebar template (and the home
// dashboard's Pages list) used to prepend the base path to each link's
// already-absolute Path, producing a doubled prefix that 404s. This test
// registers a sidebar item alongside the custom page it points at, renders the
// home dashboard, extracts the hrefs the templates actually emitted for BOTH
// the sidebar item and the home Pages entry, and then GETs each exact href
// back through the shell — asserting they resolve (200) rather than 404. If a
// renderer ever re-doubles the prefix, the extracted href becomes
// "/admin/admin/tenants" and the follow-up GET 404s, failing this test.
func TestSidebarLinkResolvesToRegisteredRoute(t *testing.T) {
	t.Parallel()
	m := newModule(t)

	const itemPath = "/admin/tenants"
	if err := m.Registrar().RegisterPage(portslib.AdminPage{
		ModuleID: "tenant_management",
		Path:     itemPath,
		Title:    "Tenants Page",
		Render: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("tenant page"))
		},
	}); err != nil {
		t.Fatalf("register page: %v", err)
	}
	if err := m.Registrar().RegisterSidebarSection(portslib.SidebarSection{
		ModuleID: "tenant_management",
		Label:    "Tenants",
		Order:    10,
		Items:    []portslib.SidebarItem{{Path: itemPath, Label: "All tenants"}},
	}); err != nil {
		t.Fatalf("register sidebar: %v", err)
	}

	// Render the home dashboard and pull the hrefs the templates actually
	// emitted for the sidebar item and the home Pages entry.
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("home status = %d", rec.Code)
	}
	body := rec.Body.String()

	for _, tc := range []struct{ where, label string }{
		{"sidebar item", "All tenants"},
		{"home Pages entry", "Tenants Page"},
	} {
		href := extractHref(t, body, tc.label)
		if href != itemPath {
			t.Fatalf("%s href = %q, want %q (base path must not be doubled)", tc.where, href, itemPath)
		}
		// The emitted href must resolve to a real route, not a 404.
		linkReq := httptest.NewRequest(http.MethodGet, href, nil)
		linkRec := httptest.NewRecorder()
		m.HTTPHandler().ServeHTTP(linkRec, linkReq)
		if linkRec.Code != http.StatusOK {
			t.Fatalf("GET %s href %q: status = %d, want 200", tc.where, href, linkRec.Code)
		}
	}
}

// extractHref returns the href value of the first <a> tag whose link text
// contains label. It is a deliberately small parser scoped to the admin
// sidebar markup the shell renders.
func extractHref(t *testing.T, body, label string) string {
	t.Helper()
	anchor := strings.Index(body, ">"+label+"<")
	if anchor == -1 {
		t.Fatalf("link text %q not found in body:\n%s", label, body)
	}
	open := strings.LastIndex(body[:anchor], "<a ")
	if open == -1 {
		t.Fatalf("no <a> tag before link text %q", label)
	}
	tag := body[open:anchor]
	const marker = `href="`
	_, after, ok := strings.Cut(tag, marker)
	if !ok {
		t.Fatalf("no href in anchor for %q: %q", label, tag)
	}
	rest := after
	end := strings.IndexByte(rest, '"')
	if end == -1 {
		t.Fatalf("unterminated href in anchor for %q: %q", label, tag)
	}
	return rest[:end]
}

func TestCompose(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	composable := m.Compose()
	if composable.Metadata().ID != admin.ModuleID {
		t.Fatalf("metadata ID = %q, want %q", composable.Metadata().ID, admin.ModuleID)
	}
	if len(composable.Provides()) != 1 {
		t.Fatalf("provides len = %d, want 1", len(composable.Provides()))
	}
	catalog := pkmodule.NewCatalog().
		Add(pkmodule.NewBundle("admin-bundle", []pkmodule.Entry{
			{ID: admin.ModuleID, New: func() pkmodule.Composable { return m.Compose() }},
		}, []string{admin.ModuleID})).
		MustBuild()
	plan, err := pkmodule.Compose(catalog)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if len(plan.Modules) != 1 {
		t.Fatalf("module count = %d", len(plan.Modules))
	}
}
