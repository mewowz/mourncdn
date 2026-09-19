package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewAuthMiddleHandler(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	publicKeyB64 := base64.StdEncoding.EncodeToString(
		publicKey,
	)
	customLogger := slog.New(slog.DiscardHandler)

	tests := []struct {
		name       string
		config     authMiddleConfig
		tokenStore *TokenStore
		logger     *slog.Logger
		wantErr    bool
	}{
		{
			"all parameters valid",
			authMiddleConfig{publicKeyB64},
			&TokenStore{},
			customLogger,
			false,
		},
		{
			"all parameters valid nil logger defaults",
			authMiddleConfig{publicKeyB64},
			&TokenStore{},
			nil,
			false,
		},
		{
			"bad public key length",
			authMiddleConfig{base64.StdEncoding.EncodeToString(publicKey[:31])},
			&TokenStore{},
			customLogger,
			true,
		},
		{
			"invalid public key encoding",
			authMiddleConfig{"!"},
			&TokenStore{},
			customLogger,
			true,
		},
		{
			"nil token store",
			authMiddleConfig{publicKeyB64},
			nil,
			customLogger,
			true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, err := NewAuthMiddleHandler(
				test.config,
				test.tokenStore,
				test.logger,
			)
			if (err != nil) != test.wantErr {
				t.Fatalf("got err=%v, wantErr=%v", err, test.wantErr)
			}
			if test.wantErr {
				if handler != nil {
					t.Fatalf("got handler=%v, want nil", handler)
				}
				return
			}
			if handler == nil {
				t.Fatalf("got handler=nil, want non-nil")
			}

			var l *slog.Logger
			if test.logger != nil {
				l = test.logger
			} else {
				l = slog.Default()
			}

			if handler.logger != l {
				t.Fatalf("got logger=%v, want %v", handler.logger, l)
			}

			gotPublicKey := base64.StdEncoding.EncodeToString(handler.publicKey)
			if gotPublicKey != test.config.PublicKey {
				t.Fatalf("got publicKey=%v, want %v", gotPublicKey, test.config.PublicKey)
			}

			if handler.tokenStore != test.tokenStore {
				t.Fatalf("got tokenStore=%v, want %v", handler.tokenStore, test.tokenStore)
			}
		})
	}
}

func TestParseAuthorizationHeader(t *testing.T) {
	tests := []struct {
		name        string
		header      string
		expected    MournCapability
		expectedErr error
	}{
		{
			"proper header",
			mournCapabilityStringPrefix + "1234" + "." + "abcd" + "." + "wxyz",
			MournCapability{
				"1234",
				"abcd",
				"wxyz",
			},
			nil,
		},
		{
			"missing prefix",
			"1234" + "." + "abcd" + "." + "wxyz",
			MournCapability{},
			ErrCapabilityPrefixNotFound,
		},

		{
			"wrong seperator",
			mournCapabilityStringPrefix + "1234" + "," + "abcd" + "," + "wxyz",
			MournCapability{},
			ErrInvalidCapabilityPartsCount,
		},
		{
			"only one separator",
			mournCapabilityStringPrefix + "1234" + "abcd" + "." + "wxyz",
			MournCapability{},
			ErrInvalidCapabilityPartsCount,
		},
		{
			"extra separator",
			mournCapabilityStringPrefix + "1234" + ".." + "abcd" + "." + "wxyz",
			MournCapability{},
			ErrInvalidCapabilityPartsCount,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseAuthorizationHeader(test.header)
			if !errors.Is(err, test.expectedErr) {
				t.Fatalf("got err=%v, want %v", err, test.expectedErr)
			}
			if got != test.expected {
				t.Fatalf("got capability=%+v, want %+v", got, test.expected)
			}
		})
	}
}

