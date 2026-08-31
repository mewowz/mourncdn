package main

import (
	"errors"
	"net/http"
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
					"did not find error string fragment '%v' in '%v'",
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
					"got writer.statusCode=%v, want %v",
					writer.statusCode,
					test.expectedStatus,
				)
			}
		})
	}
}
