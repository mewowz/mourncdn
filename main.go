package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const DefaultConfigFilePath = "./config.yml"

func main() {
	logger := slog.Default()
	if err := run(logger); err != nil {
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	config, err := LoadConfigFile(DefaultConfigFilePath)
	if err != nil {
		logger.Error("config", "err", err)
		return err
	}

	tokenStore, err := NewTokenStore(
		config.TokenStoreCfg,
		logger,
	)
	if err != nil {
		logger.Error("token store", "err", err)
		return err
	}
	defer func() {
		if err = tokenStore.Close(); err != nil {
			logger.Error("token store", "err", err)
		}
	}()

	server, err := NewCDNServer(
		config.ServeCfg,
		config.UploadCfg,
		config.HTTPCfg,
		config.AuthCfg,
		tokenStore,
		logger,
	)
	if err != nil {
		logger.Error("cdn server", "err", err)
		return err
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	logger.Info("starting server", "address", config.HTTPCfg.Address)
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()

	select {
	case err := <-errCh:
		if err != nil {
			logger.Error("server", "err", err)
		}
		return err
	case <-c:
		logger.Info("shutting down server gracefully")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := server.ShutdownGracefully(ctx); err != nil {
		logger.Error("graceful shutdown failed", "err", err)

		if err := server.ForceCloseServer(); err != nil {
			logger.Error("force close failed", "err", err)
		}
	}

	<-errCh
	logger.Info("bye")

	return nil
}
