package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestInstrumentCountsRequests(t *testing.T) {
	m := New("test", "abc123")

	handler := m.Instrument("/projeto-korp")(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
	))

	for range 3 {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/projeto-korp", nil))
	}

	want := `
		# HELP http_requests_total Total number of HTTP requests handled.
		# TYPE http_requests_total counter
		http_requests_total{code="200",method="get",path="/projeto-korp"} 3
	`
	if err := testutil.CollectAndCompare(m.requests, strings.NewReader(want), "http_requests_total"); err != nil {
		t.Error(err)
	}
}

func TestPathLabelIgnoresRequestURL(t *testing.T) {
	m := New("test", "abc123")

	handler := m.Instrument("/projeto-korp")(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
	))
	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/projeto-korp?cache-buster=1234", nil))

	if got := testutil.CollectAndCount(m.requests); got != 1 {
		t.Errorf("series count = %d, want 1", got)
	}
}

func TestBuildInfo(t *testing.T) {
	m := New("1.2.3", "deadbeef")

	want := `
		# HELP korp_build_info Build metadata of the running binary, always 1.
		# TYPE korp_build_info gauge
		korp_build_info{commit="deadbeef",version="1.2.3"} 1
	`
	if err := testutil.GatherAndCompare(m.registry, strings.NewReader(want), "korp_build_info"); err != nil {
		t.Error(err)
	}
}

func TestHandlerExposesMetrics(t *testing.T) {
	m := New("test", "abc123")

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	for _, name := range []string{
		"http_requests_in_flight",
		"korp_build_info",
		"go_goroutines",
		"process_start_time_seconds",
	} {
		if !strings.Contains(rec.Body.String(), name) {
			t.Errorf("%s missing from /metrics output", name)
		}
	}
}
