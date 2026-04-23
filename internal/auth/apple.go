package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	apperrors "stride/backend/internal/errors"
)

const (
	appleKeysURL     = "https://appleid.apple.com/auth/keys"
	appleIssuer      = "https://appleid.apple.com"
	keysCacheTTL     = 24 * time.Hour
	keysRefreshRetry = 1 * time.Hour
)

// AppleAuthConfig holds configuration for Apple Sign In verification.
type AppleAuthConfig struct {
	TeamID   string
	BundleID string
}

// AppleAuthVerifier verifies Apple Sign In identity tokens.
type AppleAuthVerifier struct {
	config      AppleAuthConfig
	httpClient  *http.Client
	keysCache   map[string]*rsa.PublicKey
	keysCachedAt time.Time
	keysMu      sync.RWMutex
}

// AppleClaims represents the claims from an Apple identity token.
type AppleClaims struct {
	jwt.RegisteredClaims
	Email         string `json:"email,omitempty"`
	EmailVerified any    `json:"email_verified,omitempty"` // can be bool or string
	IsPrivateEmail any   `json:"is_private_email,omitempty"`
	AuthTime      int64  `json:"auth_time,omitempty"`
	NonceSupported bool  `json:"nonce_supported,omitempty"`
}

// AppleKeysResponse represents the response from Apple's public keys endpoint.
type AppleKeysResponse struct {
	Keys []AppleJWK `json:"keys"`
}

// AppleJWK represents a single JSON Web Key from Apple.
type AppleJWK struct {
	Kty string `json:"kty"` // Key type (RSA)
	Kid string `json:"kid"` // Key ID
	Use string `json:"use"` // Key use (sig)
	Alg string `json:"alg"` // Algorithm (RS256)
	N   string `json:"n"`   // RSA modulus
	E   string `json:"e"`   // RSA exponent
}

// NewAppleAuthVerifier creates a new Apple identity token verifier.
func NewAppleAuthVerifier(config AppleAuthConfig) *AppleAuthVerifier {
	return &AppleAuthVerifier{
		config: config,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		keysCache: make(map[string]*rsa.PublicKey),
	}
}

// VerifyIdentityToken verifies an Apple identity token and returns the claims.
func (v *AppleAuthVerifier) VerifyIdentityToken(ctx context.Context, tokenString string) (*AppleClaims, error) {
	// Parse the token to get the key ID
	token, err := jwt.ParseWithClaims(tokenString, &AppleClaims{}, func(t *jwt.Token) (any, error) {
		// Verify the signing algorithm
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}

		// Get the key ID from the token header
		kid, ok := t.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("missing kid in token header")
		}

		// Get the public key for this key ID
		publicKey, err := v.getPublicKey(ctx, kid)
		if err != nil {
			return nil, err
		}

		return publicKey, nil
	})

	if err != nil {
		return nil, apperrors.NewUnauthorizedError("invalid Apple token").WithError(err)
	}

	if !token.Valid {
		return nil, apperrors.NewUnauthorizedError("Apple token is not valid")
	}

	claims, ok := token.Claims.(*AppleClaims)
	if !ok {
		return nil, apperrors.NewUnauthorizedError("invalid Apple token claims")
	}

	// Validate the issuer
	if claims.Issuer != appleIssuer {
		return nil, apperrors.NewUnauthorizedError("invalid Apple token issuer")
	}

	// Validate the audience (should be our app's bundle ID)
	if !v.validateAudience(claims.Audience) {
		return nil, apperrors.NewUnauthorizedError("invalid Apple token audience")
	}

	// Validate the subject (Apple user ID) is present
	if claims.Subject == "" {
		return nil, apperrors.NewUnauthorizedError("missing Apple user ID in token")
	}

	return claims, nil
}

// GetAppleUserID is a convenience method that extracts just the Apple user ID.
func (v *AppleAuthVerifier) GetAppleUserID(ctx context.Context, tokenString string) (string, error) {
	claims, err := v.VerifyIdentityToken(ctx, tokenString)
	if err != nil {
		return "", err
	}
	return claims.Subject, nil
}

// validateAudience checks if the audience includes our bundle ID.
func (v *AppleAuthVerifier) validateAudience(audiences []string) bool {
	for _, aud := range audiences {
		if aud == v.config.BundleID {
			return true
		}
	}
	return false
}

// getPublicKey retrieves the public key for the given key ID.
func (v *AppleAuthVerifier) getPublicKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	// Check cache first
	v.keysMu.RLock()
	if key, ok := v.keysCache[kid]; ok && time.Since(v.keysCachedAt) < keysCacheTTL {
		v.keysMu.RUnlock()
		return key, nil
	}
	v.keysMu.RUnlock()

	// Fetch fresh keys
	if err := v.refreshKeys(ctx); err != nil {
		return nil, err
	}

	// Try cache again
	v.keysMu.RLock()
	defer v.keysMu.RUnlock()

	key, ok := v.keysCache[kid]
	if !ok {
		return nil, fmt.Errorf("key with id %s not found", kid)
	}

	return key, nil
}

// refreshKeys fetches the latest public keys from Apple.
func (v *AppleAuthVerifier) refreshKeys(ctx context.Context) error {
	v.keysMu.Lock()
	defer v.keysMu.Unlock()

	// Double-check if another goroutine already refreshed
	if time.Since(v.keysCachedAt) < keysRefreshRetry {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, appleKeysURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch Apple keys: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Apple keys endpoint returned %d", resp.StatusCode)
	}

	var keysResp AppleKeysResponse
	if err := json.NewDecoder(resp.Body).Decode(&keysResp); err != nil {
		return fmt.Errorf("decode Apple keys: %w", err)
	}

	// Convert JWKs to RSA public keys
	newCache := make(map[string]*rsa.PublicKey)
	for _, jwk := range keysResp.Keys {
		if jwk.Kty != "RSA" {
			continue
		}

		pubKey, err := jwkToRSAPublicKey(jwk)
		if err != nil {
			continue // Skip invalid keys
		}

		newCache[jwk.Kid] = pubKey
	}

	v.keysCache = newCache
	v.keysCachedAt = time.Now()

	return nil
}

// jwkToRSAPublicKey converts a JWK to an RSA public key.
func jwkToRSAPublicKey(jwk AppleJWK) (*rsa.PublicKey, error) {
	// Decode the modulus
	nBytes, err := base64.RawURLEncoding.DecodeString(jwk.N)
	if err != nil {
		return nil, fmt.Errorf("decode modulus: %w", err)
	}

	// Decode the exponent
	eBytes, err := base64.RawURLEncoding.DecodeString(jwk.E)
	if err != nil {
		return nil, fmt.Errorf("decode exponent: %w", err)
	}

	// Convert exponent bytes to int
	var e int
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}

	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: e,
	}, nil
}

// IsEmailVerified checks if the email in the claims is verified.
func (c *AppleClaims) IsEmailVerified() bool {
	switch v := c.EmailVerified.(type) {
	case bool:
		return v
	case string:
		return v == "true"
	default:
		return false
	}
}
