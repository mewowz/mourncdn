package mourncdn

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewLocalAssetCache(t *testing.T) {
	testDirPath := t.TempDir()

	testFilePath := filepath.Join(testDirPath, "file.jpg")
	err := os.WriteFile(
		testFilePath,
		[]byte{},
		0o777,
	)
	if err != nil {
		t.Fatalf("could not make temp file %q: %v", testFilePath, err)
	}

	tests := []struct {
		name         string
		assetDir     string
		assetMaxSize int64
		cacheMaxSize int64
		ttl          time.Duration
		expectedErr  error
	}{
		{
			"no failure",
			testDirPath,
			1,
			2,
			time.Second * 1,
			nil,
		},
		{
			name:        "assetDir is not a dir",
			assetDir:    testFilePath,
			expectedErr: ErrNotADir,
		},
		{
			name:         "assetMaxSize is negative",
			assetDir:     testDirPath,
			assetMaxSize: -1,
			expectedErr:  ErrInvalidCacheLimit,
		},
		{
			name:         "cacheMaxSize is negative",
			assetDir:     testDirPath,
			assetMaxSize: 1,
			cacheMaxSize: -1,
			expectedErr:  ErrInvalidCacheLimit,
		},
		{
			name:         "assetMaxSize > cacheMaxSize",
			assetDir:     testDirPath,
			assetMaxSize: 2,
			cacheMaxSize: 1,
			expectedErr:  ErrAssetLimitExceedsCacheLimit,
		},
		{
			name:         "ttl is negative",
			assetDir:     testDirPath,
			assetMaxSize: 1,
			cacheMaxSize: 2,
			ttl:          time.Second * -1,
			expectedErr:  ErrInvalidTTL,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewLocalAssetCache(
				test.assetDir,
				test.assetMaxSize,
				test.cacheMaxSize,
				test.ttl,
			)

			if !errors.Is(err, test.expectedErr) {
				t.Errorf("got %v, want %v", err, test.expectedErr)
			}
		})
	}
}
