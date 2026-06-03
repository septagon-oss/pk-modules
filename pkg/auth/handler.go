package auth

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
	"net/http"
	"strings"

	"github.com/septagon-oss/pk-modules/pkg/auth/store"
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
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
		http.Error(w, msg, status)
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

func (h *Handler) validate(w http.ResponseWriter, r *http.Request, id string) {
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
	writeJSON(w, http.StatusOK, sess)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request, id string) {
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
