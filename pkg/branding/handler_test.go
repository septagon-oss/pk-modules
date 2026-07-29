// Validates: REQ-BRANDING-001.
// Per: ADR-0017.
// Discipline: C-14.

package branding_test

// handler_test.go validates the branding_management HTTP surface:
// GET/POST /api/v1/branding (JSON profile read, multipart setup-form write)
// and GET /api/v1/branding/logo (the servable logo blob). It covers the
// server-owns-identity boundary (RequestActor's 401), the save/skip form
// flows and their redirect-based success/error signaling, the same-origin
// guard on POST, logo upload validation delegated to the service, and the
// logo route's security headers and 404 behavior. Tests live in
// branding_test to exercise the package the way callers see it, reusing
// newTestService and pngBytes from service_test.go in the same external test
// package.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"strings"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/security/identity"
	"github.com/septagon-oss/pk-modules/pkg/branding"
)

// profileWire mirrors the JSON shape the handler emits, decoded independently
// of the handler's own (unexported) wire type so the test proves the actual
// bytes on the wire, not an internal struct's field names.
type profileWire struct {
	TenantID      string `json:"tenant_id"`
	DisplayName   string `json:"display_name"`
	LogoURL       string `json:"logo_url"`
	LogoAlt       string `json:"logo_alt"`
	PrimaryColor  string `json:"primary_color"`
	FontKey       string `json:"font_key"`
	SetupComplete bool   `json:"setup_complete"`
}

// newTestMux wires a fresh Handler onto a bare mux, the way a host
// application mounts the module: via RegisterRoutes against its own
// http.ServeMux, never against a handler-owned ServeHTTP.
func newTestMux(t *testing.T, svc *branding.Service, adminBasePath string) *http.ServeMux {
	t.Helper()
	h := branding.NewHandler(svc, adminBasePath)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux
}

// withPrincipal injects an authenticated session principal into the request
// context exactly the way tenant's handler tests do (see
// tenant/module_test.go's withPrincipal), so RequestActor sees a real actor.
// A blank tenantID leaves the request anonymous.
func withPrincipal(r *http.Request, tenantID string) *http.Request {
	if tenantID == "" {
		return r
	}
	return r.WithContext(identity.ContextWithPrincipal(r.Context(),
		identity.Principal{Subject: "u1", TenantID: tenantID, AuthMethod: "session"}))
}

// filePart describes an optional multipart file part to attach under field
// name "logo".
type filePart struct {
	filename    string
	contentType string
	data        []byte
}

