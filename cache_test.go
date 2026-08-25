package mourncdn

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
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

func TestNewLocalAsset(t *testing.T) {
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
		name        string
		path        string
		expectedErr error
	}{
		{
			name:        "no error",
			path:        testFilePath,
			expectedErr: nil,
		},
		{
			"path is not a file",
			testDirPath,
			ErrNotAFile,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewLocalAsset(test.path)
			if !errors.Is(err, test.expectedErr) {
				t.Errorf("got %v, want %v", err, test.expectedErr)
			}
		})
	}
}

func TestLocalAsset_Open(t *testing.T) {
	testDirPath := t.TempDir()

	testFileData := []byte{0x02}
	testFilePath := filepath.Join(testDirPath, "file.jpg")
	err := os.WriteFile(
		testFilePath,
		testFileData,
		0o777,
	)
	if err != nil {
		t.Fatalf("could not make temp file %q: %v", testFilePath, err)
	}

	tests := []struct {
		name         string
		localAsset   *LocalAsset
		expectedData []byte
		expectedErr  error
	}{
		{
			name: "returns cached data",
			localAsset: &LocalAsset{
				cachedBytes: []byte{0x01},
			},
			expectedData: []byte{0x01},
			expectedErr:  nil,
		},
		{
			name: "returns on-disk data",
			localAsset: &LocalAsset{
				Path: testFilePath,
			},
			expectedData: testFileData,
			expectedErr:  nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r, err := test.localAsset.Open()
			if !errors.Is(err, test.expectedErr) {
				t.Errorf("got %v, want %v", err, test.expectedErr)
			}

			data, err := io.ReadAll(r)
			if err != nil {
				t.Fatalf("cannot read from reader: %v", err)
			}
			if !bytes.Equal(data, test.expectedData) {
				t.Errorf("data=%v != expectedData=%v", data, test.expectedData)
			}
		})
	}
}

func TestLocalAsset_cache(t *testing.T) {
	testDirPath := t.TempDir()

	testFileData := []byte{0x02}
	testFilePath := filepath.Join(testDirPath, "file.jpg")
	err := os.WriteFile(
		testFilePath,
		testFileData,
		0o777,
	)
	if err != nil {
		t.Fatalf("could not make temp file %q: %v", testFilePath, err)
	}
	testFileInfo, err := os.Stat(testFilePath)
	if err != nil {
		t.Fatalf("could not stat file %v: %v", testFileInfo, err)
	}

	asset := &LocalAsset{
		FileInfo: testFileInfo,
		Path:     testFilePath,
	}
	ttl := time.Now().Add(time.Second)
	err = asset.cache(ttl)
	t.Run("error is non-nil", func(t *testing.T) {
		if err != nil {
			t.Fatalf("want err=nil, got err=%v", err)
		}
	})

	t.Run("IsCached == true", func(t *testing.T) {
		if cached := asset.IsCached(); !cached {
			t.Fatalf("want asset.IsCached()=true, got asset.IsCached()=%v", cached)
		}
	})

	t.Run("cachedBytes == testFileData", func(t *testing.T) {
		if !bytes.Equal(asset.cachedBytes, testFileData) {
			t.Fatalf(
				"asset.cachedBytes=%v != testFileData=%v",
				asset.cachedBytes,
				testFileData,
			)
		}
	})

	t.Run("ttl properly set", func(t *testing.T) {
		if expiresAt := asset.expiresAt; expiresAt != ttl {
			t.Fatalf("asset.expiresAt=%v != ttl=%v", asset.expiresAt, ttl)
		}
	})
}

