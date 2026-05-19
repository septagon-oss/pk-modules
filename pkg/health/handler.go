package health

// handler.go owns the HTTP handler that exposes /healthz. It delegates to
// the underlying pk-core health.Registrar's HTTPHandler so the JSON wire
// format, status codes, and degraded-as-200 semantics stay consistent across
// the platform.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import "net/http"

// Handler exposes the /healthz HTTP surface.
type Handler struct {
	inner http.Handler
}

// NewHandler wraps the given http.Handler (typically the pk-core
// health.Registrar HTTPHandler).
func NewHandler(inner http.Handler) *Handler { return &Handler{inner: inner} }

// RegisterRoutes mounts the handler under the canonical APIPath on the given
// mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle(APIPath, h)
}

// ServeHTTP delegates to the wrapped handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.inner.ServeHTTP(w, r)
}
