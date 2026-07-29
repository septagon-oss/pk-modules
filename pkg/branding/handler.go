package branding

// Implements: REQ-BRANDING-001.
// Per: ADR-0017.
// Discipline: C-14.
// handler.go owns the branding_management HTTP surface: GET/POST
// /api/v1/branding (the JSON profile read and the multipart setup-form
// write) and GET /api/v1/branding/logo (the servable logo blob). Both routes
// are mountable via RegisterRoutes against any http.ServeMux, so host apps
// can plug the module in without depending on a specific router — the same
// pattern tenant and auth's handler.go use.
//
// The POST form flow is redirect-based (303 back to the admin branding
// page), matching the starter app's login-form precedent
// (pk-apps/pkg/starterapp/admin_auth.go): success carries ?saved=1, failure
// carries ?error=<url-escaped message>, so the admin page (Task 6) can
// render either state without a session-scoped flash store.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/septagon-oss/pk-modules/pkg/branding/store"
	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

// apiPath is the canonical HTTP base path for the branding profile resource.
// The logo route is served one segment beneath it, at logoRoutePath
// (service.go) — this handler mounts both.
const apiPath = "/api/v1/branding"

// Handler exposes the branding HTTP surface: the JSON profile resource and
// its setup form's multipart POST, plus the servable logo blob.
type Handler struct {
	svc           *Service
	adminBasePath string
}

// NewHandler constructs a Handler wired to the given service. adminBasePath
// is the admin shell's mount point (see WithAdminBasePath); a redirect after
// a form POST lands on "<adminBasePath>/branding".
func NewHandler(svc *Service, adminBasePath string) *Handler {
	return &Handler{svc: svc, adminBasePath: adminBasePath}
}

// RegisterRoutes mounts /api/v1/branding and /api/v1/branding/logo on the
// given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc(apiPath, h.serveProfile)
	mux.HandleFunc(logoRoutePath, h.serveLogo)
}

// brandingProfileResponse is the JSON wire shape for GET /api/v1/branding.
// portslib.BrandingProfile carries no json tags — ports stay serialization-
// agnostic — so this local, unexported struct owns the snake_case field
// names the other business modules' JSON surfaces use. It never carries logo
// bytes: LogoURL is always a servable route, never raw data.
type brandingProfileResponse struct {
	TenantID      string `json:"tenant_id"`
	DisplayName   string `json:"display_name"`
	LogoURL       string `json:"logo_url"`
	LogoAlt       string `json:"logo_alt"`
	PrimaryColor  string `json:"primary_color"`
	FontKey       string `json:"font_key"`
	SetupComplete bool   `json:"setup_complete"`
}

// toResponse adapts the port's BrandingProfile to the local wire shape.
func toResponse(p portslib.BrandingProfile) brandingProfileResponse {
	return brandingProfileResponse{
		TenantID:      p.TenantID,
		DisplayName:   p.DisplayName,
		LogoURL:       p.LogoURL,
		LogoAlt:       p.LogoAlt,
		PrimaryColor:  p.PrimaryColor,
		FontKey:       p.FontKey,
		SetupComplete: p.SetupComplete,
	}
}

