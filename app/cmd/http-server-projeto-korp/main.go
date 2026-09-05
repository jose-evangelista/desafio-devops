package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"korp/internal/httpapi"
	"korp/internal/metrics"
)

// Populated at build time with -ldflags -X.
var (
	version = "dev"
	commit  = "none"
)

const (
	readTimeout       = 10 * time.Second
	readHeaderTimeout = 5 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
	healthcheckTime   = 2 * time.Second
)

type config struct {
	addr            string
	adminAddr       string
	logLevel        slog.Level
	shutdownTimeout time.Duration
}

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe the local liveness endpoint and exit")
	flag.Parse()

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	if *healthcheck {
		os.Exit(probe(cfg.addr))
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.logLevel}))

	if err := run(cfg, log); err != nil {
		log.Error("server terminated", slog.Any("error", err))
		os.Exit(1)
	}
	log.Info("shutdown complete")
}

func run(cfg config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	m := metrics.New(version, commit)

	adminMux := http.NewServeMux()
	adminMux.Handle("GET /metrics", m.Handler())

	public := newServer(cfg.addr, httpapi.NewMux(log, m.Instrument))
	admin := newServer(cfg.adminAddr, adminMux)

	log.Info("starting",
		slog.String("version", version),
		slog.String("commit", commit),
		slog.String("addr", cfg.addr),
		slog.String("admin_addr", cfg.adminAddr),
	)

	errs := make(chan error, 2)
	go serve(public, "public", errs)
	go serve(admin, "admin", errs)

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		log.Info("signal received, draining connections")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.shutdownTimeout)
	defer cancel()

	return errors.Join(public.Shutdown(shutdownCtx), admin.Shutdown(shutdownCtx))
}

func newServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadTimeout:       readTimeout,
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

func serve(s *http.Server, name string, errs chan<- error) {
	if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		errs <- fmt.Errorf("%s server: %w", name, err)
	}
}

func loadConfig() (config, error) {
	cfg := config{
		addr:            envString("KORP_ADDR", ":8080"),
		adminAddr:       envString("KORP_ADMIN_ADDR", ":9101"),
		logLevel:        slog.LevelInfo,
		shutdownTimeout: 10 * time.Second,
	}

	if raw, ok := os.LookupEnv("KORP_LOG_LEVEL"); ok {
		if err := cfg.logLevel.UnmarshalText([]byte(raw)); err != nil {
			return config{}, fmt.Errorf("KORP_LOG_LEVEL: %w", err)
		}
	}

	if raw, ok := os.LookupEnv("KORP_SHUTDOWN_TIMEOUT"); ok {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return config{}, fmt.Errorf("KORP_SHUTDOWN_TIMEOUT: %w", err)
		}
		cfg.shutdownTimeout = d
	}

	return cfg, nil
}

func envString(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// probe backs the container HEALTHCHECK: the distroless runtime has no shell
// and no curl, so the binary checks itself.
func probe(addr string) int {
	client := &http.Client{Timeout: healthcheckTime}

	resp, err := client.Get("http://" + dialable(addr) + httpapi.RouteHealthz)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "unexpected status %d\n", resp.StatusCode)
		return 1
	}
	return 0
}

func dialable(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}
