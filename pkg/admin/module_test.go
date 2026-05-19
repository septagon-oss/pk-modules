package admin_test

// module_test.go validates admin_management's contract: Registrar surface,
// HTTP handler routing for entity-CRUD/custom-page/static-asset paths,
// sidebar ordering, duplicate-registration handling, and composability.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

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
	m := newModule(t, admin.WithTitle("Acme"), admin.WithBasePath("/console"))
	if m.Title() != "Acme" {
		t.Fatalf("title = %q", m.Title())
	}
	if m.BasePath() != "/console" {
		t.Fatalf("basePath = %q", m.BasePath())
	}
}

func TestRegisterEntityCRUDStoresEntry(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	if err := m.Registrar().RegisterEntityCRUD("user_management", "User", "/api/v1/users"); err != nil {
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

func TestRegisterDuplicateEntityCRUDReturnsError(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	if err := m.Registrar().RegisterEntityCRUD("user_management", "User", "/api/v1/users"); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := m.Registrar().RegisterEntityCRUD("user_management", "User", "/api/v1/users"); err == nil {
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
	if err := m.Registrar().RegisterEntityCRUD("user_management", "User", "/api/v1/users"); err != nil {
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
	if err := m.Registrar().RegisterEntityCRUD("user_management", "User", "/api/v1/users"); err != nil {
		t.Fatalf("register: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/admin/user_management/User/new", nil)
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
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
