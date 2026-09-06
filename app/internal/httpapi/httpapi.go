// Package httpapi holds the public HTTP surface of the service.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	RouteProjetoKorp = "/projeto-korp"
	RouteHealthz     = "/healthz"
	RouteReadyz      = "/readyz"

	// Label for every unmatched path. A constant keeps the request URL out of
	// the metric, which would otherwise be unbounded cardinality.
	routeOther = "other"

	serviceLabel = "Projeto Korp"
	contentType  = "application/json; charset=utf-8"
)

// Middleware decorates a handler. The metrics package supplies the concrete
// implementation, which keeps the instrumentation library out of this package.
type Middleware = func(http.Handler) http.Handler

// Instrumenter builds a Middleware bound to a fixed route label.
type Instrumenter func(route string) Middleware

type projetoKorpResponse struct {
	Nome    string `json:"nome"`
	Horario string `json:"horario"`
}

// NewMux wires the routes. Health endpoints stay uninstrumented so probe
// traffic does not distort the request metrics.
func NewMux(log *slog.Logger, instrument Instrumenter) *http.ServeMux {
	mux := http.NewServeMux()

	mux.Handle("GET "+RouteProjetoKorp, instrument(RouteProjetoKorp)(projetoKorp(log)))
	mux.Handle("GET "+RouteHealthz, ok())
	mux.Handle("GET "+RouteReadyz, ok())

	mux.Handle(RouteProjetoKorp, instrument(RouteProjetoKorp)(methodNotAllowed(http.MethodGet)))
	mux.Handle("/", instrument(routeOther)(notFound()))

	return mux
}

func projetoKorp(log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := projetoKorpResponse{
			Nome:    serviceLabel,
			Horario: time.Now().UTC().Format(time.RFC3339),
		}

		w.Header().Set("Content-Type", contentType)
		if err := json.NewEncoder(w).Encode(body); err != nil {
			log.ErrorContext(r.Context(), "failed to encode response", slog.Any("error", err))
		}
	})
}

func methodNotAllowed(allowed ...string) http.Handler {
	allow := strings.Join(allowed, ", ")

	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Allow", allow)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})
}

func notFound() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
}

func ok() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
}
