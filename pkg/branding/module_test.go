package branding_test

// Validates: REQ-BRANDING-001.
// Per: ADR-0017.
// Discipline: C-14.
// module_test.go validates the branding_management module against its public
// API. Tests live in branding_test to ensure the OSS contract is exercised
// the way callers see it.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	pkmodule "github.com/septagon-oss/pk-core/pkg/module"
	corehealth "github.com/septagon-oss/pk-core/pkg/observability/health"

	"github.com/septagon-oss/pk-modules/pkg/branding"
	"github.com/septagon-oss/pk-modules/pkg/branding/store"
	"github.com/septagon-oss/pk-modules/pkg/portslib"

	_ "modernc.org/sqlite"
)

// moduleSQLiteDSN returns an isolated on-disk sqlite DSN so each test gets a
// fresh schema without depending on shared global memory caches.
func moduleSQLiteDSN(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return "file:" + filepath.Join(dir, "branding.db") + "?_pragma=journal_mode(WAL)"
}

func newModule(t *testing.T, opts ...branding.Option) *branding.Module {
	t.Helper()
	allOpts := append([]branding.Option{branding.WithSQLiteDSN(moduleSQLiteDSN(t))}, opts...)
	m, err := branding.NewModule(allOpts...)
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}
	return m
}

func TestNewModuleWithStoreSucceeds(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	if m.Service() == nil {
		t.Fatalf("Service() is nil")
	}
	if m.Store() == nil {
		t.Fatalf("Store() is nil")
	}
	if m.HTTPHandler() == nil {
		t.Fatalf("HTTPHandler() is nil")
	}
}

func TestNewModuleRequiresStore(t *testing.T) {
	t.Parallel()
	if _, err := branding.NewModule(); err == nil {
		t.Fatalf("NewModule() with no store should return an error")
	}
}

func TestComposeReturnsValidComposable(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	c := m.Compose()
	if c == nil {
		t.Fatalf("Compose() returned nil")
	}
	if c.Metadata().ID != branding.ModuleID {
		t.Fatalf("metadata ID = %q, want %q", c.Metadata().ID, branding.ModuleID)
	}
	if len(c.Provides()) != 1 {
		t.Fatalf("provides len = %d, want 1 (BrandingResolver)", len(c.Provides()))
	}
	deps := c.Dependencies()
	if len(deps) != 2 {
		t.Fatalf("dependencies len = %d, want 2 (admin + health)", len(deps))
	}
	for _, dep := range deps {
		if dep.Required {
			t.Fatalf("dep %s should be optional", dep.Port.Name)
		}
	}

	// Confirm the catalog validates this Composable end-to-end.
	catalog := pkmodule.NewCatalog().
		Add(pkmodule.NewBundle("branding-bundle", []pkmodule.Entry{
			{ID: branding.ModuleID, New: func() pkmodule.Composable { return c }},
		}, []string{branding.ModuleID})).
		MustBuild()
	if _, err := pkmodule.Compose(catalog); err != nil {
		t.Fatalf("Compose catalog: %v", err)
	}
}

// fakeHealthRegistrar is a minimal portslib.HealthRegistrar that captures the
// registered name and checker so the test can invoke it directly.
type fakeHealthRegistrar struct {
	name    string
	checker corehealth.Checker
}

func (f *fakeHealthRegistrar) Register(name string, checker corehealth.Checker) error {
	f.name = name
	f.checker = checker
	return nil
}

// erroringStore is a minimal store.Store whose Get always fails with a fixed
// error, used to drive the health checker's unhealthy branch — a real
// failure (unlike store.ErrNotFound) must fail the check.
type erroringStore struct {
	getErr error
}

func (e *erroringStore) Get(ctx context.Context, tenantID string) (*store.Record, error) {
	return nil, e.getErr
}

func (e *erroringStore) Upsert(ctx context.Context, r *store.Record) error {
	return nil
}

func TestNewModuleRegistersHealthCheck(t *testing.T) {
	t.Parallel()
	reg := &fakeHealthRegistrar{}
	newModule(t, branding.WithHealthRegistrar(reg))

	if reg.name != "branding_management.store" {
		t.Fatalf("registered health check name = %q, want %q", reg.name, "branding_management.store")
	}
	if reg.checker == nil {
		t.Fatalf("registered checker is nil")
	}
	if err := reg.checker.Check(context.Background()); err != nil {
		t.Fatalf("checker.Check on empty store = %v, want nil", err)
	}
}

