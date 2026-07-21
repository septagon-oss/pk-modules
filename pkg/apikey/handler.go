package apikey

// handler.go owns the HTTP handlers that expose the canonical
// /api/v1/api-keys surface. The handler is mountable via RegisterRoutes
// against any http.ServeMux, so apps can plug it in without depending on
// a specific router.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/septagon-oss/pk-core/pkg/security/identity"
	"github.com/septagon-oss/pk-modules/pkg/apikey/store"
)

// tenantOf returns the tenant the request is authorized to act in, taken from
// the authenticated principal — never from client input. Anonymous or
// no-tenant callers get 401 and ok=false. Management operations (issue, list,
// revoke) are scoped to this tenant; API-key *authentication* is a separate
// path that resolves its own tenant from the presented key.
func tenantOf(w http.ResponseWriter, r *http.Request) (string, bool) {
	tid := strings.TrimSpace(identity.PrincipalFromContext(r.Context()).TenantID)
	if tid == "" {
		http.Error(w, "unauthorized: no tenant in request identity", http.StatusUnauthorized)
		return "", false
	}
	return tid, true
}

// Handler exposes the API key HTTP surface.
type Handler struct {
	svc APIKeyService
}

// NewHandler constructs a Handler wired to the given service.
func NewHandler(svc APIKeyService) *Handler { return &Handler{svc: svc} }

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
	case http.MethodGet:
		if id != "" {
			http.Error(w, "GET by id not supported", http.StatusMethodNotAllowed)
			return
		}
		h.list(w, r)
	case http.MethodPost:
		if id != "" {
			http.Error(w, "POST does not accept an ID in the path", http.StatusMethodNotAllowed)
			return
		}
		h.issue(w, r)
	case http.MethodDelete:
		if id == "" {
			http.Error(w, "DELETE requires an ID in the path", http.StatusBadRequest)
			return
		}
		h.revoke(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type issueRequest struct {
	TenantID  string   `json:"tenant_id"`
	UserID    string   `json:"user_id"`
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	TTLSecond int64    `json:"ttl_seconds"`
}

type issueResponse struct {
	Plaintext string  `json:"plaintext"`
	Key       *APIKey `json:"key"`
}

func (h *Handler) issue(w http.ResponseWriter, r *http.Request) {
	tenant, ok := tenantOf(w, r)
	if !ok {
		return
	}
	var req issueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	// The tenant is authoritative from the request identity; a body-supplied
	// tenant_id is ignored so a caller cannot mint keys for another tenant.
	ttl := time.Duration(req.TTLSecond) * time.Second
	plaintext, key, err := h.svc.Issue(r.Context(), tenant, req.UserID, req.Name, req.Scopes, ttl)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, issueResponse{Plaintext: plaintext, Key: key})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	tenant, ok := tenantOf(w, r)
	if !ok {
		return
	}
	keys, err := h.svc.List(r.Context(), tenant)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request, id string) {
	tenant, ok := tenantOf(w, r)
	if !ok {
		return
	}
	if err := h.svc.Revoke(r.Context(), tenant, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "api key not found", http.StatusNotFound)
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
