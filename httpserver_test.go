package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metricsServer "github.com/mewowz/mourncdn/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

func TestNewCDNServer(t *testing.T) {
	tmpDirPath := t.TempDir()
	logger := slog.New(slog.DiscardHandler)
	publicKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	authCfg := authMiddleConfig{
		PublicKey: base64.StdEncoding.EncodeToString(publicKey),
	}
	tokenStore, err := NewTokenStore(
		tokenStoreConfig{
			DBDriver:    "sqlite",
			DBPath:      filepath.Join(tmpDirPath, "ts.db"),
			DBOpTimeout: 3 * time.Second,
		},
		logger,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := tokenStore.Close(); err != nil {
			t.Errorf("close token store: %v", err)
		}
	})
	metrics := metricsServer.NewMetrics(prometheus.NewRegistry())

	t.Run("bad serverCfg propagates error", func(t *testing.T) {
		serverCfg := localAssetServerConfig{
			AssetMaxSize: -1,
		}
		_, err := NewCDNServer(
			serverCfg,
			localAssetUploaderConfig{},
			CDNServerConfig{},
			authCfg,
			tokenStore,
			metrics,
			nil,
		)
		if err == nil {
			t.Errorf("got %v, want non-nil", err)
		}
	})

	t.Run("bad uploadCfg propagates error", func(t *testing.T) {
		serverCfg := localAssetServerConfig{
			AssetDir:     tmpDirPath,
			AssetMaxSize: 1024,
			CacheMaxSize: 4096,
			TTL:          time.Minute,
			WriteBufSize: 4096,
			WriteWindow:  time.Second,
		}
		uploadCfg := localAssetUploaderConfig{
			TmpDirPath:         tmpDirPath,
			OutputDirPath:      tmpDirPath,
			MaxAssetUploadSize: -1,
		}
		_, err := NewCDNServer(
			serverCfg,
			uploadCfg,
			CDNServerConfig{},
			authCfg,
			tokenStore,
			metrics,
			nil,
		)
		if err == nil {
			t.Errorf("got %v, want non-nil", err)
		}
	})

	t.Run("auth initialization propagates errors", func(t *testing.T) {
		serverCfg := localAssetServerConfig{
			AssetDir:     tmpDirPath,
			AssetMaxSize: 1024,
			CacheMaxSize: 4096,
			TTL:          time.Minute,
			WriteBufSize: 4096,
			WriteWindow:  time.Second,
		}
		uploadCfg := localAssetUploaderConfig{
			TmpDirPath:         tmpDirPath,
			OutputDirPath:      tmpDirPath,
			URLPrefix:          "/assets",
			MaxAssetUploadSize: 1024,
		}
		cdnCfg := CDNServerConfig{
			ServeRoute:  "/assets",
			UploadRoute: "/upload",
		}
		tests := []struct {
			name                string
			authCfg             authMiddleConfig
			tokenStore          *TokenStore
			expectedErrFragment string
		}{
			{
				"bad authCfg",
				authMiddleConfig{PublicKey: "not-base64"},
				tokenStore,
				"initialize auth middleware:",
			},
			{
				"nil token store",
				authCfg,
				nil,
				"initialize auth middleware: nil token store",
			},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				got, err := NewCDNServer(
					serverCfg,
					uploadCfg,
					cdnCfg,
					test.authCfg,
					test.tokenStore,
					metrics,
					logger,
				)
				if err == nil {
					t.Fatal("got nil, want non-nil error")
				}
				if !strings.Contains(err.Error(), test.expectedErrFragment) {
					t.Fatalf("got %v, want error containing %q", err, test.expectedErrFragment)
				}
				if got != nil {
					t.Fatalf("got server=%v, want nil", got)
				}
			})
		}
	})

	t.Run("assert NewCDNServer output", func(t *testing.T) {
		serverCfg := localAssetServerConfig{
			AssetDir:     tmpDirPath,
			AssetMaxSize: 1024,
			CacheMaxSize: 4096,
			TTL:          time.Minute,
			WriteBufSize: 4096,
			WriteWindow:  time.Second,
		}
		uploadCfg := localAssetUploaderConfig{
			TmpDirPath:         tmpDirPath,
			OutputDirPath:      tmpDirPath,
			URLPrefix:          "/assets",
			MaxAssetUploadSize: 1024,
		}
		cdnCfg := CDNServerConfig{
			ServeRoute:        "/assets",
			UploadRoute:       "/upload",
			Address:           "localhost:8123",
			ReadTimeout:       time.Second,
			ReadHeaderTimeout: time.Second,
			WriteTimeout:      time.Second,
			IdleTimeout:       time.Second,
		}
		got, err := NewCDNServer(
			serverCfg,
			uploadCfg,
			cdnCfg,
			authCfg,
			tokenStore,
			metrics,
			logger,
		)
		if err != nil {
			t.Fatalf("got err=%v, want %v", err, nil)
		}

		gotCfg := struct {
			Addr              string
			ReadTimeout       time.Duration
			ReadHeaderTimeout time.Duration
			WriteTimeout      time.Duration
			IdleTimeout       time.Duration
		}{
			got.server.Addr,
			got.server.ReadTimeout,
			got.server.ReadHeaderTimeout,
			got.server.WriteTimeout,
			got.server.IdleTimeout,
		}

		wantCfg := struct {
			Addr              string
			ReadTimeout       time.Duration
			ReadHeaderTimeout time.Duration
			WriteTimeout      time.Duration
			IdleTimeout       time.Duration
		}{
			cdnCfg.Address,
			cdnCfg.ReadTimeout,
			cdnCfg.ReadHeaderTimeout,
			cdnCfg.WriteTimeout,
			cdnCfg.IdleTimeout,
		}

		if diff := cmp.Diff(wantCfg, gotCfg); diff != "" {
			t.Errorf("server config mismatch (-want, +got):\n%s", diff)
		}
	})
}