// TestNewModuleHealthCheckFailsOnStoreError proves registerHealth only
// treats store.ErrNotFound as healthy: a real store error (connection
// failure and the like) must fail the check, not be swallowed alongside it.
func TestNewModuleHealthCheckFailsOnStoreError(t *testing.T) {
	t.Parallel()
	reg := &fakeHealthRegistrar{}
	wantErr := errors.New("branding/sqlite: connection refused")
	m, err := branding.NewModule(
		branding.WithStore(&erroringStore{getErr: wantErr}),
		branding.WithHealthRegistrar(reg),
	)
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}
	if m.Store() == nil {
		t.Fatalf("Store() is nil")
	}
	if reg.checker == nil {
		t.Fatalf("registered checker is nil")
	}
	if err := reg.checker.Check(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("checker.Check = %v, want %v", err, wantErr)
	}
}

func TestNewModuleWithoutHealthRegistrarSkipsRegistration(t *testing.T) {
	t.Parallel()
	// No WithHealthRegistrar: NewModule must not fail when the dependency is
	// simply absent (it's optional).
	m := newModule(t)
	if m == nil {
		t.Fatalf("NewModule returned nil module")
	}
}

// fakeAdminRegistrar is a minimal portslib.AdminRegistrar that captures every
// registered resource, page, and sidebar section, mirroring the admin shell
// without depending on pkg/admin (ports-only: this test proves what the
// branding module registers, not how the shell would render it).
type fakeAdminRegistrar struct {
	resources []portslib.AdminResource
	pages     []portslib.AdminPage
	sections  []portslib.SidebarSection
}

func (f *fakeAdminRegistrar) RegisterResource(r portslib.AdminResource) error {
	f.resources = append(f.resources, r)
	return nil
}

func (f *fakeAdminRegistrar) RegisterPage(p portslib.AdminPage) error {
	f.pages = append(f.pages, p)
	return nil
}

func (f *fakeAdminRegistrar) RegisterSidebarSection(s portslib.SidebarSection) error {
	f.sections = append(f.sections, s)
	return nil
}

func TestNewModuleWithoutAdminRegistrarSkipsPageRegistration(t *testing.T) {
	t.Parallel()
	// No WithAdminRegistrar: NewModule must not fail when the dependency is
	// simply absent (it's optional), mirroring the health-registrar case.
	m := newModule(t)
	if m == nil {
		t.Fatalf("NewModule returned nil module")
	}
}

func TestNewModuleRegistersAdminPageAndSidebar(t *testing.T) {
	t.Parallel()
	reg := &fakeAdminRegistrar{}
	newModule(t, branding.WithAdminRegistrar(reg), branding.WithAdminBasePath("/admin"))

	if len(reg.pages) != 1 {
		t.Fatalf("registered pages = %d, want 1: %+v", len(reg.pages), reg.pages)
	}
	page := reg.pages[0]
	if page.ModuleID != branding.ModuleID {
		t.Fatalf("page.ModuleID = %q, want %q", page.ModuleID, branding.ModuleID)
	}
	if page.Path != "/admin/branding" {
		t.Fatalf("page.Path = %q, want %q", page.Path, "/admin/branding")
	}
	if page.Title != "Branding" {
		t.Fatalf("page.Title = %q, want %q", page.Title, "Branding")
	}
	if page.Render == nil {
		t.Fatalf("page.Render is nil")
	}

	if len(reg.sections) != 1 {
		t.Fatalf("registered sidebar sections = %d, want 1: %+v", len(reg.sections), reg.sections)
	}
	section := reg.sections[0]
	if section.Label != "Workspace" {
		t.Fatalf("section.Label = %q, want %q", section.Label, "Workspace")
	}
	if len(section.Items) != 1 {
		t.Fatalf("section.Items len = %d, want 1: %+v", len(section.Items), section.Items)
	}
	if section.Items[0].Path != "/admin/branding" || section.Items[0].Label != "Branding" {
		t.Fatalf("section.Items[0] = %+v, want {Path: /admin/branding, Label: Branding}", section.Items[0])
	}
}

