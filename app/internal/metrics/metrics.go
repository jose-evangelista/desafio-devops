// Package metrics is the only package that imports the Prometheus client.
// Everything else receives plain net/http middleware.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inFlight prometheus.Gauge
}

func New(version, commit string) *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		requests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "Total number of HTTP requests handled.",
			},
			[]string{"method", "code", "path"},
		),
		duration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_duration_seconds",
				Help:    "HTTP request latency in seconds.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "path"},
		),
		inFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "http_requests_in_flight",
			Help: "Number of HTTP requests currently being served.",
		}),
	}

	buildInfo := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "korp_build_info",
			Help: "Build metadata of the running binary, always 1.",
		},
		[]string{"version", "commit"},
	)
	buildInfo.WithLabelValues(version, commit).Set(1)

	m.registry.MustRegister(
		m.requests,
		m.duration,
		m.inFlight,
		buildInfo,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	return m
}

// Instrument returns middleware bound to route. The path label comes from this
// argument and never from the request URL, which caps its cardinality.
func (m *Metrics) Instrument(route string) func(http.Handler) http.Handler {
	labels := prometheus.Labels{"path": route}
	requests := m.requests.MustCurryWith(labels)
	duration := m.duration.MustCurryWith(labels)

	return func(next http.Handler) http.Handler {
		return promhttp.InstrumentHandlerInFlight(
			m.inFlight,
			promhttp.InstrumentHandlerCounter(
				requests,
				promhttp.InstrumentHandlerDuration(duration, next),
			),
		)
	}
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.HTTPErrorOnError,
	})
}
