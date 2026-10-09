package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"mywebapp/internal/config"
)

var (
	ErrInvalidToken   = errors.New("invalid token")
	ErrExpiredToken   = errors.New("token has expired")
	ErrWrongTokenType = errors.New("wrong token type")
	ErrMissingJTI     = errors.New("refresh token missing jti")

	// ErrWeakSecret is returned by NewJWTService when the configured
	// secret is missing (in production) or too short to be safe.
	ErrWeakSecret = errors.New("jwt secret is missing or too weak")
)

// TokenType identifies the purpose of a token. Access tokens authorise
// individual requests; refresh tokens are only accepted by the refresh
// endpoint. Including this in the JWT payload prevents an attacker who
// steals one from using it as the other.
type TokenType string

const (
	TokenTypeAccess  TokenType = "access"
	TokenTypeRefresh TokenType = "refresh"
)

// minimumSecretBytes is the shortest HMAC key we will accept. HS256
// uses a 256-bit key; anything shorter than the hash output is a
// downgrade. 32 bytes is the RFC 7518 §3.2 minimum.
const minimumSecretBytes = 32

// Claims holds the JWT claims for a user session.
type Claims struct {
	UserID   int32     `json:"user_id"`
	Username string    `json:"username"`
	Role     string    `json:"role"`
	Typ      TokenType `json:"typ"`
	jwt.RegisteredClaims
}

// JWTService handles token creation and validation. Access tokens and
// refresh tokens share the same signing key but carry different `typ`
// and different TTLs, so a refresh token cannot be presented as an
// access token (and vice versa) even if the signature is valid.
type JWTService struct {
	secretKey  []byte
	issuer     string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewJWTService creates a JWTService.
//
// The secret is read from JWT_SECRET. In production and staging the
// constructor returns ErrWeakSecret if the variable is unset or shorter
// than 32 bytes: a deployment that forgets to set it must fail to
// start, not run with a guessable key.
//
// In development a random per-process secret is generated instead. The
// consequence is that tokens issued before a restart do not survive
// the restart, which is the correct behaviour for a dev machine and
// makes the missing-secret case visible rather than silent.
//
// TTLs are read from JWT_ACCESS_TTL and JWT_REFRESH_TTL. Defaults are
// 15 minutes and 7 days respectively.
func NewJWTService() (*JWTService, error) {
	secret, err := loadJWTSecret()
	if err != nil {
		return nil, err
	}
	return &JWTService{
		secretKey:  secret,
		issuer:     "mywebapp",
		accessTTL:  parseDurationEnv("JWT_ACCESS_TTL", 15*time.Minute),
		refreshTTL: parseDurationEnv("JWT_REFRESH_TTL", 7*24*time.Hour),
	}, nil
}

// loadJWTSecret returns the HMAC key, or an error in production when it
// is missing or too short. In development it generates a random key so
// the process can start without configuration.
func loadJWTSecret() ([]byte, error) {
	raw := os.Getenv("JWT_SECRET")

	if raw == "" {
		if config.IsProduction() || config.IsStaging() {
			return nil, fmt.Errorf(
				"%w: JWT_SECRET is unset (APP_ENV=%s)",
				ErrWeakSecret, config.Current(),
			)
		}
		// Development only.
		key := make([]byte, minimumSecretBytes)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("jwt: generate dev secret: %w", err)
		}
		slog.Warn("jwt: JWT_SECRET is unset; using a random per-process secret. " +
			"All tokens will be invalidated on restart. " +
			"Set JWT_SECRET before deploying.")
		return key, nil
	}

	key := []byte(raw)
	if len(key) < minimumSecretBytes {
		if config.IsProduction() || config.IsStaging() {
			return nil, fmt.Errorf(
				"%w: JWT_SECRET is %d bytes; need at least %d",
				ErrWeakSecret, len(key), minimumSecretBytes,
			)
		}
		slog.Warn("jwt: JWT_SECRET is shorter than the recommended minimum; "+
			"this is tolerated in development only",
			"length", len(key), "minimum", minimumSecretBytes)
	}
	return key, nil
}

// AccessTTL returns the configured access-token lifetime.
func (s *JWTService) AccessTTL() time.Duration { return s.accessTTL }

// RefreshTTL returns the configured refresh-token lifetime.
func (s *JWTService) RefreshTTL() time.Duration { return s.refreshTTL }

// GenerateAccessToken creates a short-lived signed JWT for the given user.
func (s *JWTService) GenerateAccessToken(userID int32, username, role string) (string, error) {
	now := time.Now()
	claims := &Claims{
		UserID:    userID,
		Username:  username,
		Role:      role,
		Typ:       TokenTypeAccess,
		Issuer:    s.issuer,
		Subject:   username,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secretKey)
}

// GenerateRefreshToken creates a long-lived signed JWT carrying a jti.
// The jti is the primary key used by the sessions table for revocation.
//
// Returns the signed token and the jti, so the caller can persist the
// session row in the same transaction as the token's issuance.
func (s *JWTService) GenerateRefreshToken(userID int32, username, role string) (string, string, error) {
	jti, err := newJTI()
	if err != nil {
		return "", "", err
	}

	now := time.Now()
	claims := &Claims{
		UserID:    userID,
		Username:  username,
		Role:      role,
		Typ:       TokenTypeRefresh,
		Issuer:    s.issuer,
		Subject:   username,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(s.refreshTTL)),
		ID:        jti,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.secretKey)
	if err != nil {
		return "", "", err
	}
	return signed, jti, nil
}

// ValidateAccessToken parses and validates an access token. Returns
// ErrWrongTokenType if a valid refresh token is presented instead.
func (s *JWTService) ValidateAccessToken(tokenString string) (*Claims, error) {
	return s.validate(tokenString, TokenTypeAccess)
}

// ValidateRefreshToken parses and validates a refresh token. Returns
// ErrWrongTokenType if a valid access token is presented instead.
// Returns ErrMissingJTI if the token has no jti claim — a refresh
// token without a jti cannot be revoked and must be rejected.
func (s *JWTService) ValidateRefreshToken(tokenString string) (*Claims, error) {
	claims, err := s.validate(tokenString, TokenTypeRefresh)
	if err != nil {
		return nil, err
	}
	if claims.ID == "" {
		return nil, ErrMissingJTI
	}
	return claims, nil
}

// validate parses the token, verifies the signature, checks the
// expiry, and confirms the `typ` claim matches the expected value.
func (s *JWTService) validate(tokenString string, expected TokenType) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return s.secretKey, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}
	if !token.Valid {
		return nil, ErrInvalidToken
	}
	if claims.ExpiresAt != nil && claims.ExpiresAt.Before(time.Now()) {
		return nil, ErrExpiredToken
	}
	if claims.Typ != expected {
		return nil, ErrWrongTokenType
	}
	return claims, nil
}

// newJTI returns a random, URL-safe token identifier. 16 bytes is
// sufficient for uniqueness within any realistic session volume; the
// value is not a secret and does not need the entropy of a signing key.
func newJTI() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// parseDurationEnv reads a Go duration string from the environment,
// falling back to the default on absence or parse failure.
func parseDurationEnv(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return def
	}
	// Guard against an absurdly long refresh TTL that would defeat
	// revocation by outliving the session table's expiry cleanup.
	if d > 30*24*time.Hour {
		return 30 * 24 * time.Hour
	}
	return d
}
