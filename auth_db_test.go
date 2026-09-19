package main

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestNewAuthDB(t *testing.T) {
	tempDirPath := t.TempDir()

	tests := []struct {
		name        string
		config      tokenStoreConfig
		expectedErr error
	}{
		{
			"no errors; good config",
			tokenStoreConfig{
				"sqlite",
				filepath.Join(tempDirPath, "ts.db"),
				3 * time.Second,
			},
			nil,
		},
		{
			"missing db path",
			tokenStoreConfig{
				"sqlite",
				"",
				3 * time.Second,
			},
			ErrMissingDBPath,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, err := NewAuthDB(test.config)
			if !errors.Is(test.expectedErr, err) {
				t.Fatalf("got %v, want %v", err, test.expectedErr)
			}
			if db != nil {
				db.Close()
			}
		})
	}
}

func TestNewTokenStore(t *testing.T) {
	tempDirPath := t.TempDir()
	logger := slog.New(slog.DiscardHandler)

	tests := []struct {
		name            string
		config          tokenStoreConfig
		logger          *slog.Logger
		expectedTimeout time.Duration
		expectedErr     error
	}{
		{
			"no errors; good config",
			tokenStoreConfig{"sqlite", filepath.Join(tempDirPath, "good.db"), time.Second},
			logger,
			time.Second,
			nil,
		},
		{
			"zero timeout uses default",
			tokenStoreConfig{"sqlite", filepath.Join(tempDirPath, "zero.db"), 0},
			logger,
			DefaultDBOpTimeout,
			nil,
		},
		{
			"negative timeout uses default",
			tokenStoreConfig{"sqlite", filepath.Join(tempDirPath, "negative.db"), -time.Second},
			logger,
			DefaultDBOpTimeout,
			nil,
		},
		{
			"nil logger",
			tokenStoreConfig{"sqlite", filepath.Join(tempDirPath, "nil-logger.db"), time.Second},
			nil,
			time.Second,
			nil,
		},
		{
			"missing db path propagates error",
			tokenStoreConfig{"sqlite", "", time.Second},
			logger,
			time.Second,
			ErrMissingDBPath,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NewTokenStore(test.config, test.logger)
			if got != nil {
				t.Cleanup(func() {
					if err := got.Close(); err != nil {
						t.Errorf("close token store: %v", err)
					}
				})
			}
			if !errors.Is(err, test.expectedErr) {
				t.Fatalf("got %v, want %v", err, test.expectedErr)
			}
			if test.expectedErr != nil {
				if got != nil {
					t.Fatalf("got non-nil TokenStore, want nil")
				}
				return
			}

			if got == nil {
				t.Fatalf("got nil TokenStore, want non-nil")
			}
			if got.db == nil {
				t.Fatalf("got nil db, want non-nil")
			}
			// TODO: compare got.dbOpTimeout with test.expectedTimeout.
			if got.dbOpTimeout != test.expectedTimeout {
				t.Fatalf("got dbOpTimeout=%v, want %v", got.dbOpTimeout, test.expectedTimeout)
			}
		})
	}
}

func TestTokenStore_InsertToken(t *testing.T) {
	timeNow := time.Now().Unix()
	tests := []struct {
		name              string
		jti               string
		expiresAt         int64
		existingJTI       string
		existingExpiresAt int64
		expectedErr       error
	}{
		{
			"unused token is consumed",
			"token-1", 200 + timeNow,
			"", 0 + timeNow,
			nil,
		},
		{
			"consumed token is rejected",
			"token-1", 300 + timeNow,
			"token-1", 200 + timeNow,
			ErrTokenAlreadyConsumed,
		},
		{
			"different token is accepted",
			"token-2", 300 + timeNow,
			"token-1", 200 + timeNow,
			nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, err := NewTokenStore(
				tokenStoreConfig{
					"sqlite",
					filepath.Join(t.TempDir(), "ts.db"),
					3 * time.Second,
				},
				slog.New(slog.DiscardHandler),
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := store.Close(); err != nil {
					t.Errorf("close token store: %v", err)
				}
			})

			if test.existingJTI != "" {
				if err := store.InsertToken(test.existingJTI, test.existingExpiresAt); err != nil {
					t.Fatalf("seed token: %v", err)
				}
			}

			err = store.InsertToken(test.jti, test.expiresAt)
			if !errors.Is(err, test.expectedErr) {
				t.Fatalf("got %v, want %v", err, test.expectedErr)
			}

			// TODO: query the stored row and assert its ID and expiration.
			var gotJTI string
			var gotExpiresAt int64
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			err = store.db.QueryRowContext(
				ctx, "SELECT jti, expires_at FROM consumed_tokens WHERE jti = ?", test.jti,
			).Scan(&gotJTI, &gotExpiresAt)
			if err != nil {
				t.Fatal(err)
			}

			if test.jti != gotJTI {
				t.Fatalf("got jti=%v, want %v", gotJTI, test.jti)
			}

			wantExpiresAt := test.expiresAt
			if errors.Is(test.expectedErr, ErrTokenAlreadyConsumed) {
				wantExpiresAt = test.existingExpiresAt
			}
			if gotExpiresAt != wantExpiresAt {
				t.Fatalf("got expiresAt=%v, want %v", gotExpiresAt, wantExpiresAt)
			}
		})
	}

	t.Run("expired token is rejected", func(t *testing.T) {
		store, err := NewTokenStore(
			tokenStoreConfig{
				"sqlite",
				filepath.Join(t.TempDir(), "ts.db"),
				3 * time.Second,
			},
			slog.New(slog.DiscardHandler),
		)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := store.Close(); err != nil {
				t.Errorf("close token store: %v", err)
			}
		})

		err = store.InsertToken("token-1", 0)
		if !errors.Is(err, ErrTokenAlreadyConsumed) {
			t.Fatal(err)
		}
	})
}

func TestTokenStore_InsertTokenConcurrent(t *testing.T) {
	store, err := NewTokenStore(
		tokenStoreConfig{
			"sqlite",
			filepath.Join(t.TempDir(), "ts.db"),
			3 * time.Second,
		},
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close token store: %v", err)
		}
	})

	jti := "token1"
	expiresAt := int64(100 + time.Now().Unix())
	mu := sync.Mutex{}
	var results []error
	nGoroutines := 100

	var wg sync.WaitGroup
	start := make(chan struct{})

	for range nGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			err := store.InsertToken(jti, expiresAt)
			mu.Lock()
			results = append(results, err)
			mu.Unlock()
		}()
	}

	close(start)
	wg.Wait()

	errCount := 0
	for _, e := range results {
		if e != nil && !errors.Is(e, ErrTokenAlreadyConsumed) {
			t.Fatal(e)
		}
		if e == ErrTokenAlreadyConsumed {
			errCount++
		}
	}
	if nGoroutines-errCount != 1 {
		t.Fatalf("got %v failed inserts, want %v", errCount, nGoroutines-1)
	}
}

func TestTokenStore_Close(t *testing.T) {
	store, err := NewTokenStore(
		tokenStoreConfig{
			"sqlite",
			filepath.Join(t.TempDir(), "ts.db"),
			3 * time.Second,
		},
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close token store: %v", err)
		}
	})

	err = store.Close()
	if err != nil {
		t.Fatalf("got err=%v, want %v", err, nil)
	}
	err = store.InsertToken("token1", 100)
	if errors.Is(err, ErrTokenAlreadyConsumed) {
		t.Fatalf("store did not close")
	}
}
