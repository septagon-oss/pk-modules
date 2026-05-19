package notification

// handler.go owns the HTTP handlers that expose the canonical
// /api/v1/notifications and /api/v1/notification-subscriptions surfaces.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

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
	switch r.Method {
	case http.MethodGet:
		userID := r.URL.Query().Get("user_id")
		limit := parseIntDefault(r.URL.Query().Get("limit"), 0)
		offset := parseIntDefault(r.URL.Query().Get("offset"), 0)
		got, err := h.svc.GetByUser(r.Context(), userID, limit, offset)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, got)
	case http.MethodPost:
		var n portslib.Notification
		if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
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
	path := strings.TrimPrefix(r.URL.Path, APIPath+"/")
	parts := strings.SplitN(path, "/", 2)
	id := parts[0]
	verb := ""
	if len(parts) == 2 {
		verb = parts[1]
	}
	switch verb {
	case "read":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := h.svc.MarkRead(r.Context(), id); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) serveSubscriptions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var sub Subscription
		if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
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
	id := strings.TrimPrefix(r.URL.Path, SubscriptionAPIPath+"/")
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := h.svc.Unsubscribe(r.Context(), id); err != nil {
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
	case errors.Is(err, store.ErrDuplicate):
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
