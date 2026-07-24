package auth

// Implements: REQ-AUTH-001.
// Per: ADR-0028.
// Discipline: C-14.
// handler.go owns the HTTP handlers that expose the canonical
// /api/v1/auth/sessions surface. The handler is mountable via
// RegisterRoutes against any http.ServeMux, so apps can plug it in without
// depending on a specific router.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/septagon-oss/pk-core/pkg/security/identity"
	"github.com/septagon-oss/pk-modules/pkg/auth/store"
	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

// Handler exposes the auth session HTTP surface.
type Handler struct {
	svc AuthService
}

// NewHandler constructs a Handler wired to the given service.
func NewHandler(svc AuthService) *Handler { return &Handler{svc: svc} }

// RegisterRoutes mounts the handler under the canonical APIPath on the
// given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle(APIPath, h)
	mux.Handle(APIPath+"/", h)
}

// ServeHTTP dispatches to the appropriate handler method.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, APIPath), "/")
	switch r.Method {
	case http.MethodPost:
		if id != "" {
			http.Error(w, "POST does not accept an ID in the path", http.StatusMethodNotAllowed)
			return
		}
		h.login(w, r)
	case http.MethodGet:
		if id == "" {
			http.Error(w, "GET requires a session ID in the path", http.StatusBadRequest)
			return
		}
		h.validate(w, r, id)
	case http.MethodDelete:
		if id == "" {
			http.Error(w, "DELETE requires a session ID in the path", http.StatusBadRequest)
			return
		}
		h.logout(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type loginRequest struct {
	TenantID string `json:"tenant_id"`
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := portslib.DecodeJSONBody(r.Body, &req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	sess, err := h.svc.Login(r.Context(), req.TenantID, Credentials{
		Email:    req.Email,
		Username: req.Username,
		Password: req.Password,
	})
	if err != nil {
		status := http.StatusInternalServerError
		msg := "internal error"
		switch {
		case errors.Is(err, ErrInvalidRequest), errors.Is(err, ErrNoCredentials):
			status = http.StatusBadRequest
			msg = err.Error()
		case errors.Is(err, ErrInvalidCredentials):
			status = http.StatusUnauthorized
			msg = err.Error()
		case errors.Is(err, ErrUserInactive):
			status = http.StatusForbidden
			msg = err.Error()
		case errors.Is(err, ErrPolicyDenied):
			status = http.StatusTooManyRequests
			msg = err.Error()
		}
		if status == http.StatusInternalServerError {
			slog.ErrorContext(r.Context(), "authentication login failed", "error", err)
		}
		http.Error(w, msg, status)
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

func (h *Handler) validate(w http.ResponseWriter, r *http.Request, id string) {
	// A session is a bearer secret; only its owner may read its metadata.
	// Anonymous callers and callers asking about a session that is not theirs
	// get an indistinguishable response, closing the unauthenticated session
	// oracle.
	p := identity.PrincipalFromContext(r.Context())
	if p.IsAnonymous() {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sess, err := h.svc.ValidateSession(r.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, store.ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, ErrSessionExpired), errors.Is(err, ErrSessionRevoked):
			status = http.StatusUnauthorized
		}
		http.Error(w, err.Error(), status)
		return
	}
	if sess.UserID != p.Subject {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request, id string) {
	// Only the session's owner may revoke it. The mutation gate already blocks
	// anonymous DELETEs; this is the ownership check that stops an authenticated
	// caller from force-revoking another user's session by ID. A session that is
	// already dead (not found/expired/revoked) is a harmless idempotent no-op,
	// so we only enforce ownership for a live session.
	p := identity.PrincipalFromContext(r.Context())
	if p.IsAnonymous() {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if sess, verr := h.svc.ValidateSession(r.Context(), id); verr == nil && sess.UserID != p.Subject {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	if err := h.svc.Logout(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
