// Validates: REQ-ADMIN-001.
// Per: ADR-0017.
// Discipline: C-14.

package admin_test

// shell_branding_test.go validates the shell's consumption of the tenant
// branding port: default chrome stays intact without a resolver, the resolver
// is consulted exactly once per authenticated page request, incomplete setup
// gates every surface except the branding page and static assets, themed
// chrome swaps in the tenant identity, and /static/_branding.css serves the
// resolver's theme CSS. Resolver failures degrade to the default chrome.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/security/identity"
	"github.com/septagon-oss/pk-modules/pkg/admin"
	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

// stubBranding is a counting portslib.BrandingResolver test double.
type stubBranding struct {
	mu           sync.Mutex
	profile      portslib.BrandingProfile
	profileErr   error
	css          string
	cssErr       error
	resolveCalls int
	cssCalls     int
}

func (s *stubBranding) ResolveBranding(_ context.Context, _ string) (portslib.BrandingProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolveCalls++
	return s.profile, s.profileErr
}

func (s *stubBranding) BrandingCSS(_ context.Context, _ string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cssCalls++
	return s.css, s.cssErr
}

func (s *stubBranding) counts() (resolve, css int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resolveCalls, s.cssCalls
}

// withSessionPrincipal injects an authenticated session principal the same
// way branding's handler tests do (see branding/handler_test.go).
func withSessionPrincipal(r *http.Request, tenantID string) *http.Request {
	return r.WithContext(identity.ContextWithPrincipal(r.Context(),
		identity.Principal{Subject: "u1", TenantID: tenantID, AuthMethod: "session"}))
}