func TestAdminPageRenderSetupCopyForNewTenant(t *testing.T) {
	t.Parallel()
	reg := &fakeAdminRegistrar{}
	newModule(t, branding.WithAdminRegistrar(reg), branding.WithAdminBasePath("/admin"))
	render := reg.pages[0].Render

	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/admin/branding", nil), "tenant-setup")
	rec := httptest.NewRecorder()
	render(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Set up your workspace",
		`name="display_name"`,
		`action="/api/v1/branding"`,
		`enctype="multipart/form-data"`,
		`name="action" value="skip"`,
		"Skip for now",
		`<link rel="stylesheet" href="/admin/static/_admin.css">`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q; body=%s", want, body)
		}
	}
}

func TestAdminPageRenderSettingsCopyForCompletedTenant(t *testing.T) {
	t.Parallel()
	reg := &fakeAdminRegistrar{}
	m := newModule(t, branding.WithAdminRegistrar(reg), branding.WithAdminBasePath("/admin"))

	if err := m.Service().Save(context.Background(), "tenant-done", branding.SaveParams{
		DisplayName:     "Acme Inc",
		PrimaryColor:    "#14b8a6",
		FontKey:         "plex",
		LogoData:        pngBytes,
		LogoContentType: "image/png",
	}); err != nil {
		t.Fatalf("seed Save: %v", err)
	}
	profile, err := m.Service().ResolveBranding(context.Background(), "tenant-done")
	if err != nil {
		t.Fatalf("ResolveBranding: %v", err)
	}

	render := reg.pages[0].Render
	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/admin/branding", nil), "tenant-done")
	rec := httptest.NewRecorder()
	render(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<h1>Branding</h1>") {
		t.Fatalf("body missing settings heading; body=%s", body)
	}
	if strings.Contains(body, "Set up your workspace") {
		t.Fatalf("body still shows first-login setup copy for a completed tenant; body=%s", body)
	}
	if !strings.Contains(body, `value="Acme Inc"`) {
		t.Fatalf("body missing prefilled display name; body=%s", body)
	}
	if !strings.Contains(body, `value="plex" selected`) {
		t.Fatalf("body missing selected plex font option; body=%s", body)
	}
	if !strings.Contains(body, "<img") {
		t.Fatalf("body missing logo preview <img>; body=%s", body)
	}
	if profile.LogoURL == "" || !strings.Contains(body, profile.LogoURL) {
		t.Fatalf("body missing logo URL %q; body=%s", profile.LogoURL, body)
	}
	if strings.Contains(body, "Skip for now") {
		t.Fatalf("body still shows Skip for now for a completed tenant; body=%s", body)
	}
}

// TestAdminPageRenderEscapesErrorQueryParam is the security regression this
// page carries forward from Task 5's handler review: ?error= is
// attacker-controlled (it round-trips whatever handler.go's redirectError
// URL-escapes into the query string), so the page must never let it through
// as live markup.
func TestAdminPageRenderEscapesErrorQueryParam(t *testing.T) {
	t.Parallel()
	reg := &fakeAdminRegistrar{}
	newModule(t, branding.WithAdminRegistrar(reg), branding.WithAdminBasePath("/admin"))
	render := reg.pages[0].Render

	payload := `<img src=x onerror=alert(1)>`
	req := withPrincipal(httptest.NewRequest(http.MethodGet,
		"/admin/branding?error="+url.QueryEscape(payload), nil), "tenant-xss")
	rec := httptest.NewRecorder()
	render(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "&lt;img") {
		t.Fatalf("error text was not HTML-escaped; body=%s", body)
	}
	if strings.Contains(body, "<img src=x") {
		t.Fatalf("raw attack payload leaked unescaped into the page; body=%s", body)
	}
}

func TestAdminPageRenderSavedQueryShowsConfirmation(t *testing.T) {
	t.Parallel()
	reg := &fakeAdminRegistrar{}
	newModule(t, branding.WithAdminRegistrar(reg), branding.WithAdminBasePath("/admin"))
	render := reg.pages[0].Render

	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/admin/branding?saved=1", nil), "tenant-saved")
	rec := httptest.NewRecorder()
	render(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Saved") {
		t.Fatalf("body missing saved confirmation; body=%s", rec.Body.String())
	}
}

func TestAdminPageRenderUnauthenticatedReturns401(t *testing.T) {
	t.Parallel()
	reg := &fakeAdminRegistrar{}
	newModule(t, branding.WithAdminRegistrar(reg), branding.WithAdminBasePath("/admin"))
	render := reg.pages[0].Render

	req := httptest.NewRequest(http.MethodGet, "/admin/branding", nil)
	rec := httptest.NewRecorder()
	render(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}
