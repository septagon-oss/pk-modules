package notification

// Implements: REQ-NOTIF-002.
// Per: ADR-0017.
// Discipline: C-14.
// handler.go owns the HTTP handlers that expose the canonical
// /api/v1/notifications and /api/v1/notification-subscriptions surfaces.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/septagon-oss/pk-modules/pkg/notification/store"
	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

// Handler exposes the notification HTTP surface.
type Handler struct {
	svc NotificationService
}

// NewHandler constructs a Handler wired to the given service.
func NewHandler(svc NotificationService) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes mounts the handler under the canonical APIPath /
// SubscriptionAPIPath patterns on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc(APIPath, h.serveNotifications)
	mux.HandleFunc(APIPath+"/", h.serveNotificationItem)
	mux.HandleFunc(SubscriptionAPIPath, h.serveSubscriptions)
	mux.HandleFunc(SubscriptionAPIPath+"/", h.serveSubscriptionItem)
}

// ServeHTTP routes notification collection requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.serveNotifications(w, r)
}

func (h *Handler) serveNotifications(w http.ResponseWriter, r *http.Request) {
	// Notifications are per-user resources: tenant AND user (subject) are
	// authoritative from the request identity, never from client input. A
	// caller reads and creates only its OWN notifications over HTTP; the
	// in-process service still accepts an explicit recipient for system
	// delivery.
	tenant, subject, ok := portslib.RequestActor(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		limit, offset, err := portslib.ParsePagination(r.URL.Query())
		if err != nil {
			http.Error(w, "invalid pagination: "+err.Error(), http.StatusBadRequest)
			return
		}
		got, err := h.svc.GetByUser(r.Context(), tenant, subject, limit, offset)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, got)
	case http.MethodPost:
		var n portslib.Notification
		if err := portslib.DecodeJSONBody(r.Body, &n); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		n.ID = ""
		n.TenantID = tenant
		n.UserID = subject
		n.EmittedAt = time.Time{}
		if err := h.svc.Create(r.Context(), &n); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, &n)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) serveNotificationItem(w http.ResponseWriter, r *http.Request) {
	tenant, subject, ok := portslib.RequestActor(w, r)
	if !ok {
		return
	}
	id, verb, err := portslib.EntityIDFromPath(r.URL.Path, APIPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	switch verb {
	case "read":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Scoped to the caller's own notification: a tenant-mate cannot mark
		// another user's notification read by ID.
		if err := h.svc.MarkRead(r.Context(), tenant, subject, id); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) serveSubscriptions(w http.ResponseWriter, r *http.Request) {
	tenant, subject, ok := portslib.RequestActor(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodPost:
		var sub Subscription
		if err := portslib.DecodeJSONBody(r.Body, &sub); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		// Tenant and user are authoritative from the identity: a caller
		// subscribes itself, never another user.
		sub.ID = ""
		sub.TenantID = tenant
		sub.UserID = subject
		sub.CreatedAt = time.Time{}
		if err := h.svc.Subscribe(r.Context(), &sub); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, &sub)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) serveSubscriptionItem(w http.ResponseWriter, r *http.Request) {
	tenant, subject, ok := portslib.RequestActor(w, r)
	if !ok {
		return
	}
	id, _, err := portslib.EntityIDFromPath(r.URL.Path, SubscriptionAPIPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Scoped to the caller's own subscription.
	if err := h.svc.Unsubscribe(r.Context(), tenant, subject, id); err != nil {
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
	case errors.Is(err, store.ErrDuplicate):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}