func TestLocalAsset_uncache(t *testing.T) {
	asset := &LocalAsset{
		cachedBytes: []byte{0x01, 0x02},
		expiresAt:   time.Now().Add(time.Second),
	}

	asset.uncache()

	t.Run("cachedBytes cleared", func(t *testing.T) {
		if asset.cachedBytes != nil {
			t.Fatalf("cachedBytes=%v, want cachedBytes=nil", asset.cachedBytes)
		}
	})

	t.Run("expiresAt cleared", func(t *testing.T) {
		zeroTime := time.Time{}
		if asset.expiresAt.Compare(zeroTime) != 0 {
			t.Fatalf(
				"asset.expiresAt=%v, want asset.expiresAt=%v",
				asset.expiresAt,
				zeroTime,
			)
		}
	})

	t.Run("IsCached() == false", func(t *testing.T) {
		if cached := asset.IsCached(); cached != false {
			t.Fatalf("asset.IsCached()=%v, want asset.IsCached()=false", cached)
		}
	})
}

func TestLocalAsset_OpenConcurrent(t *testing.T) {
	var wg sync.WaitGroup

	testDirPath := t.TempDir()

	testFileData := []byte{0x00, 0x10, 0x20}
	testFilePath := filepath.Join(testDirPath, "file.jpg")
	err := os.WriteFile(
		testFilePath,
		testFileData,
		0o777,
	)
	if err != nil {
		t.Fatalf("could not make temp file %q: %v", testFilePath, err)
	}

	asset, err := NewLocalAsset(testFilePath)
	if err != nil {
		t.Fatalf("could not make LocalAsset %v: %v", testFilePath, err)
	}

	nFlips := 1234
	cacheFlipper := func() {
		for range nFlips {
			ttl := time.Now().Add(time.Second)
			err = asset.cache(ttl)
			if err != nil {
				t.Fatalf("cache: %v", err)
			}
			asset.uncache()
		}
	}

	nReaders := 1234
	cacheReader := func() {
		r, err := asset.Open()
		if err != nil {
			t.Fatalf("asset Open(): %v", err)
		}
		data, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("io.Read: %v", err)
		}
		if !bytes.Equal(data, testFileData) {
			t.Fatalf("data=%v != testFileData=%v", data, testFileData)
		}
	}

	wg.Go(cacheFlipper)
	for range nReaders {
		wg.Go(cacheReader)
	}

	wg.Wait()
}

func TestLocalAssetCache_uncacheAssetLocked(t *testing.T) {
	testDirPath := t.TempDir()

	testFileData := []byte{0x00, 0x10, 0x20}
	testFilePath := filepath.Join(testDirPath, "file.jpg")
	err := os.WriteFile(
		testFilePath,
		testFileData,
		0o777,
	)
	if err != nil {
		t.Fatalf("could not make temp file %q: %v", testFilePath, err)
	}
	testFileInfo, err := os.Stat(testFilePath)
	if err != nil {
		t.Fatalf("could not stat file %v: %v", testFileInfo, err)
	}

	tests := []struct {
		name             string
		asset            *LocalAsset
		initialCacheSize int64
		postCacheSize    int64
	}{
		{
			name: "uncache uncached asset",
			asset: &LocalAsset{
				Path:     testFilePath,
				FileInfo: testFileInfo,
			},
			initialCacheSize: 0,
			postCacheSize:    0,
		},
		{
			name: "uncache cached asset",
			asset: &LocalAsset{
				Path:        testFilePath,
				FileInfo:    testFileInfo,
				cachedBytes: []byte{},
			},
			initialCacheSize: testFileInfo.Size(),
			postCacheSize:    0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assetCache := &LocalAssetCache{
				cacheSize: test.initialCacheSize,
			}
			assetCache.uncacheAssetLocked(test.asset)
			if assetCache.cacheSize != test.postCacheSize {
				t.Errorf(
					"cacheSize=%v, want cacheSize=%v",
					assetCache.cacheSize,
					test.postCacheSize,
				)
			}
		})
	}
}

