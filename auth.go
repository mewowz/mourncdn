package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	mournCapabilityPartsCount     = 3
	mournCapabilitySep            = "."
	mournCapabilityStringPrefix   = "MournCapability "
	mournTokenFormatVersionString = "muc1"

	mournPayloadPrefix = "mourn/profile-upload/v1\n"

	mournRequiredIssuerString   = "mourn-api"
	mournRequiredAudienceString = "mourncdn"
	mournRequiredScopeString    = "upload"
)

var (
	ErrCapabilityPrefixNotFound = errors.New(
		"could not find " + mournCapabilityStringPrefix,
	)
	ErrInvalidCapabilityPartsCount = errors.New(
		"need exactly " + strconv.Itoa(mournCapabilityPartsCount) + " capability parts",
	)
	ErrInvalidTokenVersion    = errors.New("invalid token capability version")
	ErrInvalidUploadSignature = errors.New("invalid upload signature")
	ErrCapabilityExpired      = errors.New("capability expired")
	ErrUploadTooLarge         = errors.New("upload too large")
	ErrInvalidUploadLength    = errors.New("invalid content length")
	ErrInvalidKeySize         = errors.New("invalid size for ed25519 public key")
)

type AuthMiddleHandler struct {
	publicKey []byte

	tokenStore *TokenStore

	logger *slog.Logger
}

type authMiddleConfig struct {
	PublicKey string `koanf:"public-key"`
}

type UploadClaims struct {
	Issuer    string `json:"iss"`
	Audience  string `json:"aud"`
	Subject   string `json:"sub"`
	Scope     string `json:"scope"`
	MediaType string `json:"media_type"`
	TokenID   string `json:"jti"`
	KeyID     string `json:"kid"`
	IssuedAt  int64  `json:"iat"`
	NotBefore int64  `json:"nbf"`
	ExpiresAt int64  `json:"exp"`
	MaxBytes  int64  `json:"max_bytes"`
}

type MournCapability struct {
	version   string
	payload   string
	signature string
}

func NewAuthMiddleHandler(
	config authMiddleConfig,
	tokenStore *TokenStore,
	logger *slog.Logger,
) (*AuthMiddleHandler, error) {
	if logger == nil {
		logger = slog.Default()
	}

	if tokenStore == nil {
		return nil, errors.New("nil token store")
	}

	publicKey, err := base64.StdEncoding.DecodeString(config.PublicKey)
	if err != nil {
		return nil, err
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return nil, errors.New("ED25519 public key must decode to 32 bytes")
	}

	return &AuthMiddleHandler{
		publicKey:  publicKey,
		tokenStore: tokenStore,
		logger:     logger,
	}, nil
}

func NewUploadClaimsFromCapability(capability MournCapability) (UploadClaims, error) {
	payloadBytes, err := base64.RawURLEncoding.DecodeString(capability.payload)
	if err != nil {
		return UploadClaims{}, fmt.Errorf("decode payload: %w", err)
	}

	var c UploadClaims
	err = json.Unmarshal(payloadBytes, &c)
	if err != nil {
		return UploadClaims{}, fmt.Errorf("unmarshal payload: %w", err)
	}

	now := time.Now().Unix()
	if c.NotBefore > now || c.ExpiresAt <= now {
		return UploadClaims{}, ErrCapabilityExpired
	}

	if c.Issuer != mournRequiredIssuerString {
		return UploadClaims{}, errors.New("invalid issuer")
	}
	if c.Audience != mournRequiredAudienceString {
		return UploadClaims{}, errors.New("invalid audience")
	}
	if c.Scope != mournRequiredScopeString {
		return UploadClaims{}, errors.New("invalid scope")
	}
	if c.Subject == "" {
		return UploadClaims{}, errors.New("subject is empty")
	}
	if c.TokenID == "" {
		return UploadClaims{}, errors.New("tokenID is empty")
	}
	if lft := c.ExpiresAt - c.IssuedAt; lft <= 0 || lft > 120 {
		return UploadClaims{}, errors.New("invalid lifetime")
	}
	if c.MaxBytes <= 0 {
		return UploadClaims{}, errors.New("maxbytes <= 0")
	}
	return c, nil
}

func parseAuthorizationHeader(authHeader string) (MournCapability, error) {
	capabilityString, found := strings.CutPrefix(authHeader, mournCapabilityStringPrefix)
	if !found {
		return MournCapability{}, ErrCapabilityPrefixNotFound
	}

	capabilityParts := strings.Split(capabilityString, mournCapabilitySep)
	if len(capabilityParts) != mournCapabilityPartsCount {
		return MournCapability{}, fmt.Errorf(
			"got %d parts: %w",
			len(capabilityParts),
			ErrInvalidCapabilityPartsCount,
		)
	}

	return MournCapability{
		version:   capabilityParts[0],
		payload:   capabilityParts[1],
		signature: capabilityParts[2],
	}, nil
}

func (c MournCapability) verifySignature(publicKey []byte) error {
	if c.version != mournTokenFormatVersionString {
		return ErrInvalidTokenVersion
	}

	signatureBytes, err := base64.RawURLEncoding.DecodeString(c.signature)
	if err != nil {
		return fmt.Errorf("decode upload signature: %w", err)
	}

	if len(publicKey) != ed25519.PublicKeySize {
		return ErrInvalidKeySize
	}
	message := []byte(mournPayloadPrefix + c.payload)
	if !ed25519.Verify(publicKey, message, signatureBytes) {
		return ErrInvalidUploadSignature
	}

	return nil
}

func verifyContentLengthAgainstUploadLimits(
	contentLength,
	limit int64,
) error {
	if contentLength <= 0 {
		return ErrInvalidUploadLength
	}
	if contentLength > limit {
		return ErrUploadTooLarge
	}
	return nil
}

func (a *AuthMiddleHandler) HTTPAuthenticator(
	next http.Handler,
	uploadCfg localAssetUploaderConfig,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "missing authorization", http.StatusUnauthorized)
			return
		}
		capability, err := parseAuthorizationHeader(authHeader)
		if err != nil {
			http.Error(w, "bad authorization", http.StatusUnauthorized)
			return
		}

		if err := capability.verifySignature(a.publicKey); err != nil {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		claims, err := NewUploadClaimsFromCapability(capability)
		if err != nil {
			http.Error(w, "invalid claims", http.StatusUnauthorized)
			return
		}

		limit := min(claims.MaxBytes, uploadCfg.MaxAssetUploadSize)
		err = verifyContentLengthAgainstUploadLimits(r.ContentLength, limit)
		if err != nil {
			switch {
			case errors.Is(err, ErrInvalidUploadLength):
				http.Error(w, "invalid content length", http.StatusBadRequest)
			case errors.Is(err, ErrUploadTooLarge):
				http.Error(w, "content too large", http.StatusRequestEntityTooLarge)
			default:
				http.Error(w, "bad content length", http.StatusBadRequest)
			}
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)

		if r.Header.Get("Content-Type") != "application/octet-stream" {
			http.Error(w, "wrong content type", http.StatusUnsupportedMediaType)
			return
		}

		err = a.tokenStore.InsertToken(claims.TokenID, claims.ExpiresAt)
		if err != nil {
			switch {
			case errors.Is(err, ErrTokenAlreadyConsumed):
				http.Error(w, "token already consumed", http.StatusUnauthorized)
			default:
				http.Error(w, "server error", http.StatusServiceUnavailable)
				a.logger.Error("insert token", "err", err)
			}
			return
		}

		next.ServeHTTP(w, r)
	})
}
