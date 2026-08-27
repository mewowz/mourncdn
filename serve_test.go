package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

func TestLocalAssetServer_cacheAndFetch(t *testing.T) {
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

type errorResponseWriter struct {
	header     http.Header
	err        error
	statusCode int
}

func (w *errorResponseWriter) Header() http.Header {
	return w.header
}

func (w *errorResponseWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func (w *errorResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}

func TestLocalAssetServer_writeAssetToClient(t *testing.T) {
	errWrite := errors.New("write error")

	tests := []struct {
		name          string
		fileData      []byte
		cacheAsset    bool
		removeFile    bool
		cancelRequest bool
		writeErr      error
		expectedErr   error
	}{
		{
			"uncached asset written",
			[]byte{0x01},
			false,
			false,
			false,
			nil,
			nil,
		},
		{
			"cached asset written",
			[]byte{0x01},
			true,
			false,
			false,
			nil,
			nil,
		},
		{
			"asset open error propagated",
			[]byte{0x01},
			false,
			true,
			false,
			nil,
			os.ErrNotExist,
		},
		{
			"cancelled request propagated",
			[]byte{0x01},
			false,
			false,
			true,
			nil,
			context.Canceled,
		},
		{
			"write error propagated",
			[]byte{0x01},
			false,
			false,
			false,
			errWrite,
			errWrite,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testDirPath := t.TempDir()
			filePath := filepath.Join(testDirPath, "abcd.jpg")

			if err := os.WriteFile(filePath, test.fileData, 0o777); err != nil {
				t.Fatal(err)
			}

			asset, err := NewLocalAsset(filePath)
			if err != nil {
				t.Fatal(err)
			}

			if test.cacheAsset {
				if err = asset.cache(time.Now().Add(time.Hour)); err != nil {
					t.Fatal(err)
				}
			}

			if test.removeFile {
				if err = os.Remove(filePath); err != nil {
					t.Fatal(err)
				}
			}

			server := &LocalAssetServer{
				writeBufSize: 3,
				writeWindow:  time.Second,
			}

			req := httptest.NewRequest(
				http.MethodGet,
				"/abcd.jpg",
				nil,
			)

			if test.cancelRequest {
				ctx, cancel := context.WithCancel(req.Context())
				cancel()
				req = req.WithContext(ctx)
			}

			var recorder *httptest.ResponseRecorder
			var writer http.ResponseWriter

			if test.writeErr != nil {
				writer = &errorResponseWriter{
					header: make(http.Header),
					err:    test.writeErr,
				}
			} else {
				recorder = httptest.NewRecorder()
				writer = recorder
			}

			err = server.writeAssetToClient(asset, writer, req)
			if !errors.Is(err, test.expectedErr) {
				t.Fatalf("got err=%v, want %v", err, test.expectedErr)
			}

			if test.expectedErr != nil {
				return
			}

			if got := recorder.Body.Bytes(); !cmp.Equal(got, test.fileData) {
				t.Errorf(
					"written data mismatch (-want, +got):\n%s",
					cmp.Diff(test.fileData, got),
				)
			}
		})
	}
}

func TestLocalAssetServer_handleCachAndFetchErr(t *testing.T) {
	unknownErr := errors.New("unknown error")
	tests := []struct {
		name           string
		err            error
		expectedStatus int
	}{
		{
			"unknown error propagates status 500",
			unknownErr,
			http.StatusInternalServerError,
		},
		{
			"invalid asset name propagates status 404",
			ErrInvalidAssetName,
			http.StatusNotFound,
		},
		{
			"not a file propagates status 404",
			ErrNotAFile,
			http.StatusNotFound,
		},
		{
			"file not exist propagates status 404",
			os.ErrNotExist,
			http.StatusNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assetServer := &LocalAssetServer{}
			writer := &errorResponseWriter{
				header: make(http.Header),
			}
			assetServer.handleCacheAndFetchErr(
				test.err,
				writer,
			)
			if writer.statusCode != test.expectedStatus {
				t.Fatalf(
					"got writer.statusCode=%v, want %v",
					writer.statusCode,
					test.expectedStatus,
				)
			}
		})
	}
}
