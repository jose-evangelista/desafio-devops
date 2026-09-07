package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testMux() *http.ServeMux {
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	noop := func(string) Middleware {
		return func(next http.Handler) http.Handler { return next }
	}
	return NewMux(log, noop)
}

func TestRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
		wantCType  string
	}{
		{"projeto korp", http.MethodGet, RouteProjetoKorp, http.StatusOK, contentType},
		{"liveness", http.MethodGet, RouteHealthz, http.StatusOK, "text/plain; charset=utf-8"},
		{"readiness", http.MethodGet, RouteReadyz, http.StatusOK, "text/plain; charset=utf-8"},
		{"wrong method", http.MethodPost, RouteProjetoKorp, http.StatusMethodNotAllowed, ""},
		{"unknown path", http.MethodGet, "/nao-existe", http.StatusNotFound, ""},
		{"metrics is not public", http.MethodGet, "/metrics", http.StatusNotFound, ""},
		{"root", http.MethodGet, "/", http.StatusNotFound, ""},
	}

	mux := testMux()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.target, nil))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantCType != "" {
				if got := rec.Header().Get("Content-Type"); got != tt.wantCType {
					t.Errorf("Content-Type = %q, want %q", got, tt.wantCType)
				}
			}
		})
	}
}

func TestMethodNotAllowedAdvertisesGet(t *testing.T) {
	rec := httptest.NewRecorder()
	testMux().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, RouteProjetoKorp, nil))

	if got := rec.Header().Get("Allow"); got != http.MethodGet {
		t.Errorf("Allow = %q, want %q", got, http.MethodGet)
	}
}

func TestUnmatchedPathsAreInstrumentedUnderOneLabel(t *testing.T) {
	var seen []string
	spy := func(route string) Middleware {
		seen = append(seen, route)
		return func(next http.Handler) http.Handler { return next }
	}

	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	NewMux(log, spy)

	var others int
	for _, route := range seen {
		if route == routeOther {
			others++
		}
	}
	if others != 1 {
		t.Errorf("routeOther registered %d times, want 1: %v", others, seen)
	}
}

func TestProjetoKorpBody(t *testing.T) {
	rec := httptest.NewRecorder()
	testMux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, RouteProjetoKorp, nil))

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	// The challenge specifies this object exactly; an extra field is a failure.
	if len(body) != 2 {
		t.Errorf("response has %d fields, want exactly 2: %v", len(body), body)
	}

	if body["nome"] != serviceLabel {
		t.Errorf("nome = %v, want %q", body["nome"], serviceLabel)
	}

	horario, ok := body["horario"].(string)
	if !ok {
		t.Fatalf("horario is %T, want string", body["horario"])
	}

	parsed, err := time.Parse(time.RFC3339, horario)
	if err != nil {
		t.Fatalf("horario %q is not RFC3339: %v", horario, err)
	}
	if _, offset := parsed.Zone(); offset != 0 {
		t.Errorf("horario offset = %d, want UTC", offset)
	}
	if elapsed := time.Since(parsed); elapsed > time.Minute || elapsed < -time.Minute {
		t.Errorf("horario %q is not close to now", horario)
	}
}

func TestHorarioIsResolvedPerRequest(t *testing.T) {
	mux := testMux()

	first := horarioFrom(t, mux)
	time.Sleep(1100 * time.Millisecond)
	second := horarioFrom(t, mux)

	if first == second {
		t.Errorf("horario did not change between requests: %q", first)
	}
}

func horarioFrom(t *testing.T, mux *http.ServeMux) string {
	t.Helper()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, RouteProjetoKorp, nil))

	var body struct {
		Horario string `json:"horario"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body.Horario
}