func TestLocalAssetCache_Fetch(t *testing.T) {
	testDirPath := t.TempDir()

	testFileName1 := "abcd.jpg"
	testFileName2 := "bcdf.png"

	testFileData := []byte{0x00, 0x10}

	testFilePath1 := filepath.Join(testDirPath, testFileName1)
	testFilePath2 := filepath.Join(testDirPath, testFileName2)

	err := os.WriteFile(
		testFilePath1,
		testFileData,
		0o777,
	)
	if err != nil {
		t.Fatalf("could not make temp file %q: %v", testFilePath1, err)
	}

	err = os.WriteFile(
		testFilePath2,
		testFileData,
		0o777,
	)
	if err != nil {
		t.Fatalf("could not make temp file %q: %v", testFilePath2, err)
	}

	testFileInfo1, err := os.Stat(testFilePath1)
	if err != nil {
		t.Fatalf("could not stat file %v: %v", testFileInfo1, err)
	}
	testFileInfo2, err := os.Stat(testFilePath2)
	if err != nil {
		t.Fatalf("could not stat file %v: %v", testFileInfo2, err)
	}

	knownAsset := &LocalAsset{
		Path:     testFilePath1,
		FileInfo: testFileInfo2,
	}
	unknownAsset := &LocalAsset{
		Path:     testFilePath2,
		FileInfo: testFileInfo2,
	}

	tests := []struct {
		name      string
		fileName  string
		wantAsset *LocalAsset
	}{
		{
			"fetch known asset",
			testFileName1,
			knownAsset,
		},
		{
			"fetch unknown asset",
			testFileName2,
			unknownAsset,
		},
	}

	cache, err := NewLocalAssetCache(
		testDirPath,
		2,
		4,
		time.Second,
	)
	if err != nil {
		t.Fatalf("could not create cache: %v", err)
	}
	cache.assets[testFileName1] = knownAsset

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			asset, err := cache.Fetch(test.fileName)
			if err != nil {
				t.Errorf("got err=%v, want err=nil", err)
			}

			if asset.Path != test.wantAsset.Path ||
				asset.FileInfo.Name() != test.wantAsset.FileInfo.Name() ||
				asset.FileInfo.Size() != test.wantAsset.FileInfo.Size() {
				t.Errorf("got asset=%v, want %v", asset, test.wantAsset)
			}
		})
	}
}

func TestLocalAssetCache_findEvictionCandidateLocked(t *testing.T) {
	nowTime := time.Now()
	cacheExpiryBase := nowTime.Add(10 * time.Second)

	type assetMeta struct {
		name  string
		asset *LocalAsset
	}
	assets := struct {
		newest assetMeta
		middle assetMeta
		oldest assetMeta
	}{
		assetMeta{
			"abcd.jpg",
			&LocalAsset{
				FileInfo:    &MockFileInfo{size: 2},
				cachedBytes: []byte{0x00, 0x01},
				expiresAt:   cacheExpiryBase,
			},
		},
		assetMeta{
			"bcdf.png",
			&LocalAsset{
				FileInfo:    &MockFileInfo{size: 1},
				cachedBytes: []byte{0x00},
				expiresAt: cacheExpiryBase.Add(
					time.Second * -1,
				),
			},
		},
		assetMeta{
			"cdef.jpg",
			&LocalAsset{
				FileInfo:    &MockFileInfo{size: 1},
				cachedBytes: []byte{0x00},
				expiresAt: cacheExpiryBase.Add(
					time.Second * -20,
				),
			},
		},
	}

	assetsEmpty := make(map[string]*LocalAsset)
	assetsSingle := map[string]*LocalAsset{
		assets.newest.name: assets.newest.asset,
	}
	assetsTwo := map[string]*LocalAsset{
		assets.newest.name: assets.newest.asset,
		assets.middle.name: assets.middle.asset,
	}
	assetsMany := map[string]*LocalAsset{
		assets.newest.name: assets.newest.asset,
		assets.middle.name: assets.middle.asset,
		assets.oldest.name: assets.oldest.asset,
	}

	tests := []struct {
		name          string
		assets        map[string]*LocalAsset
		bytesNeeded   int64
		expectedAsset *LocalAsset
	}{
		{
			"empty assets map returns nil",
			assetsEmpty,
			1,
			nil,
		},
		{
			"assets map of length 1 with expired asset returns asset",
			map[string]*LocalAsset{
				assets.oldest.name: assets.oldest.asset,
			},
			1,
			assets.oldest.asset,
		},
		{
			"assets map of length 1 with unexpired asset returns nil",
			map[string]*LocalAsset{
				assets.newest.name: assets.newest.asset,
			},
			1,
			nil,
		},

		{
			"candidate smaller than bytesNeeded reutrns nil",
			assetsSingle,
			4,
			nil,
		},
		{
			"two candidates not expired returns nil",
			assetsTwo,
			1,
			nil,
		},
		{
			"two candidates both smaller than bytesNeeded returns nil",
			assetsTwo,
			100,
			nil,
		},
		{
			"three candidates picks oldest",
			assetsMany,
			1,
			assetsMany[assets.oldest.name],
		},
	}

	for _, test := range tests {
		cache := &LocalAssetCache{
			assets: test.assets,
		}
		t.Run(test.name, func(t *testing.T) {
			gotVictim := cache.findEvictionCandidateLocked(
				test.bytesNeeded,
				nowTime,
			)
			if gotVictim != test.expectedAsset {
				t.Errorf("got %v, want %v", gotVictim, test.expectedAsset)
			}
		})
	}
}

