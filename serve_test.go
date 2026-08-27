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

func TestLocalAssetServerCacheAndFetch(t *testing.T) {
	tests := []struct {
		name           string
		fileName       string
		fileData       []byte
		assetMaxSize   int64
		cacheMaxSize   int64
		preCacheName   string
		preCacheData   []byte
		expectedCached bool
		expectedErr    error
	}{
		{
			"cacheable asset is cached and fetched",
			"abcd.jpg",
			[]byte("hello"),
			8,
			16,
			"",
			nil,
			true,
			nil,
		},
		{
			"asset too large to cache is still fetched",
			"abcd.jpg",
			[]byte("hello"),
			4,
			16,
			"",
			nil,
			false,
			nil,
		},
		{
			"cache full is still fetched",
			"abcd.jpg",
			[]byte("data"),
			8,
			8,
			"resident.txt",
			[]byte("12345678"),
			false,
			nil,
		},
		{
			"fetch error is propagated",
			"doesnotexist.png",
			nil,
			8,
			16,
			"",
			nil,
			false,
			os.ErrNotExist,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testDirPath := t.TempDir()

			if test.fileData != nil {
				err := os.WriteFile(
					filepath.Join(testDirPath, test.fileName),
					test.fileData,
					0o777,
				)
				if err != nil {
					t.Fatal(err)
				}
			}

			server, err := NewLocalAssetServer(
				localAssetServerConfig{
					testDirPath,
					test.assetMaxSize,
					test.cacheMaxSize,
					time.Hour,
					1,
					time.Second,
				},
				nil,
			)
			if err != nil {
				t.Fatal(err)
			}

			if test.preCacheName != "" {
				err = os.WriteFile(
					filepath.Join(testDirPath, test.preCacheName),
					test.preCacheData,
					0o777,
				)
				if err != nil {
					t.Fatal(err)
				}

				if err = server.cache.Cache(test.preCacheName); err != nil {
					t.Fatal(err)
				}
			}

			got, err := server.cacheAndFetch(test.fileName)
			if !errors.Is(err, test.expectedErr) {
				t.Fatalf("got err=%v, want %v", err, test.expectedErr)
			}

			if test.expectedErr != nil {
				return
			}

			if got == nil {
				t.Fatal("got nil asset")
			}

			if got.Path != filepath.Join(testDirPath, test.fileName) {
				t.Errorf(
					"got.Path=%v, want %v",
					got.Path,
					filepath.Join(testDirPath, test.fileName),
				)
			}

			if got.IsCached() != test.expectedCached {
				t.Errorf(
					"got.IsCached()=%v, want %v",
					got.IsCached(),
					test.expectedCached,
				)
			}
		})
	}
}
