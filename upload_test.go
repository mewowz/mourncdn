package main

import (
	"bytes"
	"crypto/sha512"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewLocalAssetUploader(t *testing.T) {
	tempDirPath := t.TempDir()

	tests := []struct {
		name                string
		urlPrefix           string
		maxUploadSize       int64
		expectedErr         error
		expectedErrFragment string
	}{
		{
			"no errors",
			"/assets",
			1,
			nil,
			"",
		},
		{
			"negative asset size limit",
			"/assets",
			-1,
			ErrInvalidAssetSizeLimit,
			"",
		},
		{
			"URL prefix not starting with /",
			".assets/",
			1,
			nil,
			"must start with /",
		},
		{
			"URL prefix containing query",
			"/assets?x=1",
			1,
			nil,
			"query or fragment",
		},
		{
			"URL prefix containing fragment",
			"/assets#x=10",
			1,
			nil,
			"query or fragment",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewLocalAssetUploader(
				tempDirPath,
				tempDirPath,
				test.urlPrefix,
				test.maxUploadSize,
			)
			if test.expectedErr != nil && !errors.Is(err, test.expectedErr) {
				t.Fatalf("got %v, want %v", err, test.expectedErr)
			}
			if test.expectedErrFragment != "" &&
				!strings.Contains(err.Error(), test.expectedErrFragment) {
				t.Fatalf(
					"did not find error string fragment '%q' in '%v'",
					test.expectedErrFragment,
					err,
				)
			}
		})
	}
}

func TestLocalAssetUploader_handleAssetUploadErr(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		expectedStatus int
	}{
		{
			"max bytes propagates status 413",
			&http.MaxBytesError{},
			http.StatusRequestEntityTooLarge,
		},
		{
			"unknown error propagates status 500",
			errors.New("unknown error"),
			http.StatusInternalServerError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			u := &LocalAssetUploader{}
			writer := &errorResponseWriter{
				header: make(http.Header),
			}
			u.handleAssetUploadErr(
				test.err,
				writer,
			)

			if writer.statusCode != test.expectedStatus {
				t.Fatalf(
					"got writer.statusCode=%d, want %d",
					writer.statusCode,
					test.expectedStatus,
				)
			}
		})
	}
}

type errorReader struct {
	readErr  error
	closeErr error
}

func (e *errorReader) Read(p []byte) (int, error) {
	return 0, e.readErr
}

func (e *errorReader) Close() error {
	return e.closeErr
}

func TestLocalAssetUploader_writeAssetToDisk(t *testing.T) {
	tempDirPath := t.TempDir()

	pngSig := []byte{
		0x89, 'P', 'N', 'G',
		0x0D, 0x0A, 0x1A, 0x0A,
	}

	errRead := errors.New("read error")

	tests := []struct {
		name            string
		fileData        []byte
		expectedHashSum [sha512.Size]byte
		expectedMIME    string
		readErr         error
		expectedErr     error
	}{
		{
			"no errors; good data",
			pngSig,
			sha512.Sum512(pngSig),
			"image/png",
			nil,
			nil,
		},
		{
			"reader error propagated",
			nil,
			[sha512.Size]byte{},
			"",
			errRead,
			errRead,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			u := &LocalAssetUploader{
				tmpDirPath: tempDirPath,
			}

			var reader io.ReadCloser
			if test.readErr != nil {
				reader = &errorReader{
					readErr: test.readErr,
				}
			} else {
				reader = io.NopCloser(bytes.NewReader(test.fileData))
			}

			got, err := u.writeAssetUploadToDisk(reader)
			if !errors.Is(err, test.expectedErr) {
				t.Fatalf("got err=%v, want %v", err, test.expectedErr)
			}

			if test.expectedErr != nil {
				entries, readDirErr := os.ReadDir(tempDirPath)
				if readDirErr != nil {
					t.Fatal(readDirErr)
				}

				if len(entries) != 0 {
					t.Fatalf(
						"got %d files remaining in temp dir, want 0",
						len(entries),
					)
				}

				return
			}

			if got.Path == "" {
				t.Fatal("got empty asset path")
			}

			if _, err := os.Stat(got.Path); err != nil {
				t.Fatalf("stat uploaded asset: %v", err)
			}

			gotHash := got.Hash.Sum(nil)
			if !bytes.Equal(gotHash, test.expectedHashSum[:]) {
				t.Errorf(
					"got hash=%x, want %x",
					gotHash,
					test.expectedHashSum,
				)
			}

			if got.MimeInfo.String() != test.expectedMIME {
				t.Errorf(
					"got MIME=%q, want %q",
					got.MimeInfo.String(),
					test.expectedMIME,
				)
			}

			if got.Path != filepath.Join(tempDirPath, filepath.Base(got.Path)) {
				t.Errorf(
					"got path=%q outside temp dir %q",
					got.Path,
					tempDirPath,
				)
			}

			if err := os.Remove(got.Path); err != nil {
				t.Fatal(err)
			}
		})
	}
}
