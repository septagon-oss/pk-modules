package content

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
	"strconv"
	"strings"

	"github.com/septagon-oss/pk-modules/pkg/content/store"
)

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
	path := strings.TrimPrefix(r.URL.Path, APIPath)
	path = strings.TrimPrefix(path, "/")

	if path == "" {
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

	// path is "<id>" or "<id>/<verb>"
	parts := strings.SplitN(path, "/", 2)
	id := parts[0]
	verb := ""
	if len(parts) == 2 {
		verb = parts[1]
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
	q := r.URL.Query()
	tenantID := q.Get("tenant_id")
	kind := q.Get("kind")
	limit := parseIntDefault(q.Get("limit"), 0)
	offset := parseIntDefault(q.Get("offset"), 0)
	rows, err := h.svc.List(r.Context(), tenantID, kind, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var c Content
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := h.svc.Create(r.Context(), &c); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, &c)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, id string) {
	c, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, id string) {
	var c Content
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	c.ID = id
	if err := h.svc.Update(r.Context(), &c); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, &c)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.svc.Delete(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) publish(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.svc.Publish(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) unpublish(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.svc.Unpublish(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
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

func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