func TestLocalAssetCache_Cache(t *testing.T) {
	testDirPath := t.TempDir()

	testFileName1 := "abcd.jpg"
	testFileName2 := "bcdf.png"
	testFileData := []byte{0x00, 0x10}

	testFilePath1 := filepath.Join(testDirPath, testFileName1)
	testFilePath2 := filepath.Join(testDirPath, testFileName2)

	err := os.WriteFile(
		testFilePath1,
		testFileData,
		0o777,
	)
	if err != nil {
		t.Fatalf("could not make temp file %q: %v", testFilePath1, err)
	}

	err = os.WriteFile(
		testFilePath2,
		testFileData,
		0o777,
	)
	if err != nil {
		t.Fatalf("could not make temp file %q: %v", testFilePath2, err)
	}

	testFileInfo1, err := os.Stat(testFilePath1)
	if err != nil {
		t.Fatalf("could not stat file %v: %v", testFileInfo1, err)
	}

	tests := []struct {
		name                  string
		fileName              string
		assetMaxSize          int64
		cacheMaxSize          int64
		initialAsset          *LocalAsset
		initialCacheSize      int64
		expectedCacheSize     int64
		expectedCached        bool
		expectedInitialCached bool
		expectedErr           error
	}{
		{
			name:              "cache asset",
			fileName:          testFileName2,
			assetMaxSize:      2,
			cacheMaxSize:      4,
			expectedCacheSize: 2,
			expectedCached:    true,
			expectedErr:       nil,
		},
		{
			name:              "asset too large to cache",
			fileName:          testFileName2,
			assetMaxSize:      1,
			cacheMaxSize:      4,
			expectedCacheSize: 0,
			expectedCached:    false,
			expectedErr:       ErrAssetTooLargeToCache,
		},
		{
			name:         "cache full with no eviction candidate",
			fileName:     testFileName2,
			assetMaxSize: 2,
			cacheMaxSize: 2,
			initialAsset: &LocalAsset{
				FileInfo:    testFileInfo1,
				Path:        testFilePath1,
				cachedBytes: testFileData,
				expiresAt:   time.Now().Add(time.Second),
			},
			initialCacheSize:      2,
			expectedCacheSize:     2,
			expectedCached:        false,
			expectedInitialCached: true,
			expectedErr:           ErrCacheFull,
		},
		{
			name:         "expired asset evicted",
			fileName:     testFileName2,
			assetMaxSize: 2,
			cacheMaxSize: 2,
			initialAsset: &LocalAsset{
				FileInfo:    testFileInfo1,
				Path:        testFilePath1,
				cachedBytes: testFileData,
				expiresAt:   time.Now().Add(-time.Second),
			},
			initialCacheSize:      2,
			expectedCacheSize:     2,
			expectedCached:        true,
			expectedInitialCached: false,
			expectedErr:           nil,
		},
		{
			name:         "already cached asset",
			fileName:     testFileName1,
			assetMaxSize: 2,
			cacheMaxSize: 4,
			initialAsset: &LocalAsset{
				FileInfo:    testFileInfo1,
				Path:        testFilePath1,
				cachedBytes: testFileData,
				expiresAt:   time.Now().Add(time.Second),
			},
			initialCacheSize:      2,
			expectedCacheSize:     2,
			expectedCached:        true,
			expectedInitialCached: true,
			expectedErr:           nil,
		},
		{
			name:              "invalid asset name",
			fileName:          "../abcd.jpg",
			assetMaxSize:      2,
			cacheMaxSize:      4,
			expectedCacheSize: 0,
			expectedCached:    false,
			expectedErr:       ErrInvalidAssetName,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cache, err := NewLocalAssetCache(
				testDirPath,
				test.assetMaxSize,
				test.cacheMaxSize,
				time.Second,
			)
			if err != nil {
				t.Fatalf("could not create cache: %v", err)
			}

			if test.initialAsset != nil {
				cache.assets[testFileName1] = test.initialAsset
				cache.cacheSize = test.initialCacheSize
			}

			err = cache.Cache(test.fileName)
			if !errors.Is(err, test.expectedErr) {
				t.Errorf("got err=%v, want err=%v", err, test.expectedErr)
			}

			if cache.cacheSize != test.expectedCacheSize {
				t.Errorf(
					"cacheSize=%v, want cacheSize=%v",
					cache.cacheSize,
					test.expectedCacheSize,
				)
			}

			cached := false
			asset := cache.assets[test.fileName]
			if asset != nil {
				cached = asset.IsCached()
			}

			if cached != test.expectedCached {
				t.Errorf(
					"asset.IsCached()=%v, want asset.IsCached()=%v",
					cached,
					test.expectedCached,
				)
			}

			if test.expectedCached && !bytes.Equal(asset.cachedBytes, testFileData) {
				t.Errorf(
					"asset.cachedBytes=%v != testFileData=%v",
					asset.cachedBytes,
					testFileData,
				)
			}

			if test.initialAsset != nil {
				cached := test.initialAsset.IsCached()
				if cached != test.expectedInitialCached {
					t.Errorf(
						"initialAsset.IsCached()=%v, want initialAsset.IsCached()=%v",
						cached,
						test.expectedInitialCached,
					)
				}
			}
		})
	}
}