func TestVerifySignature(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	version := mournTokenFormatVersionString
	payload := "abcd"
	message := []byte(mournPayloadPrefix + payload)
	signature := base64.RawURLEncoding.EncodeToString(
		ed25519.Sign(privateKey, message),
	)
	tests := []struct {
		name        string
		capability  MournCapability
		expectedErr error
	}{
		{
			"good capability; valid signature",
			MournCapability{
				version,
				payload,
				signature,
			},
			nil,
		},
		{
			"bad version",
			MournCapability{
				"badversion",
				payload,
				signature,
			},
			ErrInvalidTokenVersion,
		},
		{
			"bad signature",
			MournCapability{
				version,
				payload,
				"abcd",
			},
			ErrInvalidUploadSignature,
		},
		{
			"bad payload",
			MournCapability{
				version,
				"1234",
				signature,
			},
			ErrInvalidUploadSignature,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.capability.verifySignature(publicKey)
			if !errors.Is(err, test.expectedErr) {
				t.Fatalf("got err=%v, want %v", err, test.expectedErr)
			}
		})
	}

	t.Run("bad public key length", func(t *testing.T) {
		capability := MournCapability{
			version,
			payload,
			signature,
		}
		err := capability.verifySignature(publicKey[:len(publicKey)-1])
		if !errors.Is(err, ErrInvalidKeySize) {
			t.Fatalf("got err=%v, want %v", err, ErrInvalidKeySize)
		}
	})
}

func validUploadClaims() UploadClaims {
	now := time.Now().Unix()
	return UploadClaims{
		NotBefore: now - 1,
		ExpiresAt: now + 60,
		Issuer:    mournRequiredIssuerString,
		Audience:  mournRequiredAudienceString,
		Scope:     mournRequiredScopeString,
		Subject:   "not empty",
		TokenID:   "token1",
		IssuedAt:  now,
		MaxBytes:  123,
	}
}

func TestNewUploadClaimsFromCapability(t *testing.T) {
	tests := []struct {
		name     string
		modifier func(*UploadClaims)
		wantErr  bool
	}{
		{
			"valid claims",
			func(*UploadClaims) {},
			false,
		},
		{
			"invalid issuer",
			func(c *UploadClaims) { c.Issuer = "other-api" },
			true,
		},
		{
			"missing issuer",
			func(c *UploadClaims) { c.Issuer = "" },
			true,
		},
		{
			"invalid audience",
			func(c *UploadClaims) { c.Audience = "other-cdn" },
			true,
		},
		{
			"missing audience",
			func(c *UploadClaims) { c.Audience = "" },
			true,
		},
		{
			"invalid scope",
			func(c *UploadClaims) { c.Scope = "download" },
			true,
		},
		{
			"missing scope",
			func(c *UploadClaims) { c.Scope = "" },
			true,
		},
		{
			"empty subject",
			func(c *UploadClaims) { c.Subject = "" },
			true,
		},
		{
			"empty token ID",
			func(c *UploadClaims) { c.TokenID = "" },
			true,
		},
		{
			"not yet valid",
			func(c *UploadClaims) { c.NotBefore = c.IssuedAt + 30 },
			true,
		},
		{
			"valid from issue time",
			func(c *UploadClaims) { c.NotBefore = c.IssuedAt },
			false,
		},
		{
			"expired token",
			func(c *UploadClaims) {
				c.IssuedAt -= 120
				c.NotBefore = c.IssuedAt
				c.ExpiresAt = c.IssuedAt + 60
			},
			true,
		},
		{
			"expiration at current time is rejected",
			func(c *UploadClaims) {
				c.ExpiresAt = c.IssuedAt
				c.IssuedAt -= 60
				c.NotBefore = c.IssuedAt
			},
			true,
		},
		{
			"120 second lifetime is accepted",
			func(c *UploadClaims) { c.IssuedAt = c.ExpiresAt - 120 },
			false,
		},
		{
			"121 second lifetime is rejected",
			func(c *UploadClaims) { c.IssuedAt = c.ExpiresAt - 121 },
			true,
		},
		{
			"zero lifetime",
			func(c *UploadClaims) { c.IssuedAt = c.ExpiresAt },
			true,
		},
		{
			"negative lifetime",
			func(c *UploadClaims) { c.IssuedAt = c.ExpiresAt + 1 },
			true,
		},
		{
			"zero byte limit",
			func(c *UploadClaims) { c.MaxBytes = 0 },
			true,
		},
		{
			"negative byte limit",
			func(c *UploadClaims) { c.MaxBytes = -1 },
			true,
		},
		{
			"one byte limit is accepted",
			func(c *UploadClaims) { c.MaxBytes = 1 },
			false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			claims := validUploadClaims()
			test.modifier(&claims)

			payload, err := json.Marshal(claims)
			if err != nil {
				t.Fatal(err)
			}

			capability := MournCapability{
				payload: base64.RawURLEncoding.EncodeToString(payload),
			}

			got, err := NewUploadClaimsFromCapability(capability)

			if (err != nil) != test.wantErr {
				t.Fatalf("got err=%v, wantErr=%v", err, test.wantErr)
			}
			if test.wantErr && got != (UploadClaims{}) {
				t.Fatalf("got claims=%+v, want zero value on error", got)
			}

			if !test.wantErr && got != claims {
				t.Fatalf("got claims=%+v, want %+v", got, claims)
			}
		})
	}

	t.Run("malformed payloads", func(t *testing.T) {
		tests := []struct {
			name              string
			payload           string
			expectedErrPrefix string
		}{
			{"empty payload", "", "unmarshal payload:"},
			{"invalid base64", "!", "decode payload:"},
			{"padded base64", base64.URLEncoding.EncodeToString([]byte("{}")), "decode payload:"},
			{"invalid JSON", base64.RawURLEncoding.EncodeToString([]byte("{")), "unmarshal payload:"},
			{"wrong JSON shape", base64.RawURLEncoding.EncodeToString([]byte("[]")), "unmarshal payload:"},
			{"wrong claim type", base64.RawURLEncoding.EncodeToString([]byte(`{"max_bytes":"123"}`)), "unmarshal payload:"},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				got, err := NewUploadClaimsFromCapability(MournCapability{
					payload: test.payload,
				})
				if err == nil {
					t.Fatal("got nil, want non-nil error")
				}
				if !strings.HasPrefix(err.Error(), test.expectedErrPrefix) {
					t.Fatalf("got err=%v, want prefix %q", err, test.expectedErrPrefix)
				}
				if got != (UploadClaims{}) {
					t.Fatalf("got claims=%+v, want zero value on error", got)
				}
			})
		}
	})
}

