package metrics

import (
	"context"
	"errors"
	"io"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func TestServeMetricsWithContext(t *testing.T) {
	t.Run("startup failure returns early", func(t *testing.T) {
		listener, err := net.Listen("tcp", "localhost:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err = ServeMetricsWithContext(ctx, prometheus.NewRegistry(), MetricsServerConfig{
			Addr:                   listener.Addr().String(),
			Route:                  "/upload",
			ShutdownTimeoutSeconds: 3 * time.Second,
			PromCfg:                promhttp.HandlerOpts{},
		})

		if err == nil {
			t.Fatalf("got err=nil, want non-nil")
		}
		if ctx.Err() != nil {
			t.Fatalf("server waited for context cancellation: %v", err)
		}
	})

	t.Run("context cancellation triggers graceful shutdown", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		errCh := make(chan error, 1)
		go func() {
			err := ServeMetricsWithContext(ctx, prometheus.NewRegistry(), MetricsServerConfig{
				Addr:                   "localhost:0",
				Route:                  "/upload",
				ShutdownTimeoutSeconds: 3 * time.Second,
				PromCfg:                promhttp.HandlerOpts{},
			})
			errCh <- err
		}()

		cancel()
		err := <-errCh
		if err != nil {
			t.Fatalf("got err=%v, want nil", err)
		}
	})

	t.Run("timeout forces connections close", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		testCtx, stopTest := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopTest()

		started := make(chan struct{})
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		reg := prometheus.NewRegistry()
		reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "blocked_scrape",
			Help: "Keeps a scrape active during the shutdown test",
		}, func() float64 {
			close(started)
			<-release
			return 1
		}))

		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := listener.Addr().String()
		err = listener.Close()
		if err != nil {
			t.Fatal(err)
		}

		errCh := make(chan error, 1)
		go func() {
			err := ServeMetricsWithContext(ctx, reg, MetricsServerConfig{
				Addr:                   addr,
				Route:                  "/upload",
				ShutdownTimeoutSeconds: 0,
				PromCfg:                promhttp.HandlerOpts{},
			})
			errCh <- err
		}()

		var conn net.Conn
		dialer := net.Dialer{}
		retry := time.NewTicker(10 * time.Millisecond)
		defer retry.Stop()
		for {
			conn, err = dialer.DialContext(testCtx, "tcp", addr)
			if err == nil {
				break
			}
			select {
			case err := <-errCh:
				t.Fatalf("server exited before scrape: %v", err)
			case <-testCtx.Done():
				t.Fatalf("waiting for server to listen: %v", err)
			case <-retry.C:
			}
		}
		t.Cleanup(func() { _ = conn.Close() })
		deadline, _ := testCtx.Deadline()
		err = conn.SetDeadline(deadline)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.WriteString(conn, "GET /upload HTTP/1.1\r\nHost: localhost\r\n\r\n")
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-started:
		case err := <-errCh:
			t.Fatalf("server exited before scrape started: %v", err)
		case <-testCtx.Done():
			t.Fatal("timed out waiting for scrape to start")
		}
		cancel()

		select {
		case err := <-errCh:
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("got err=%v, want %v", err, context.DeadlineExceeded)
			}
		case <-testCtx.Done():
			t.Fatal("server did not return after shutdown timed out")
		}

		n, err := io.Copy(io.Discard, conn)
		if err != nil && !errors.Is(err, syscall.ECONNRESET) {
			t.Fatalf("waiting for forced connection closure: %v", err)
		}
		if n != 0 {
			t.Fatalf("got %d response bytes while scrape was blocked, want 0", n)
		}
	})
}