func TestValidateAssetName(t *testing.T) {
	tests := []struct {
		fileName    string
		expectedErr error
	}{
		{
			"abcdefg",
			nil,
		},
		{
			"abcdefg.jpg",
			nil,
		},
		{
			"abcdefg.jpg.png",
			nil,
		},
		{
			"",
			ErrInvalidAssetName,
		},
		{
			".",
			ErrInvalidAssetName,
		},
		{
			"..",
			ErrInvalidAssetName,
		},
		{
			"./.././..///...",
			ErrInvalidAssetName,
		},
		{
			"/etc/passwd",
			ErrInvalidAssetName,
		},
	}

	for _, test := range tests {
		t.Run(test.fileName, func(t *testing.T) {
			err := validateAssetName(test.fileName)
			if !errors.Is(err, test.expectedErr) {
				t.Errorf("got %v, want %v", err, test.expectedErr)
			}
		})
	}
}

func TestLocalAsset_IsCached(t *testing.T) {
	t.Run("is cached and returns true", func(t *testing.T) {
		asset := &LocalAsset{
			cachedBytes: []byte{0x01},
		}
		if cached := asset.IsCached(); cached != true {
			t.Errorf("asset.IsCached=%v, want true", cached)
		}
	})
	t.Run("is not cached and returns false", func(t *testing.T) {
		asset := &LocalAsset{}
		if cached := asset.IsCached(); cached != false {
			t.Errorf("asset.IsCached=%v, want true", cached)
		}
	})
}
