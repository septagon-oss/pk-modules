package content

// Implements: REQ-CONTENT-001.
// Per: ADR-0017.
// Discipline: C-14.
// handler.go owns the HTTP handlers that expose the canonical
// /api/v1/content surface. The handler is mountable via RegisterRoutes
// against any http.ServeMux.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/septagon-oss/pk-core/pkg/security/identity"
	"github.com/septagon-oss/pk-modules/pkg/content/store"
	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

// tenantOf returns the tenant the request is authorized to act in, taken from
// the authenticated principal on the context — never from client-supplied
// input. When the principal carries no tenant (anonymous or cross-tenant
// caller) it writes 401 and reports ok=false; callers must return without
// touching the store. This is the single choke point that keeps by-ID
// operations from crossing the tenant boundary.
func tenantOf(w http.ResponseWriter, r *http.Request) (string, bool) {
	tid := strings.TrimSpace(identity.PrincipalFromContext(r.Context()).TenantID)
	if tid == "" {
		http.Error(w, "unauthorized: no tenant in request identity", http.StatusUnauthorized)
		return "", false
	}
	return tid, true
}

// Handler exposes the content HTTP surface.
type Handler struct {
	svc ContentService
}

// NewHandler constructs a Handler wired to the given service.
func NewHandler(svc ContentService) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes mounts the handler under the canonical APIPath on the given
// mux. Both APIPath and APIPath+"/" patterns are wired so the standard mux
// dispatches both collection and item routes.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle(APIPath, h)
	mux.Handle(APIPath+"/", h)
}

// ServeHTTP dispatches to the appropriate handler method.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The path is the collection, "<id>", or "<id>/<verb>".
	id, verb, err := portslib.EntityIDFromPath(r.URL.Path, APIPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if id == "" {
		switch r.Method {
		case http.MethodGet:
			h.list(w, r)
		case http.MethodPost:
			h.create(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	switch verb {
	case "":
		switch r.Method {
		case http.MethodGet:
			h.get(w, r, id)
		case http.MethodPut:
			h.update(w, r, id)
		case http.MethodDelete:
			h.delete(w, r, id)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "publish":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.publish(w, r, id)
	case "unpublish":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.unpublish(w, r, id)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	kind := q.Get("kind")
	limit, offset, err := portslib.ParsePagination(q)
	if err != nil {
		http.Error(w, "invalid pagination: "+err.Error(), http.StatusBadRequest)
		return
	}
	rows, err := h.svc.List(r.Context(), tenantID, kind, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	tenantID, subject, ok := portslib.RequestActor(w, r)
	if !ok {
		return
	}
	var c Content
	if err := portslib.DecodeJSONBody(r.Body, &c); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	// Server-owned fields come from the request identity / server, never the
	// body: tenant and author are the authenticated caller, and the ID is
	// generated server-side. A client cannot forge cross-tenant rows, spoof the
	// author, or choose a predictable/colliding ID.
	c.TenantID = tenantID
	c.AuthorID = subject
	c.ID = ""
	if err := h.svc.Create(r.Context(), &c); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, &c)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, id string) {
	tenantID, ok := tenantOf(w, r)
	if !ok {
		return
	}
	c, err := h.svc.Get(r.Context(), tenantID, id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, id string) {
	tenantID, ok := tenantOf(w, r)
	if !ok {
		return
	}
	var c Content
	if err := portslib.DecodeJSONBody(r.Body, &c); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	c.ID = id
	c.TenantID = tenantID
	if err := h.svc.Update(r.Context(), &c); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, &c)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request, id string) {
	tenantID, ok := tenantOf(w, r)
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), tenantID, id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) publish(w http.ResponseWriter, r *http.Request, id string) {
	tenantID, ok := tenantOf(w, r)
	if !ok {
		return
	}
	if err := h.svc.Publish(r.Context(), tenantID, id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) unpublish(w http.ResponseWriter, r *http.Request, id string) {
	tenantID, ok := tenantOf(w, r)
	if !ok {
		return
	}
	if err := h.svc.Unpublish(r.Context(), tenantID, id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // justified: the status and headers are already written; an encode failure mid-body cannot be reported to the client
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, store.ErrSlugTaken), errors.Is(err, store.ErrDuplicate):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}
