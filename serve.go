package main

import (
	"context"
	"errors"
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
	AssetDir     string        `koanf:"asset-dir"`
	AssetMaxSize int64         `koanf:"max-cacheable-size"`
	CacheMaxSize int64         `koanf:"cache-size"`
	TTL          time.Duration `koanf:"asset-ttl"`

	// LocalAssetServer
	WriteBufSize int           `koanf:"advanced.write-buffer-size"`
	WriteWindow  time.Duration `koanf:"advanced.write-window"`
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
		return nil, err
	}

	if logger == nil {
		logger = slog.Default()
	}

	if cfg.WriteBufSize <= 0 {
		return nil, ErrInvalidWriteBufSize
	}

	if cfg.WriteWindow <= 0 {
		return nil, ErrInvalidWriteWindow
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
		s.handleWriteAssetToClientError(r, err)
		return
	}
}

func (s *LocalAssetServer) handleWriteAssetToClientError(
	r *http.Request,
	err error,
) {
	writeErrorLogger := s.logger.With(
		"err", err.Error(),
		"method", r.Method,
		"remote", r.RemoteAddr,
		"url", r.URL.String(),
	)

	var pe *os.PathError
	if errors.As(err, &pe) {
		writeErrorLogger.Error("path error")
		return
	}

	if errors.Is(err, context.DeadlineExceeded) {
		writeErrorLogger.Warn(
			"deadline exceeded",
			"note", "request deadline exceeded",
		)
		return
	}

	if errors.Is(err, context.Canceled) {
		writeErrorLogger.Debug("request context cancelled")
		return
	}

	writeErrorLogger.Error("failed to serve asset")
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

		deadlineErr := rc.SetWriteDeadline(time.Now().Add(s.writeWindow))
		if deadlineErr != nil {
			return deadlineErr
		}
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