func get(t *testing.T, h http.Handler, target string, tenantID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if tenantID != "" {
		req = withSessionPrincipal(req, tenantID)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestShellWithoutBrandingKeepsDefaultChrome(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	rec := get(t, m.HTTPHandler(), "/admin", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<link rel="icon" href="/admin/static/favicon.svg">`) {
		t.Errorf("default chrome missing the default favicon link:\n%s", body)
	}
	if strings.Contains(body, "_branding.css") {
		t.Error("default chrome must not link the tenant stylesheet")
	}
	if !strings.Contains(body, `class="pk-brand-mark"`) {
		t.Error("default chrome missing the 3-span brand mark")
	}
}

func TestBrandingResolverConsultedOncePerAuthenticatedRequest(t *testing.T) {
	t.Parallel()
	stub := &stubBranding{profile: portslib.BrandingProfile{TenantID: "t1", SetupComplete: true}}
	m := newModule(t, admin.WithBranding(stub))

	if rec := get(t, m.HTTPHandler(), "/admin", ""); rec.Code != http.StatusOK {
		t.Fatalf("anonymous home status = %d, want 200", rec.Code)
	}
	if resolve, _ := stub.counts(); resolve != 0 {
		t.Fatalf("anonymous request consulted the resolver %d times, want 0", resolve)
	}

	if rec := get(t, m.HTTPHandler(), "/admin", "t1"); rec.Code != http.StatusOK {
		t.Fatalf("session home status = %d, want 200", rec.Code)
	}
	if resolve, _ := stub.counts(); resolve != 1 {
		t.Fatalf("session request consulted the resolver %d times, want exactly 1", resolve)
	}
}

func TestIncompleteSetupGatesToBrandingPage(t *testing.T) {
	t.Parallel()
	stub := &stubBranding{profile: portslib.BrandingProfile{TenantID: "t1", SetupComplete: false}}
	m := newModule(t, admin.WithBranding(stub))
	if err := m.Registrar().RegisterResource(testResource()); err != nil {
		t.Fatalf("register resource: %v", err)
	}
	if err := m.Registrar().RegisterPage(portslib.AdminPage{
		ModuleID: "branding_management",
		Path:     "/admin/branding",
		Title:    "Branding",
		Render: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("branding setup"))
		},
	}); err != nil {
		t.Fatalf("register page: %v", err)
	}

	if rec := get(t, m.HTTPHandler(), "/admin/", "t1"); rec.Code != http.StatusSeeOther {
		t.Fatalf("home status = %d, want 303", rec.Code)
	} else if loc := rec.Header().Get("Location"); loc != "/admin/branding" {
		t.Fatalf("home redirect Location = %q, want /admin/branding", loc)
	}

	if rec := get(t, m.HTTPHandler(), "/admin/branding", "t1"); rec.Code != http.StatusOK {
		t.Fatalf("branding page status = %d, want 200 (exempt from the gate)", rec.Code)
	}

	resolveBefore, cssBefore := stub.counts()
	if rec := get(t, m.HTTPHandler(), "/admin/static/_admin.css", "t1"); rec.Code != http.StatusOK {
		t.Fatalf("static css status = %d, want 200 (static exempt)", rec.Code)
	}
	if resolve, css := stub.counts(); resolve != resolveBefore || css != cssBefore {
		t.Fatalf("static request consulted the resolver (resolve %d->%d, css %d->%d), want untouched",
			resolveBefore, resolve, cssBefore, css)
	}

	if rec := get(t, m.HTTPHandler(), "/admin/user_management/User", "t1"); rec.Code != http.StatusSeeOther {
		t.Fatalf("entity route status = %d, want 303", rec.Code)
	}
}

// TestAbsentBrandingRecordGatesToBrandingPage pins the integration contract
// with the real branding service (Task 4): when no record exists it returns a
// FULLY zero profile — TenantID "" included (store.ErrNotFound → zero value,
// pinned by branding/service_test.go). That absent-record case is exactly the
// first-login state the gate exists for, so the gate must key on the session
// principal's tenant, never on profile.TenantID.
func TestAbsentBrandingRecordGatesToBrandingPage(t *testing.T) {
	t.Parallel()
	stub := &stubBranding{} // zero profile, nil error: the service's "no record" answer
	m := newModule(t, admin.WithBranding(stub))
	rec := get(t, m.HTTPHandler(), "/admin/", "t1")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (absent record must gate to first-login setup)", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/branding" {
		t.Fatalf("Location = %q, want /admin/branding", loc)
	}
}

func TestThemedChromeRendersTenantIdentity(t *testing.T) {
	t.Parallel()
	stub := &stubBranding{
		profile: portslib.BrandingProfile{
			TenantID:      "t1",
			DisplayName:   "Acme Ops",
			LogoURL:       "/api/v1/branding/logo?v=1",
			LogoAlt:       "Acme",
			SetupComplete: true,
		},
		css: ":root{--pk-color-signal:#123456}",
	}
	m := newModule(t, admin.WithBranding(stub))
	rec := get(t, m.HTTPHandler(), "/admin", "t1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	titleStart := strings.Index(body, "<title>")
	titleEnd := strings.Index(body, "</title>")
	if titleStart == -1 || titleEnd == -1 || !strings.Contains(body[titleStart:titleEnd], "Acme Ops") {
		t.Errorf("<title> does not carry the tenant display name:\n%s", body)
	}

	brandAt := strings.Index(body, `class="pk-brand"`)
	if brandAt == -1 {
		t.Fatalf("brand link not rendered:\n%s", body)
	}
	brandEnd := strings.Index(body[brandAt:], "</a>")
	if brandEnd == -1 {
		t.Fatalf("brand link not closed:\n%s", body)
	}
	brand := body[brandAt : brandAt+brandEnd]
	if !strings.Contains(brand, `<img class="pk-brand-logo" src="/api/v1/branding/logo?v=1"`) {
		t.Errorf("brand link missing the tenant logo image:\n%s", brand)
	}
	if strings.Contains(body, "pk-brand-mark") {
		t.Error("themed chrome must replace the 3-span mark, not render both")
	}

	if !strings.Contains(body, `<link rel="icon" href="/api/v1/branding/logo?v=1">`) {
		t.Errorf("favicon must point at the tenant logo:\n%s", body)
	}

	adminCSSAt := strings.Index(body, `href="/admin/static/_admin.css"`)
	brandingCSSAt := strings.Index(body, `<link rel="stylesheet" href="/admin/static/_branding.css">`)
	if adminCSSAt == -1 || brandingCSSAt == -1 || brandingCSSAt < adminCSSAt {
		t.Errorf("tenant stylesheet link must follow _admin.css (admin at %d, branding at %d):\n%s",
			adminCSSAt, brandingCSSAt, body)
	}
}

func TestBrandingStylesheetRoute(t *testing.T) {
	t.Parallel()
	stub := &stubBranding{
		profile: portslib.BrandingProfile{TenantID: "t1", SetupComplete: true},
		css:     ":root{--pk-color-signal:#123456}",
	}
	m := newModule(t, admin.WithBranding(stub))

	rec := get(t, m.HTTPHandler(), "/admin/static/_branding.css", "t1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/css; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/css; charset=utf-8", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, max-age=60" {
		t.Errorf("Cache-Control = %q, want private, max-age=60", got)
	}
	if got := rec.Body.String(); got != stub.css {
		t.Errorf("body = %q, want the resolver's CSS %q", got, stub.css)
	}
}

func TestBrandingStylesheetRouteEmptyCSS(t *testing.T) {
	t.Parallel()
	stub := &stubBranding{profile: portslib.BrandingProfile{TenantID: "t1", SetupComplete: true}}
	m := newModule(t, admin.WithBranding(stub))
	rec := get(t, m.HTTPHandler(), "/admin/static/_branding.css", "t1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "" {
		t.Errorf("body = %q, want empty stylesheet", got)
	}
}

func TestBrandingStylesheetRouteWithoutResolver(t *testing.T) {
	t.Parallel()
	// Without a resolver the path falls through to the embedded static file
	// server, which has no _branding.css — a plain 404, never a broken page.
	m := newModule(t)
	rec := get(t, m.HTTPHandler(), "/admin/static/_branding.css", "t1")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (no resolver, no embedded file)", rec.Code)
	}
}

func TestBrandingResolverErrorDegradesToDefaultChrome(t *testing.T) {
	t.Parallel()
	stub := &stubBranding{profileErr: errors.New("store offline")}
	m := newModule(t, admin.WithBranding(stub))
	rec := get(t, m.HTTPHandler(), "/admin", "t1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (resolver failure must degrade, not fail)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `class="pk-brand-mark"`) {
		t.Error("degraded chrome missing the default brand mark")
	}
	if !strings.Contains(body, `<link rel="icon" href="/admin/static/favicon.svg">`) {
		t.Error("degraded chrome missing the default favicon")
	}
	if strings.Contains(body, "_branding.css") {
		t.Error("degraded chrome must not link the tenant stylesheet")
	}
}

func TestThemedChromeEmptyDisplayNameFallsBackToTitle(t *testing.T) {
	t.Parallel()
	stub := &stubBranding{profile: portslib.BrandingProfile{TenantID: "t1", SetupComplete: true}}
	m := newModule(t, admin.WithBranding(stub))
	rec := get(t, m.HTTPHandler(), "/admin", "t1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	titleStart := strings.Index(body, "<title>")
	titleEnd := strings.Index(body, "</title>")
	if titleStart == -1 || titleEnd == -1 || !strings.Contains(body[titleStart:titleEnd], "PlatformKit Admin") {
		t.Errorf("<title> must fall back to the configured shell title:\n%s", body)
	}
}
