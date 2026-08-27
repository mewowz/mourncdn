package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"
)

var (
	ErrInvalidWriteBufSize = errors.New("WriteBufSize must be >= 1")
	ErrInvalidWriteWindow  = errors.New("WriteWindow must be >= 1")
)

type LocalAssetServer struct {
	cache *LocalAssetCache

	writeBufSize int
	writeWindow  time.Duration

	logger *slog.Logger
}

type localAssetServerConfig struct {
	// LocalAssetCache
	AssetDir     string
	AssetMaxSize int64
	CacheMaxSize int64
	TTL          time.Duration

	// LocalAssetServer
	WriteBufSize int
	WriteWindow  time.Duration
}

func NewLocalAssetServer(
	cfg localAssetServerConfig,
	logger *slog.Logger,
) (*LocalAssetServer, error) {
	cache, err := NewLocalAssetCache(
		cfg.AssetDir,
		cfg.AssetMaxSize,
		cfg.CacheMaxSize,
		cfg.TTL,
	)
	if err != nil {
		return nil, fmt.Errorf("local asset server: %w", err)
	}

	if logger == nil {
		logger = slog.Default()
	}

	if cfg.WriteBufSize <= 0 {
		return nil, fmt.Errorf(
			"local asset server WriteBufSize=%d: %w",
			cfg.WriteBufSize,
			ErrInvalidWriteBufSize,
		)
	}

	if cfg.WriteWindow <= 0 {
		return nil, fmt.Errorf(
			"local asset server WriteWindow=%d: %w",
			cfg.WriteWindow,
			ErrInvalidWriteWindow,
		)
	}

	return &LocalAssetServer{
		cache:        cache,
		logger:       logger,
		writeBufSize: cfg.WriteBufSize,
		writeWindow:  cfg.WriteWindow,
	}, nil
}

func (s *LocalAssetServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// We're expecting the handler to be configured for routes like
	// /assets/{id...} or /{id...} or /file/{id...}
	// where "id" points to a filename relative to the source dir.
	// For instance, for the route "/assets/{id...}" and AssetDir = ./data/assets/:
	// http://foo.com/assets/abcdef.jpg -> ./data/assets/abcdef.jpg

	assetID := r.PathValue("id")
	asset, err := s.cacheAndFetch(assetID)
	if err != nil {
		s.handleCacheAndFetchErr(err, w)
		return
	}

	err = s.writeAssetToClient(asset, w, r)
	if err != nil {
		return
	}
}

func (s *LocalAssetServer) handleCacheAndFetchErr(
	err error,
	w http.ResponseWriter,
) {
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case errors.Is(err, ErrInvalidAssetName):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, ErrNotAFile):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, os.ErrNotExist):
		http.Error(w, "not found", http.StatusNotFound)
	default:
		http.Error(w, "server error", http.StatusInternalServerError)
	}
}

func (s *LocalAssetServer) cacheAndFetch(
	fileName string,
) (*LocalAsset, error) {
	recoverableErrors := []error{
		ErrCacheFull,
		ErrAssetTooLargeToCache,
	}

	err := s.cache.Cache(fileName)
	if err != nil {
		for _, rErr := range recoverableErrors {
			if errors.Is(err, rErr) {
				// Really want to figure out a right way to
				// log these without cluttering my methods with logs
				// everywhere.
				// s.logErrorWithRequest(err, r)
				err = nil
				break
			}
		}
		if err != nil {
			return nil, err
		}
	}

	asset, err := s.cache.Fetch(fileName)
	if err != nil {
		return nil, err
	}

	return asset, nil
}

func (s *LocalAssetServer) writeAssetToClient(
	asset *LocalAsset,
	w http.ResponseWriter,
	r *http.Request,
) error {
	assetReader, err := asset.Open()
	if err != nil {
		return err
	}
	defer assetReader.Close()

	rc := http.NewResponseController(w)

	buf := make([]byte, s.writeBufSize)
	for {
		select {
		case <-r.Context().Done():
			return r.Context().Err()
		default:
		}
		n, err := assetReader.Read(buf)

		_ = rc.SetWriteDeadline(time.Now().Add(s.writeWindow))
		if n > 0 {
			_, writeErr := w.Write(buf[:n])
			if writeErr != nil {
				return writeErr
			}
		}

		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return err
		}

	}
}
