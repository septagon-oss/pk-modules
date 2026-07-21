package audit

// handler.go owns the HTTP handlers that expose the canonical
// /api/v1/audit-events surface. The handler is mountable via RegisterRoutes
// against any http.ServeMux. The OSS surface is GET (query) and POST
// (record); the log is append-only so PUT/DELETE are not exposed.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/septagon-oss/pk-core/pkg/security/identity"
)

// tenantOf returns the tenant the request is authorized to act in, taken from
// the authenticated principal — never from client input. Anonymous or
// no-tenant callers get 401. Audit reads and writes are confined to this
// tenant, so no caller can read or forge another tenant's audit trail.
func tenantOf(w http.ResponseWriter, r *http.Request) (string, bool) {
	tid := identity.PrincipalFromContext(r.Context()).TenantID
	if tid == "" {
		http.Error(w, "unauthorized: no tenant in request identity", http.StatusUnauthorized)
		return "", false
	}
	return tid, true
}

// Handler exposes the audit HTTP surface.
type Handler struct {
	svc    AuditService
	reader AuditReader
}

// NewHandler constructs a Handler wired to the given service/reader pair.
func NewHandler(svc AuditService, reader AuditReader) *Handler {
	return &Handler{svc: svc, reader: reader}
}

// RegisterRoutes mounts the handler under the canonical APIPath on the given
// mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle(APIPath, h)
}

// ServeHTTP dispatches to the appropriate handler method.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.list(w, r)
	case http.MethodPost:
		h.record(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	tenant, ok := tenantOf(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	filter := QueryFilter{
		TenantID: tenant, // authoritative from identity, never the query string
		Actor:    q.Get("actor"),
		Action:   q.Get("action"),
	}
	// Malformed since/until/limit previously fell through silently, which
	// hid pagination/time-window bugs in callers. Surface them as 400 so
	// integration tests and humans see the mistake immediately.
	if v := q.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, "invalid since: must be RFC3339", http.StatusBadRequest)
			return
		}
		filter.Since = t
	}
	if v := q.Get("until"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, "invalid until: must be RFC3339", http.StatusBadRequest)
			return
		}
		filter.Until = t
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			http.Error(w, "invalid limit: must be a positive integer", http.StatusBadRequest)
			return
		}
		if n < 0 {
			http.Error(w, "invalid limit: must be a positive integer", http.StatusBadRequest)
			return
		}
		filter.Limit = n
	}
	events, err := h.reader.Query(r.Context(), filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (h *Handler) record(w http.ResponseWriter, r *http.Request) {
	tenant, ok := tenantOf(w, r)
	if !ok {
		return
	}
	var e Event
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	// The event is stamped with the caller's tenant; a body-supplied tenant_id
	// is ignored so a caller cannot forge audit events into another tenant.
	e.TenantID = tenant
	if err := h.svc.Record(r.Context(), &e); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, &e)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