// serveProfile dispatches GET (read the resolved profile as JSON) and POST
// (the setup form: save or skip) on /api/v1/branding. Every request must
// carry an authenticated principal — RequestActor writes 401 itself and
// this returns immediately when one is missing.
func (h *Handler) serveProfile(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := portslib.RequestActor(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.getProfile(w, r, tenantID)
	case http.MethodPost:
		h.postProfile(w, r, tenantID)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) getProfile(w http.ResponseWriter, r *http.Request, tenantID string) {
	profile, err := h.svc.ResolveBranding(r.Context(), tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, toResponse(profile))
}

// sameOrigin rejects state-changing cross-origin form posts. Browsers always
// send Origin on cross-origin POSTs; absent Origin (curl, same-origin GET
// navigations) is allowed — the session cookie is SameSite anyway; this guard
// is defense in depth for the form flow.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}

// postProfile handles the multipart setup form. action=save persists the
// submitted display name, palette, font, and optional logo; action=skip
// records that the tenant explicitly skipped setup, using the submitted (or
// fallback) display name. Both redirect back to the admin branding page on
// completion — 303 with ?saved=1 on success, ?error=<message> on failure —
// mirroring the starter app's login-form flow. A ParseMultipartForm failure
// takes that same redirect path: it can arise from an ordinary submission —
// a client that dropped the connection mid-upload, or a truncated/malformed
// multipart body from a flaky network — which is a form outcome the admin
// page should render exactly like any other save error. An unrecognized
// action cannot arise from the rendered form at all — only from a
// hand-crafted request — so it is treated as a malformed request, not a form
// outcome, and gets a plain 400 rather than a redirect.
func (h *Handler) postProfile(w http.ResponseWriter, r *http.Request, tenantID string) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin request rejected", http.StatusForbidden)
		return
	}
	// maxMemory (2 MiB) is deliberately 2×maxLogoBytes: an at-the-limit,
	// 1-MiB logo part is read fully into memory with headroom to spare, so
	// ParseMultipartForm never spills it to a temp file on disk just because
	// the small text fields and multipart framing around it pushed the total
	// past a tighter bound.
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		h.redirectError(w, r, err)
		return
	}
	switch r.FormValue("action") {
	case "save":
		h.handleSave(w, r, tenantID)
	case "skip":
		h.handleSkip(w, r, tenantID)
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
	}
}

func (h *Handler) handleSave(w http.ResponseWriter, r *http.Request, tenantID string) {
	params := SaveParams{
		DisplayName:  r.FormValue("display_name"),
		PrimaryColor: r.FormValue("primary_color"),
		FontKey:      r.FormValue("font_key"),
		LogoAlt:      r.FormValue("logo_alt"),
	}
	file, header, err := r.FormFile("logo")
	switch {
	case err == nil:
		defer file.Close()
		// The +1 read bounds how much we buffer in memory; it does not
		// itself reject an oversized upload — Save's own size check does
		// that, with its normal error message, once the data reaches it.
		data, rerr := io.ReadAll(io.LimitReader(file, maxLogoBytes+1))
		if rerr != nil {
			h.redirectError(w, r, rerr)
			return
		}
		params.LogoData = data
		params.LogoContentType = header.Header.Get("Content-Type")
	case errors.Is(err, http.ErrMissingFile):
		// No logo part supplied: Save preserves whatever logo (if any) is
		// already on record for this tenant.
	default:
		h.redirectError(w, r, err)
		return
	}
	if err := h.svc.Save(r.Context(), tenantID, params); err != nil {
		h.redirectError(w, r, err)
		return
	}
	h.redirectSaved(w, r)
}

func (h *Handler) handleSkip(w http.ResponseWriter, r *http.Request, tenantID string) {
	fallback := strings.TrimSpace(r.FormValue("display_name"))
	if fallback == "" {
		fallback = "Workspace"
	}
	if err := h.svc.Skip(r.Context(), tenantID, fallback); err != nil {
		h.redirectError(w, r, err)
		return
	}
	h.redirectSaved(w, r)
}

func (h *Handler) redirectSaved(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, h.adminBasePath+"/branding?saved=1", http.StatusSeeOther)
}

func (h *Handler) redirectError(w http.ResponseWriter, r *http.Request, err error) {
	http.Redirect(w, r, h.adminBasePath+"/branding?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
}

// serveLogo serves the tenant's raw logo bytes. The four security/cache
// headers are set before any bytes are written and are identical on hit —
// nosniff, a deny-everything CSP, no-referrer, and a short private cache —
// since the response is untrusted tenant-supplied content served back to a
// browser.
func (h *Handler) serveLogo(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := portslib.RequestActor(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data, contentType, err := h.svc.Logo(r.Context(), tenantID)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "logo not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		// justified: headers/status are already sent, so a client-disconnect write error is non-actionable here.
		_, _ = w.Write(data)
	}
}

// writeJSON encodes v as the response body, mirroring the writeJSON helper
// every other business module's handler.go defines locally (tenant, auth,
// user, ...).
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// justified: the status code is already sent, so a client-disconnect encode error is non-actionable here.
	_ = json.NewEncoder(w).Encode(v)
}
