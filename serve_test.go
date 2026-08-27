package main

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestNewLocalAssetServer(t *testing.T) {
	testDirPath := t.TempDir()

	tests := []struct {
		name        string
		cfg         localAssetServerConfig
		logger      *slog.Logger
		expectedErr error
	}{
		{
			"good config no error",
			localAssetServerConfig{
				testDirPath,
				1,
				2,
				time.Second,
				1,
				time.Second,
			},
			nil,
			nil,
		},
		{
			"good config custom logger",
			localAssetServerConfig{
				testDirPath,
				1,
				2,
				time.Second,
				1,
				time.Second,
			},
			slog.New(slog.DiscardHandler),
			nil,
		},
		{
			"NewLocalAssetCache errors propagated",
			localAssetServerConfig{
				filepath.Join(testDirPath, "doesnotexist"),
				1,
				2,
				time.Second,
				1,
				time.Second,
			},
			nil,
			os.ErrNotExist,
		},
		{
			"<= 0 WriteBufSize raises error",
			localAssetServerConfig{
				testDirPath,
				1,
				2,
				time.Second,
				-1,
				time.Second,
			},
			nil,
			ErrInvalidWriteBufSize,
		},
		{
			"<= 0 WriteWindow raises error",
			localAssetServerConfig{
				testDirPath,
				1,
				2,
				time.Second,
				1,
				-1 * time.Second,
			},
			nil,
			ErrInvalidWriteWindow,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NewLocalAssetServer(
				test.cfg,
				test.logger,
			)
			if !errors.Is(err, test.expectedErr) {
				t.Fatalf("got %v, want %v", err, test.expectedErr)
			}

			if test.expectedErr != nil {
				return
			}

			if test.logger != nil && got.logger != test.logger {
				t.Fatalf("got.logger=%v, want logger=%v", got.logger, test.logger)
			}

			want := &LocalAssetServer{
				writeBufSize: test.cfg.WriteBufSize,
				writeWindow:  test.cfg.WriteWindow,
			}

			diff := cmp.Diff(
				got,
				want,
				cmp.AllowUnexported(LocalAssetServer{}),
				cmpopts.IgnoreFields(LocalAssetServer{}, "cache", "logger"),
			)
			if diff != "" {
				t.Errorf("NewLocalAssetServer() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}