func TestVerifyContentLengthAgainstUploadLimits(t *testing.T) {
	tests := []struct {
		name          string
		contentLength int64
		limit         int64
		expectedErr   error
	}{
		{
			"content length within limit",
			10,
			100,
			nil,
		},
		{
			"content length exactly limit",
			100,
			100,
			nil,
		},
		{
			"content length less than 0",
			-10,
			100,
			ErrInvalidUploadLength,
		},
		{
			"content length above limit",
			1000,
			100,
			ErrUploadTooLarge,
		},
		{
			"zero length rejected",
			0,
			100,
			ErrInvalidUploadLength,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := verifyContentLengthAgainstUploadLimits(
				test.contentLength,
				test.limit,
			)
			if !errors.Is(err, test.expectedErr) {
				t.Fatalf("got err=%v, want %v", err, test.expectedErr)
			}
		})
	}
}

func newHTTPAuthenticatorTestSetup(
	t *testing.T,
	publicKeyB64, authHeader, contentType string,
) (*TokenStore, *AuthMiddleHandler, *http.Request, *httptest.ResponseRecorder) {
	t.Helper()

	logger := slog.New(slog.DiscardHandler)
	tokenStore, err := NewTokenStore(
		tokenStoreConfig{
			"sqlite",
			t.TempDir() + "/ts.db",
			3 * time.Second,
			60 * time.Second,
		},
		logger,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := tokenStore.Close(); err != nil {
			t.Errorf("close token store: %v", err)
		}
	})

	authHandler, err := NewAuthMiddleHandler(
		authMiddleConfig{publicKeyB64},
		tokenStore,
		logger,
	)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/upload", strings.NewReader("foo"))
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Content-Type", contentType)

	return tokenStore, authHandler, req, httptest.NewRecorder()
}

