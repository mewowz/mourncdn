package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type CDNServer struct {
	mux    *http.ServeMux
	server *http.Server

	logger *slog.Logger
}

type CDNServerConfig struct {
	ServeRoute  string
	UploadRoute string

	Address           string
	ReadTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

func NewCDNServer(
	serveCfg localAssetServerConfig,
	uploadCfg localAssetUploaderConfig,
	cdnCfg CDNServerConfig,
	logger *slog.Logger,
) (*CDNServer, error) {
	if logger == nil {
		logger = slog.Default()
		logger.Info("no logger specified - using default logger")
	}
	serveHandler, err := NewLocalAssetServer(serveCfg, logger)
	if err != nil {
		return nil, err
	}

	uploadHandler, err := NewLocalAssetUploader(uploadCfg)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle(fmt.Sprintf("GET %s/{id...}", cdnCfg.ServeRoute), serveHandler)
	mux.Handle(fmt.Sprintf("POST %s", cdnCfg.UploadRoute), HTTPAuthenticator(uploadHandler))

	server := &http.Server{
		Addr:              cdnCfg.Address,
		Handler:           mux,
		ReadTimeout:       cdnCfg.ReadTimeout,
		ReadHeaderTimeout: cdnCfg.ReadHeaderTimeout,
		WriteTimeout:      cdnCfg.WriteTimeout,
		IdleTimeout:       cdnCfg.IdleTimeout,
	}

	return &CDNServer{
		mux:    mux,
		server: server,
		logger: logger,
	}, nil
}

func (s *CDNServer) ShutdownGracefully(ctx context.Context) error {
	s.logger.Info("gracefully shutting down server")
	err := s.server.Shutdown(ctx)
	if err != nil {
		return err
	}

	s.logger.Info("server successfully shutdown")
	return nil
}

func (s *CDNServer) ForceCloseServer() error {
	err := s.server.Close()
	if err != nil {
		s.logger.Error("force closing server", "err", err)
	}
	return err
}

func (s *CDNServer) Start() error {
	err := s.server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
