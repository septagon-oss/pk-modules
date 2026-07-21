package tenant

// Implements: REQ-TENANT-001.
// Per: ADR-0017.
// Discipline: C-14.
// handler.go owns the HTTP handlers that expose the canonical
// /api/v1/tenants surface. The handler is mountable via RegisterRoutes against
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
	"github.com/septagon-oss/pk-modules/pkg/tenant/store"
)

// callerTenant returns the tenant the authenticated principal belongs to.
// Unlike the other business modules, tenant records are top-level (there is no
// parent tenant to scope by), so the HTTP surface enforces a self-only rule:
// a caller may read or modify ONLY its own tenant. Anonymous callers get 401.
//
// The tenant SERVICE methods stay un-scoped on purpose — they are also used
// internally (seed provisioning, user_management validating a tenant_id) with
// arbitrary tenant IDs. The authorization boundary is here, at the HTTP edge.
func callerTenant(w http.ResponseWriter, r *http.Request) (string, bool) {
	tid := strings.TrimSpace(identity.PrincipalFromContext(r.Context()).TenantID)
	if tid == "" {
		http.Error(w, "unauthorized: no tenant in request identity", http.StatusUnauthorized)
		return "", false
	}
	return tid, true
}

// Handler exposes the tenant CRUD HTTP surface.
type Handler struct {
	svc TenantService
}

// NewHandler constructs a Handler wired to the given service.
func NewHandler(svc TenantService) *Handler { return &Handler{svc: svc} }

// RegisterRoutes mounts the handler under the canonical APIPath on the given
// mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle(APIPath, h)
	mux.Handle(APIPath+"/", h)
}

// ServeHTTP dispatches to the appropriate handler method.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, APIPath), "/")
	// Reject IDs that still contain a slash — those are nested paths the
	// handler does not own. Without this guard a request like
	// /api/v1/tenants/a/b would resolve to id="a/b" and be served as a
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
	// A tenant-scoped caller sees only its own tenant, never the platform-wide
	// list. (The full svc.List() is a platform/operator view, not exposed here.)
	self, ok := callerTenant(w, r)
	if !ok {
		return
	}
	t, err := h.svc.Get(r.Context(), self)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusOK, []*Tenant{})
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, []*Tenant{t})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, id string) {
	self, ok := callerTenant(w, r)
	if !ok {
		return
	}
	// Self-only: another tenant's record reads back as 404, never disclosed.
	if id != self {
		http.Error(w, "tenant not found", http.StatusNotFound)
		return
	}
	t, err := h.svc.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "tenant not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	// Provisioning a new tenant is a platform/operator operation, not something
	// a tenant-scoped caller may do (it would be privilege escalation, and OSS
	// ships no platform-admin role). Tenants are seeded in-process or created
	// by an operator out of band. A downstream distribution with a platform
	// authz tier can re-open this route behind that check.
	if _, ok := callerTenant(w, r); !ok {
		return
	}
	http.Error(w, "tenant provisioning is a platform operation and is not available via the tenant-scoped API", http.StatusForbidden)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, id string) {
	self, ok := callerTenant(w, r)
	if !ok {
		return
	}
	if id != self {
		http.Error(w, "tenant not found", http.StatusNotFound)
		return
	}
	var t Tenant
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	t.ID = id
	if err := h.svc.Update(r.Context(), &t); err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, store.ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, store.ErrDuplicateSlug):
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, http.StatusOK, &t)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request, id string) {
	self, ok := callerTenant(w, r)
	if !ok {
		return
	}
	if id != self {
		http.Error(w, "tenant not found", http.StatusNotFound)
		return
	}
	if err := h.svc.Delete(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "tenant not found", http.StatusNotFound)
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