func TestHTTPAuthenticator(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	publicKeyB64 := base64.StdEncoding.EncodeToString(
		publicKey,
	)

	prefix := mournCapabilityStringPrefix
	version := mournTokenFormatVersionString

	jsonPayloadBytes, err := json.Marshal(validUploadClaims())
	if err != nil {
		t.Fatal(err)
	}
	jsonPayloadB64 := base64.RawURLEncoding.EncodeToString(
		jsonPayloadBytes,
	)

	message := []byte(mournPayloadPrefix + jsonPayloadB64)
	signature := base64.RawURLEncoding.EncodeToString(
		ed25519.Sign(privateKey, message),
	)

	badClaims := validUploadClaims()
	badClaims.MaxBytes = -123
	jsonBadClaims, err := json.Marshal(badClaims)
	if err != nil {
		t.Fatal(err)
	}
	jsonBadClaimsB64 := base64.RawURLEncoding.EncodeToString(
		jsonBadClaims,
	)
	message = []byte(mournPayloadPrefix + jsonBadClaimsB64)
	badClaimsSignature := base64.RawURLEncoding.EncodeToString(
		ed25519.Sign(privateKey, message),
	)

	uploadCfg := localAssetUploaderConfig{MaxAssetUploadSize: 12345}

	authHeaderTests := []struct {
		name               string
		authHeader         string
		contentType        string
		expectedStatusCode int
	}{
		{
			"valid input and reaches next",
			prefix + version + "." + jsonPayloadB64 + "." + signature,
			"application/octet-stream",
			http.StatusCreated,
		},
		{
			"missing authorization header value",
			"",
			"application/octet-stream",
			http.StatusUnauthorized,
		},
		{
			"invalid content type",
			prefix + version + "." + jsonPayloadB64 + "." + signature,
			"text/plain",
			http.StatusUnsupportedMediaType,
		},

		{
			"missing prefix",
			version + "." + jsonPayloadB64 + "." + signature,
			"application/octet-stream",
			http.StatusUnauthorized,
		},
		{
			"incorrect capability version",
			prefix + "wrongversion" + "." + jsonPayloadB64 + "." + signature,
			"application/octet-stream",
			http.StatusUnauthorized,
		},
		{
			"incorrect separator",
			prefix + version + "," + jsonPayloadB64 + "," + signature,
			"application/octet-stream",
			http.StatusUnauthorized,
		},
		{
			"no separator",
			prefix + version + "" + jsonPayloadB64 + "" + signature,
			"application/octet-stream",
			http.StatusUnauthorized,
		},
		{
			"only one separator",
			prefix + version + "." + jsonPayloadB64 + "" + signature,
			"application/octet-stream",
			http.StatusUnauthorized,
		},
		{
			"missing signature",
			prefix + version + "." + jsonPayloadB64 + ".",
			"application/octet-stream",
			http.StatusUnauthorized,
		},
		{
			"invalid signature length",
			prefix + version + "." + jsonPayloadB64 + "." + signature[:len(signature)-1],
			"application/octet-stream",
			http.StatusUnauthorized,
		},
		{
			"invalid signature",
			prefix + version + "." + jsonPayloadB64 + "." + "abcd",
			"application/octet-stream",
			http.StatusUnauthorized,
		},
		{
			"valid signature but invalid claims",
			prefix + version + "." + jsonBadClaimsB64 + "." + badClaimsSignature,
			"application/octet-stream",
			http.StatusUnauthorized,
		},
	}

	for _, test := range authHeaderTests {
		t.Run(test.name, func(t *testing.T) {
			_, authHandler, req, rec := newHTTPAuthenticatorTestSetup(
				t, publicKeyB64, test.authHeader, test.contentType,
			)

			nextCalled := false
			nextFunc201 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusCreated)
			})

			handler := authHandler.HTTPAuthenticator(
				nextFunc201,
				uploadCfg,
			)
			handler.ServeHTTP(rec, req)

			resp := rec.Result()

			if resp.StatusCode != test.expectedStatusCode {
				t.Fatalf("got StatusCode=%v, want %v", resp.StatusCode, test.expectedStatusCode)
			}
			wantNext := test.expectedStatusCode == http.StatusCreated
			if nextCalled != wantNext {
				t.Fatalf("got nextCalled=%v, want %v", nextCalled, wantNext)
			}
		})
	}

	t.Run("test failing token store", func(t *testing.T) {
		expectedStatusCode := http.StatusServiceUnavailable

		tokenStore, authHandler, req, rec := newHTTPAuthenticatorTestSetup(
			t, publicKeyB64,
			prefix+version+"."+jsonPayloadB64+"."+signature,
			"application/octet-stream",
		)
		if err := tokenStore.Close(); err != nil {
			t.Fatal(err)
		}

		nextCalled := false
		nextFunc201 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusCreated)
		})

		handler := authHandler.HTTPAuthenticator(
			nextFunc201,
			uploadCfg,
		)
		handler.ServeHTTP(rec, req)

		resp := rec.Result()

		if resp.StatusCode != expectedStatusCode {
			t.Fatalf("got StatusCode=%v, want %v", resp.StatusCode, expectedStatusCode)
		}
		if nextCalled {
			t.Fatal("next was called with a failing token store")
		}
	})

	lengthTests := []struct {
		contentLength      int
		serverLimit        int
		expectedStatusCode int
	}{
		{
			10,
			100,
			http.StatusCreated,
		},
		{
			-10,
			100,
			http.StatusBadRequest,
		},
		{
			0,
			100,
			http.StatusBadRequest,
		},
		{
			1000,
			100,
			http.StatusRequestEntityTooLarge,
		},
		{
			-1,
			100,
			http.StatusBadRequest,
		},
	}

	for _, test := range lengthTests {
		_, authHandler, req, rec := newHTTPAuthenticatorTestSetup(
			t, publicKeyB64,
			prefix+version+"."+jsonPayloadB64+"."+signature,
			"application/octet-stream",
		)

		req.ContentLength = int64(test.contentLength)

		nextCalled := false
		nextFunc201 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusCreated)
		})

		handler := authHandler.HTTPAuthenticator(
			nextFunc201,
			localAssetUploaderConfig{
				MaxAssetUploadSize: int64(test.serverLimit),
			},
		)
		handler.ServeHTTP(rec, req)

		resp := rec.Result()

		if resp.StatusCode != test.expectedStatusCode {
			t.Fatalf("got StatusCode=%v, want %v", resp.StatusCode, test.expectedStatusCode)
		}

		wantNext := test.expectedStatusCode == http.StatusCreated
		if nextCalled != wantNext {
			if !wantNext {
				t.Fatal("next was called with failing content length")
			} else {
				t.Fatal("next was not called on valid content length")
			}
		}
	}

	t.Run("token reuse fails", func(t *testing.T) {
		expectedStatusCode := http.StatusCreated

		_, authHandler, req, rec := newHTTPAuthenticatorTestSetup(
			t, publicKeyB64,
			prefix+version+"."+jsonPayloadB64+"."+signature,
			"application/octet-stream",
		)

		nextCalledCt := 0
		nextFunc201 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalledCt++
			w.WriteHeader(http.StatusCreated)
		})

		handler := authHandler.HTTPAuthenticator(
			nextFunc201,
			uploadCfg,
		)
		handler.ServeHTTP(rec, req)

		resp := rec.Result()

		if resp.StatusCode != expectedStatusCode {
			t.Fatalf("got StatusCode=%v, want %v", resp.StatusCode, expectedStatusCode)
		}

		expectedStatusCode = http.StatusUnauthorized

		req = httptest.NewRequest("POST", "/upload", strings.NewReader("foo"))
		req.Header.Set("Authorization", prefix+version+"."+jsonPayloadB64+"."+signature)
		req.Header.Set("Content-Type", "application/octet-stream")

		rec = httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		resp = rec.Result()

		if resp.StatusCode != expectedStatusCode {
			t.Fatalf("got StatusCode=%v, want %v", resp.StatusCode, expectedStatusCode)
		}

		if nextCalledCt != 1 {
			t.Fatalf("got nextCalledCt=%d, want 1", nextCalledCt)
		}
	})

	t.Run("middleware wraps claims limit", func(t *testing.T) {
		_, authHandler, req, rec := newHTTPAuthenticatorTestSetup(
			t, publicKeyB64,
			prefix+version+"."+jsonPayloadB64+"."+signature,
			"application/octet-stream",
		)

		req.Body = io.NopCloser(strings.NewReader(strings.Repeat("a", 124)))
		req.ContentLength = 3

		nextCalled := false
		nextFuncCheckLimits := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			_, err := io.ReadAll(r.Body)

			var sizeErr *http.MaxBytesError
			if !errors.As(err, &sizeErr) {
				t.Fatalf("got err=%v, want *http.MaxBytesError", err)
			}
		})

		handler := authHandler.HTTPAuthenticator(
			nextFuncCheckLimits,
			uploadCfg,
		)
		handler.ServeHTTP(rec, req)

		if !nextCalled {
			t.Fatalf("next was not called; body limit was not tested")
		}
	})
}
