package main

import (
	"errors"
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