// newMultipartRequest builds a multipart/form-data POST request with the
// given plain fields and an optional file part, setting the file part's
// declared Content-Type explicitly (multipart.Writer.CreateFormFile always
// forces application/octet-stream, so tests that need a specific declared
// type — the whole point of the save-logo validation path — build the part
// header by hand via CreatePart).
func newMultipartRequest(t *testing.T, target string, fields map[string]string, fp *filePart) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("WriteField(%s): %v", k, err)
		}
	}
	if fp != nil {
		hdr := make(textproto.MIMEHeader)
		hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="logo"; filename=%q`, fp.filename))
		hdr.Set("Content-Type", fp.contentType)
		part, err := w.CreatePart(hdr)
		if err != nil {
			t.Fatalf("CreatePart: %v", err)
		}
		if _, err := part.Write(fp.data); err != nil {
			t.Fatalf("write file part: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, target, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

// decodeProfile decodes rec's body as profileWire, failing the test on any
// JSON error.
func decodeProfile(t *testing.T, rec *httptest.ResponseRecorder) profileWire {
	t.Helper()
	var got profileWire
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode profile JSON: %v; body=%s", err, rec.Body.String())
	}
	return got
}

func TestHandlerGetProfileUnsetReturnsZeroJSON(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	mux := newTestMux(t, svc, "/admin")

	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/branding", nil), "tenant-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	got := decodeProfile(t, rec)
	if got != (profileWire{}) {
		t.Fatalf("profile = %+v, want zero value (no branding record yet)", got)
	}
	// The JSON must never carry raw logo bytes, only the servable LogoURL.
	if strings.Contains(strings.ToLower(rec.Body.String()), "logodata") {
		t.Fatalf("response leaked a logo-bytes field: %s", rec.Body.String())
	}
}

func TestHandlerGetProfileUnauthenticatedReturns401(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	mux := newTestMux(t, svc, "/admin")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/branding", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerPostSaveSuccessRedirectsAndPersists(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	mux := newTestMux(t, svc, "/admin")

	fields := map[string]string{
		"action":        "save",
		"display_name":  "Acme Corp",
		"color_mode":    "custom",
		"primary_color": "#14b8a6",
		"font_key":      "editorial",
	}
	// logo_alt is deliberately omitted here: it is logo metadata, so Save
	// only applies it alongside an actual logo upload (see
	// TestHandlerPostSaveWithValidLogoPersistsAndServesLogoURL) — sending it
	// without a file part must not persist anything. color_mode=custom is
	// what tells handleSave to trust the submitted primary_color at all (see
	// TestHandlerPostSaveColorModeDefaultIgnoresSubmittedColor for the
	// opposite case).
	req := withPrincipal(newMultipartRequest(t, "/api/v1/branding", fields, nil), "tenant-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/branding?saved=1" {
		t.Fatalf("Location = %q, want /admin/branding?saved=1", loc)
	}

	profile, err := svc.ResolveBranding(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("ResolveBranding: %v", err)
	}
	if profile.DisplayName != "Acme Corp" || profile.PrimaryColor != "#14b8a6" || profile.FontKey != "editorial" {
		t.Fatalf("persisted profile = %+v, want the submitted fields", profile)
	}
	if !profile.SetupComplete {
		t.Fatalf("SetupComplete = false after save, want true")
	}
}

// TestHandlerPostSaveColorModeDefaultIgnoresSubmittedColor is the "no silent
// black" regression test: an HTML <input type="color"> can never submit
// "empty" — even one nobody touched still posts its browser default,
// #000000 — so an untouched first-login form (color_mode defaults to
// "default" on the rendered page) must never let that default value get
// mistaken for a deliberate color choice. Only color_mode=custom trusts the
// submitted primary_color at all; every other value, including this
// explicit "default" and a literal #000000, clears the palette.
func TestHandlerPostSaveColorModeDefaultIgnoresSubmittedColor(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	mux := newTestMux(t, svc, "/admin")

	fields := map[string]string{
		"action":        "save",
		"display_name":  "Acme Corp",
		"color_mode":    "default",
		"primary_color": "#000000",
	}
	req := withPrincipal(newMultipartRequest(t, "/api/v1/branding", fields, nil), "tenant-colormode-default")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}

	profile, err := svc.ResolveBranding(context.Background(), "tenant-colormode-default")
	if err != nil {
		t.Fatalf("ResolveBranding: %v", err)
	}
	if profile.PrimaryColor != "" {
		t.Fatalf("PrimaryColor = %q, want empty (color_mode=default must not persist #000000)", profile.PrimaryColor)
	}
	if profile.DisplayName != "Acme Corp" {
		t.Fatalf("DisplayName = %q, want %q", profile.DisplayName, "Acme Corp")
	}
}

// TestHandlerPostSaveColorModeMissingDefaultsToNoColor proves a hand-crafted
// (or otherwise malformed) request that omits color_mode entirely fails safe
// the same way "default" does, rather than falling back to trusting
// primary_color.
func TestHandlerPostSaveColorModeMissingDefaultsToNoColor(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	mux := newTestMux(t, svc, "/admin")

	fields := map[string]string{
		"action":        "save",
		"display_name":  "Acme Corp",
		"primary_color": "#14b8a6",
	}
	req := withPrincipal(newMultipartRequest(t, "/api/v1/branding", fields, nil), "tenant-colormode-missing")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}

	profile, err := svc.ResolveBranding(context.Background(), "tenant-colormode-missing")
	if err != nil {
		t.Fatalf("ResolveBranding: %v", err)
	}
	if profile.PrimaryColor != "" {
		t.Fatalf("PrimaryColor = %q, want empty (missing color_mode must not persist a color)", profile.PrimaryColor)
	}
}

func TestHandlerPostSaveValidationFailureRedirectsWithError(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	mux := newTestMux(t, svc, "/admin")

	fields := map[string]string{
		"action":       "save",
		"display_name": "", // required — Save rejects a blank name.
	}
	req := withPrincipal(newMultipartRequest(t, "/api/v1/branding", fields, nil), "tenant-2")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}
	wantErrMsg := "branding: display name is required"
	wantLoc := "/admin/branding?error=" + url.QueryEscape(wantErrMsg)
	if loc := rec.Header().Get("Location"); loc != wantLoc {
		t.Fatalf("Location = %q, want %q", loc, wantLoc)
	}

	profile, err := svc.ResolveBranding(context.Background(), "tenant-2")
	if err != nil {
		t.Fatalf("ResolveBranding: %v", err)
	}
	if profile.SetupComplete {
		t.Fatalf("profile persisted despite validation failure: %+v", profile)
	}
}

func TestHandlerPostSkipUsesSubmittedDisplayName(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	mux := newTestMux(t, svc, "/admin")

	fields := map[string]string{"action": "skip", "display_name": "Acme Skip"}
	req := withPrincipal(newMultipartRequest(t, "/api/v1/branding", fields, nil), "tenant-3")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/branding?saved=1" {
		t.Fatalf("Location = %q, want /admin/branding?saved=1", loc)
	}

	profile, err := svc.ResolveBranding(context.Background(), "tenant-3")
	if err != nil {
		t.Fatalf("ResolveBranding: %v", err)
	}
	if !profile.SetupComplete {
		t.Fatalf("SetupComplete = false after skip, want true")
	}
	if profile.DisplayName != "Acme Skip" {
		t.Fatalf("DisplayName = %q, want %q", profile.DisplayName, "Acme Skip")
	}
}

func TestHandlerPostSkipBlankDisplayNameFallsBackToWorkspace(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	mux := newTestMux(t, svc, "/admin")

	fields := map[string]string{"action": "skip", "display_name": "   "}
	req := withPrincipal(newMultipartRequest(t, "/api/v1/branding", fields, nil), "tenant-4")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}

	profile, err := svc.ResolveBranding(context.Background(), "tenant-4")
	if err != nil {
		t.Fatalf("ResolveBranding: %v", err)
	}
	if profile.DisplayName != "Workspace" {
		t.Fatalf("DisplayName = %q, want fallback %q", profile.DisplayName, "Workspace")
	}
}

func TestHandlerPostSameOriginGuard(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		origin     string
		wantStatus int
	}{
		{"cross-origin rejected", "http://evil.example", http.StatusForbidden},
		{"same-origin allowed", "http://example.com", http.StatusSeeOther},
		{"absent origin allowed", "", http.StatusSeeOther},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc, _ := newTestService(t)
			mux := newTestMux(t, svc, "/admin")

			fields := map[string]string{"action": "skip", "display_name": "Acme"}
			req := withPrincipal(newMultipartRequest(t, "/api/v1/branding", fields, nil), "tenant-origin")
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestHandlerPostSaveWithValidLogoPersistsAndServesLogoURL(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	mux := newTestMux(t, svc, "/admin")

	fields := map[string]string{"action": "save", "display_name": "Acme Corp", "logo_alt": "Acme logo"}
	fp := &filePart{filename: "logo.png", contentType: "image/png", data: pngBytes}
	req := withPrincipal(newMultipartRequest(t, "/api/v1/branding", fields, fp), "tenant-logo")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/branding?saved=1" {
		t.Fatalf("Location = %q, want /admin/branding?saved=1", loc)
	}

	getReq := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/branding", nil), "tenant-logo")
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)
	got := decodeProfile(t, getRec)
	if got.LogoURL == "" {
		t.Fatalf("logo_url is empty after a successful logo upload")
	}
	if got.LogoAlt != "Acme logo" {
		t.Fatalf("logo_alt = %q, want %q", got.LogoAlt, "Acme logo")
	}
}

func TestHandlerPostSaveWithOversizedLogoRejected(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	mux := newTestMux(t, svc, "/admin")

	fields := map[string]string{"action": "save", "display_name": "Acme Corp"}
	fp := &filePart{filename: "logo.png", contentType: "image/png", data: oversizedLogoBytes}
	req := withPrincipal(newMultipartRequest(t, "/api/v1/branding", fields, fp), "tenant-oversize")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/admin/branding?error=") {
		t.Fatalf("Location = %q, want an error redirect", loc)
	}

	profile, err := svc.ResolveBranding(context.Background(), "tenant-oversize")
	if err != nil {
		t.Fatalf("ResolveBranding: %v", err)
	}
	if profile.SetupComplete {
		t.Fatalf("oversized logo upload should not have persisted anything: %+v", profile)
	}
}

func TestHandlerPostSaveWithMistypedLogoRejected(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	mux := newTestMux(t, svc, "/admin")

	fields := map[string]string{"action": "save", "display_name": "Acme Corp"}
	// Declared image/png but the bytes are plain text — content sniffing
	// must catch the mismatch, the same as Service.Save's own validation
	// table (TestSaveLogoValidation in service_test.go).
	fp := &filePart{filename: "logo.png", contentType: "image/png", data: []byte("not actually a png, just some plain text padding")}
	req := withPrincipal(newMultipartRequest(t, "/api/v1/branding", fields, fp), "tenant-mistyped")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/admin/branding?error=") {
		t.Fatalf("Location = %q, want an error redirect", loc)
	}

	profile, err := svc.ResolveBranding(context.Background(), "tenant-mistyped")
	if err != nil {
		t.Fatalf("ResolveBranding: %v", err)
	}
	if profile.SetupComplete {
		t.Fatalf("mistyped logo upload should not have persisted anything: %+v", profile)
	}
}

func TestHandlerPostUnknownActionReturns400(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	mux := newTestMux(t, svc, "/admin")

	fields := map[string]string{"action": "delete-everything"}
	req := withPrincipal(newMultipartRequest(t, "/api/v1/branding", fields, nil), "tenant-badaction")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerGetLogoServesStoredBytesWithSecurityHeaders(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	mux := newTestMux(t, svc, "/admin")

	if err := svc.Save(context.Background(), "tenant-logo-get", branding.SaveParams{
		DisplayName:     "Acme Corp",
		LogoData:        pngBytes,
		LogoContentType: "image/png",
	}); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/branding/logo", nil), "tenant-logo-get")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.Bytes(); !bytes.Equal(got, pngBytes) {
		t.Fatalf("logo body = %x, want %x", got, pngBytes)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", ct)
	}
	wantHeaders := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "default-src 'none'",
		"Referrer-Policy":         "no-referrer",
		"Cache-Control":           "private, max-age=300",
	}
	for k, want := range wantHeaders {
		if got := rec.Header().Get(k); got != want {
			t.Fatalf("header %s = %q, want %q", k, got, want)
		}
	}
}

func TestHandlerHeadLogoServesHeadersWithZeroBody(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	mux := newTestMux(t, svc, "/admin")

	if err := svc.Save(context.Background(), "tenant-logo-head", branding.SaveParams{
		DisplayName:     "Acme Corp",
		LogoData:        pngBytes,
		LogoContentType: "image/png",
	}); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	req := withPrincipal(httptest.NewRequest(http.MethodHead, "/api/v1/branding/logo", nil), "tenant-logo-head")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	// HEAD must carry every header a GET would, but no body: the handler only
	// writes bytes on http.MethodGet, and this is the one branch that path
	// leaves untested.
	if got := rec.Body.Len(); got != 0 {
		t.Fatalf("HEAD body length = %d, want 0", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", ct)
	}
	wantHeaders := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "default-src 'none'",
		"Referrer-Policy":         "no-referrer",
		"Cache-Control":           "private, max-age=300",
	}
	for k, want := range wantHeaders {
		if got := rec.Header().Get(k); got != want {
			t.Fatalf("header %s = %q, want %q", k, got, want)
		}
	}
}

func TestHandlerGetLogoNoLogoReturns404(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	mux := newTestMux(t, svc, "/admin")

	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/branding/logo", nil), "tenant-no-logo")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerGetLogoUnauthenticatedReturns401(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	mux := newTestMux(t, svc, "/admin")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/branding/logo", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerMethodNotAllowedOnProfile(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	mux := newTestMux(t, svc, "/admin")

	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			req := withPrincipal(httptest.NewRequest(method, "/api/v1/branding", nil), "tenant-method")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want 405; body=%s", rec.Code, rec.Body.String())
			}
			if rec.Header().Get("Allow") == "" {
				t.Fatalf("Allow header missing on 405 response")
			}
		})
	}
}

func TestHandlerMethodNotAllowedOnLogo(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	mux := newTestMux(t, svc, "/admin")

	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/v1/branding/logo", nil), "tenant-method")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405; body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Allow") == "" {
		t.Fatalf("Allow header missing on 405 response")
	}
}
