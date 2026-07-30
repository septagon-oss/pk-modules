package user

// Implements: REQ-USER-001.
// Per: ADR-0017.
// Discipline: C-14.
// handler.go owns the HTTP handlers that expose the canonical
// /api/v1/users surface. The handler is mountable via RegisterRoutes against
// any http.ServeMux, so apps can plug it in without depending on a specific
// router.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/septagon-oss/pk-core/pkg/security/identity"
	"github.com/septagon-oss/pk-modules/pkg/portslib"
	"github.com/septagon-oss/pk-modules/pkg/user/store"
)

// tenantOf returns the tenant the request is authorized to act in, taken from
// the authenticated principal — never from client input. Anonymous or
// no-tenant callers get 401 and ok=false; the caller must return without
// touching the store. This choke point keeps by-ID user operations (including
// password resets) from crossing the tenant boundary.
func tenantOf(w http.ResponseWriter, r *http.Request) (string, bool) {
	tid := strings.TrimSpace(identity.PrincipalFromContext(r.Context()).TenantID)
	if tid == "" {
		http.Error(w, "unauthorized: no tenant in request identity", http.StatusUnauthorized)
		return "", false
	}
	return tid, true
}

// Handler exposes the user CRUD HTTP surface.
type Handler struct {
	svc UserService
}

// NewHandler constructs a Handler wired to the given service.
func NewHandler(svc UserService) *Handler { return &Handler{svc: svc} }

type userRequest struct {
	Email       string `json:"email"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
	Active      *bool  `json:"active"`
}

func (input userRequest) user(tenantID, id string, active bool) User {
	return User{
		ID:          id,
		TenantID:    tenantID,
		Email:       input.Email,
		Username:    input.Username,
		DisplayName: input.DisplayName,
		Active:      active,
	}
}

func canSetPassword(r *http.Request) bool {
	principal := identity.PrincipalFromContext(r.Context())
	// Password mutation is an interactive-administrator capability, not
	// ordinary user CRUD. The API-key module refuses to mint the reserved
	// "admin" scope, so a users:write automation key cannot turn itself into
	// an interactive administrator by replacing a login password.
	return principal.HasScope("admin")
}

func canMutateUser(r *http.Request, id string) bool {
	principal := identity.PrincipalFromContext(r.Context())
	return principal.HasScope("admin") || principal.Subject != id
}

// RegisterRoutes mounts the handler under the canonical APIPath on the given
// mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle(APIPath, h)
	mux.Handle(APIPath+"/", h)
}

// ServeHTTP dispatches to the appropriate handler method.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, _, err := portslib.EntityIDFromPath(r.URL.Path, APIPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Reject IDs that still contain a slash — those are nested paths the
	// handler does not own. Without this guard a request like
	// /api/v1/users/a/b would resolve to id="a/b" and be served as a
	// per-row lookup that can never succeed.
	if strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if id == "" {
			h.list(w, r)
			return
		}
		h.get(w, r, id)
	case http.MethodPost:
		if id != "" {
			http.Error(w, "POST does not accept an ID in the path", http.StatusMethodNotAllowed)
			return
		}
		h.create(w, r)
	case http.MethodPut:
		if id == "" {
			http.Error(w, "PUT requires an ID in the path", http.StatusBadRequest)
			return
		}
		h.update(w, r, id)
	case http.MethodDelete:
		if id == "" {
			http.Error(w, "DELETE requires an ID in the path", http.StatusBadRequest)
			return
		}
		h.delete(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	tenant, ok := tenantOf(w, r)
	if !ok {
		return
	}
	limit, offset, err := portslib.ParsePagination(r.URL.Query())
	if err != nil {
		http.Error(w, "invalid pagination: "+err.Error(), http.StatusBadRequest)
		return
	}
	users, err := h.svc.List(r.Context(), tenant, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, id string) {
	tenant, ok := tenantOf(w, r)
	if !ok {
		return
	}
	u, err := h.svc.Get(r.Context(), tenant, id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	tenant, ok := tenantOf(w, r)
	if !ok {
		return
	}
	var input userRequest
	if err := portslib.DecodeJSONBody(r.Body, &input); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if input.Password != "" && !canSetPassword(r) {
		http.Error(w, "forbidden: admin scope required to set a password", http.StatusForbidden)
		return
	}
	if err := validatePassword(input.Password); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The tenant is authoritative from the request identity. tenant_id is not
	// part of the accepted request contract, so strict decoding rejects it.
	active := true
	if input.Active != nil {
		active = *input.Active
	}
	u := input.user(tenant, "", active)
	if err := h.svc.Create(r.Context(), &u); err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrValidation), errors.Is(err, ErrUnknownTenant):
			status = http.StatusBadRequest
		case errors.Is(err, store.ErrDuplicateEmail),
			errors.Is(err, store.ErrDuplicateUsername),
			errors.Is(err, store.ErrUniqueConstraintViolation):
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	if input.Password != "" {
		if err := h.svc.SetPassword(r.Context(), tenant, u.ID, input.Password); err != nil {
			_ = h.svc.Delete(r.Context(), tenant, u.ID) // justified: best-effort compensation after SetPassword failed; that failure is already returned to the client, and a leftover passwordless user cannot authenticate
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	writeJSON(w, http.StatusCreated, &u)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, id string) {
	tenant, ok := tenantOf(w, r)
	if !ok {
		return
	}
	if !canMutateUser(r, id) {
		http.Error(w, "forbidden: admin scope required to modify the credential owner", http.StatusForbidden)
		return
	}
	var input userRequest
	if err := portslib.DecodeJSONBody(r.Body, &input); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if input.Password != "" && !canSetPassword(r) {
		http.Error(w, "forbidden: admin scope required to set a password", http.StatusForbidden)
		return
	}
	if err := validatePassword(input.Password); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	existing, err := h.svc.Get(r.Context(), tenant, id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	active := existing.Active
	if input.Active != nil {
		active = *input.Active
	}
	u := input.user(tenant, id, active)
	if err := h.svc.Update(r.Context(), &u); err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrValidation), errors.Is(err, ErrUnknownTenant):
			status = http.StatusBadRequest
		case errors.Is(err, store.ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, store.ErrDuplicateEmail),
			errors.Is(err, store.ErrDuplicateUsername),
			errors.Is(err, store.ErrUniqueConstraintViolation):
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	if input.Password != "" {
		if err := h.svc.SetPassword(r.Context(), tenant, id, input.Password); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	writeJSON(w, http.StatusOK, &u)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request, id string) {
	tenant, ok := tenantOf(w, r)
	if !ok {
		return
	}
	if !canMutateUser(r, id) {
		http.Error(w, "forbidden: admin scope required to modify the credential owner", http.StatusForbidden)
		return
	}
	// Deleting the account you are authenticated as destroys the identity
	// behind the in-flight credential. Refuse it: another administrator (or a
	// platform operation) removes an operator's account.
	if principal := identity.PrincipalFromContext(r.Context()); principal.Subject == id {
		http.Error(w, "conflict: a user cannot delete their own account", http.StatusConflict)
		return
	}
	if err := h.svc.Delete(r.Context(), tenant, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "user not found", http.StatusNotFound)
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
	_ = json.NewEncoder(w).Encode(v) // justified: the status and headers are already written; an encode failure mid-body cannot be reported to the client
}
