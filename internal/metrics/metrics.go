package metrics

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	BytesTransferred *prometheus.CounterVec
	CacheHits        prometheus.Counter
	CacheMisses      prometheus.Counter
	CacheSize        prometheus.Gauge
	TotalRequests    *prometheus.CounterVec
	RequestsDuration *prometheus.HistogramVec
}

type MetricsServerConfig struct {
	Addr                   string               `koanf:"address"`
	Route                  string               `koanf:"endpoint"`
	ShutdownTimeoutSeconds time.Duration        `koanf:"advanced.shutdown-timeout"`
	PromCfg                promhttp.HandlerOpts `koanf:"-"`
}

type StatusWriter struct {
	http.ResponseWriter

	status int
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		BytesTransferred: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "bytes_transferred",
				Help: "Tracks the number of bytes transferred via ingress and egress",
			},
			[]string{"direction"},
		),
		CacheHits: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "cache_hits",
				Help: "Counts the amount of cache hits",
			},
		),
		CacheMisses: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "cache_misses",
				Help: "Counts the amount of cache misses",
			},
		),
		CacheSize: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "cache_size",
				Help: "Tracks the current cache size",
			},
		),
		TotalRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "The total number of HTTP requests",
			},
			[]string{"method", "status"},
		),
		RequestsDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "http_request_duration_seconds",
				Help: "Amount of time in seconds for each request",
			},
			[]string{"method", "status"},
		),
	}

	reg.MustRegister(
		m.BytesTransferred,
		m.CacheHits,
		m.CacheSize,
		m.CacheMisses,
		m.TotalRequests,
		m.RequestsDuration,
	)

	return m
}

func ServeMetricsWithContext(
	ctx context.Context,
	reg *prometheus.Registry,
	cfg MetricsServerConfig,
) error {
	mux := http.NewServeMux()
	mux.Handle(cfg.Route, promhttp.HandlerFor(
		reg,
		cfg.PromCfg,
	))

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: mux,
	}

	errCh := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		errCh <- err
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	shutdownCtx, shutdownCtxCancelF := context.WithTimeout(
		context.Background(), cfg.ShutdownTimeoutSeconds,
	)
	defer shutdownCtxCancelF()

	err := srv.Shutdown(shutdownCtx)
	if err == nil {
		return nil
	}

	closeErr := srv.Close()
	return errors.Join(err, closeErr)
}

func (w *StatusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *StatusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(p)
}

func (w *StatusWriter) StatusString() string {
	return strconv.Itoa(w.status)
}
