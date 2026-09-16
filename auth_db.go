package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrTokenAlreadyConsumed = errors.New("token already consumed")
	ErrMissingDBPath        = errors.New("missing database path")
)

type TokenStore struct {
	db          *sql.DB
	dbOpTimeout time.Duration
}

type tokenStoreConfig struct {
	DBDriver    string        `koanf:"dbdriver"`
	DBPath      string        `koanf:"dbpath"`
	DBOpTimeout time.Duration `koanf:"advanced.db-op-timeout"`
}

const DefaultDBOpTimeout = 3 * time.Second

const authDBTableInitString = `
CREATE TABLE IF NOT EXISTS consumed_tokens (
	jti TEXT PRIMARY KEY NOT NULL,
	expires_at INTEGER NOT NULL
)
`

const authDBInsertTokenString = `
INSERT INTO consumed_tokens (jti, expires_at)
VALUES (?, ?)
ON CONFLICT(jti) DO NOTHING
`

func NewTokenStore(
	config tokenStoreConfig,
	logger *slog.Logger,
) (*TokenStore, error) {
	if config.DBOpTimeout <= 0 {
		config.DBOpTimeout = DefaultDBOpTimeout
	}
	db, err := NewAuthDB(config)
	if err != nil {
		return nil, err
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &TokenStore{
		db:          db,
		dbOpTimeout: config.DBOpTimeout,
	}, nil
}

func NewAuthDB(config tokenStoreConfig) (*sql.DB, error) {
	if config.DBPath == "" {
		return nil, ErrMissingDBPath
	}
	db, err := sql.Open(config.DBDriver, config.DBPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), config.DBOpTimeout)
	defer cancel()
	_, err = db.ExecContext(
		ctx,
		authDBTableInitString,
	)
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (t *TokenStore) InsertToken(jti string, expiresAt int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), t.dbOpTimeout)
	defer cancel()
	res, err := t.db.ExecContext(
		ctx, authDBInsertTokenString, jti, expiresAt,
	)
	if err != nil {
		return err
	}

	nAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if nAffected != 1 {
		return ErrTokenAlreadyConsumed
	}
	return nil
}

func (t *TokenStore) Close() error {
	return t.db.Close()
}
