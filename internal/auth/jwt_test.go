package auth

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"mywebapp/internal/config"
)

// -----------------------------------------------------------------------------
// Test helpers
// -----------------------------------------------------------------------------

// setEnv sets an environment variable for the duration of the test and
// restores the previous value on cleanup. This is a local helper rather
// than t.Setenv because a handful of these tests need to unset a
// variable entirely, which t.Setenv cannot express (t.Setenv only sets,
// it does not unset). Using one helper for both cases keeps the tests
// readable.
func setEnv(t *testing.T, key, value string) {
	t.Helper()
	prev, had := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("setenv %s: %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, prev)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

// unsetEnv removes an environment variable for the duration of the
// test, restoring it on cleanup.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	prev, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unsetenv %s: %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, prev)
		}
	})
}

// devSecret is a 32-byte value that satisfies the minimum length check.
// It is used by tests that exercise token behaviour rather than secret
// policy, so they don't have to reason about the length requirement.
const devSecret = "test-secret-do-not-use-in-prod!!" // exactly 32 bytes

// newTestService returns a JWTService configured for behavioural tests.
//
// It pins APP_ENV=development so the test is hermetic: a machine with
// APP_ENV=production in its shell must not change the outcome of a test
// that is not about environment resolution. It also sets a valid
// JWT_SECRET so the caller does not have to.
func newTestService(t *testing.T, accessTTL, refreshTTL time.Duration) *JWTService {
	t.Helper()
	setEnv(t, "APP_ENV", "development")
	setEnv(t, "JWT_SECRET", devSecret)
	setEnv(t, "JWT_ACCESS_TTL", accessTTL.String())
	setEnv(t, "JWT_REFRESH_TTL", refreshTTL.String())

	svc, err := NewJWTService()
	if err != nil {
		t.Fatalf("NewJWTService: %v", err)
	}
	return svc
}

// -----------------------------------------------------------------------------
// Secret policy: production and staging fail closed
// -----------------------------------------------------------------------------

func TestNewJWTService_ProductionMissingSecretFails(t *testing.T) {
	setEnv(t, "APP_ENV", "production")
	unsetEnv(t, "JWT_SECRET")

	_, err := NewJWTService()
	if err == nil {
		t.Fatal("expected error when JWT_SECRET is unset in production, got nil")
	}
	if !errors.Is(err, ErrWeakSecret) {
		t.Fatalf("expected ErrWeakSecret, got %v", err)
	}
}

func TestNewJWTService_ProductionShortSecretFails(t *testing.T) {
	setEnv(t, "APP_ENV", "production")
	setEnv(t, "JWT_SECRET", strings.Repeat("x", 16))

	_, err := NewJWTService()
	if err == nil {
		t.Fatal("expected error for short JWT_SECRET in production, got nil")
	}
	if !errors.Is(err, ErrWeakSecret) {
		t.Fatalf("expected ErrWeakSecret, got %v", err)
	}
}

func TestNewJWTService_StagingMissingSecretFails(t *testing.T) {
	// Staging is treated as production for secret requirements. This
	// is the property that keeps a staging deploy from running with a
	// per-process key and producing intermittent 403s across nodes.
	setEnv(t, "APP_ENV", "staging")
	unsetEnv(t, "JWT_SECRET")

	if _, err := NewJWTService(); !errors.Is(err, ErrWeakSecret) {
		t.Fatalf("expected ErrWeakSecret, got %v", err)
	}
}

func TestNewJWTService_ProductionValidSecretSucceeds(t *testing.T) {
	setEnv(t, "APP_ENV", "production")
	secret := strings.Repeat("a", minimumSecretBytes)
	setEnv(t, "JWT_SECRET", secret)

	svc, err := NewJWTService()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(svc.secretKey) != secret {
		t.Fatal("secret key not loaded from env")
	}
}

// -----------------------------------------------------------------------------
// Secret policy: development falls back to a random per-process key
// -----------------------------------------------------------------------------

func TestNewJWTService_DevelopmentMissingSecretUsesRandomKey(t *testing.T) {
	setEnv(t, "APP_ENV", "development")
	unsetEnv(t, "JWT_SECRET")

	svc, err := NewJWTService()
	if err != nil {
		t.Fatalf("unexpected error in development: %v", err)
	}
	if len(svc.secretKey) < minimumSecretBytes {
		t.Fatalf("dev key too short: %d bytes", len(svc.secretKey))
	}

	// Two dev instances must not share a key. This is the property
	// that makes a forgotten JWT_SECRET visible: tokens issued by one
	// process are rejected by the next.
	svc2, err := NewJWTService()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(svc.secretKey) == string(svc2.secretKey) {
		t.Fatal("two dev instances generated the same key; randomness is broken")
	}
}

// -----------------------------------------------------------------------------
// Environment resolution
// -----------------------------------------------------------------------------

func TestNewJWTService_DefaultEnvironmentIsDevelopment(t *testing.T) {
	unsetEnv(t, "APP_ENV")
	unsetEnv(t, "JWT_SECRET")

	// The default must be development: an operator running `go run`
	// without configuration should not be forced to set a secret.
	if config.Current() != config.EnvDevelopment {
		t.Fatalf("default environment = %q, want development", config.Current())
	}

	if _, err := NewJWTService(); err != nil {
		t.Fatalf("unexpected error with default environment: %v", err)
	}
}

func TestNewJWTService_UnknownEnvironmentIsTreatedAsProduction(t *testing.T) {
	setEnv(t, "APP_ENV", "prod-typo")
	unsetEnv(t, "JWT_SECRET")

	// Fail closed: a mistyped APP_ENV must not disable the secret
	// requirement.
	if config.Current() != config.EnvProduction {
		t.Fatalf("unknown env resolved to %q, want production", config.Current())
	}
	if _, err := NewJWTService(); err == nil {
		t.Fatal("expected error for unset secret under unknown env, got nil")
	}
}

// -----------------------------------------------------------------------------
// Token behaviour
// -----------------------------------------------------------------------------

func TestGenerateAccessToken_ValidatesAsAccess(t *testing.T) {
	svc := newTestService(t, 15*time.Minute, 7*24*time.Hour)

	tok, err := svc.GenerateAccessToken(1, "alice", "admin")
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}

	claims, err := svc.ValidateAccessToken(tok)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.UserID != 1 || claims.Username != "alice" || claims.Role != "admin" {
		t.Errorf("unexpected claims: %+v", claims)
	}
	if claims.Typ != TokenTypeAccess {
		t.Errorf("expected typ=access, got %q", claims.Typ)
	}
}

func TestGenerateAccessToken_RejectedAsRefresh(t *testing.T) {
	svc := newTestService(t, 15*time.Minute, 7*24*time.Hour)

	tok, _ := svc.GenerateAccessToken(1, "alice", "admin")

	_, err := svc.ValidateRefreshToken(tok)
	if !errors.Is(err, ErrWrongTokenType) {
		t.Errorf("expected ErrWrongTokenType, got %v", err)
	}
}

func TestGenerateRefreshToken_ValidatesAsRefresh(t *testing.T) {
	svc := newTestService(t, 15*time.Minute, 7*24*time.Hour)

	tok, jti, err := svc.GenerateRefreshToken(1, "alice", "admin")
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}
	if jti == "" {
		t.Fatal("expected non-empty jti")
	}

	claims, err := svc.ValidateRefreshToken(tok)
	if err != nil {
		t.Fatalf("ValidateRefreshToken: %v", err)
	}
	if claims.ID != jti {
		t.Errorf("jti mismatch: token=%q claims=%q", jti, claims.ID)
	}
	if claims.Typ != TokenTypeRefresh {
		t.Errorf("expected typ=refresh, got %q", claims.Typ)
	}
}

func TestGenerateRefreshToken_RejectedAsAccess(t *testing.T) {
	svc := newTestService(t, 15*time.Minute, 7*24*time.Hour)

	tok, _, _ := svc.GenerateRefreshToken(1, "alice", "admin")

	_, err := svc.ValidateAccessToken(tok)
	if !errors.Is(err, ErrWrongTokenType) {
		t.Errorf("expected ErrWrongTokenType, got %v", err)
	}
}

func TestValidateAccessToken_RejectsExpired(t *testing.T) {
	// Use a 1-second TTL and sleep 2 seconds. A sub-second TTL is
	// unreliable because jwt/v5 stores exp at second precision.
	svc := newTestService(t, time.Second, 7*24*time.Hour)

	tok, _ := svc.GenerateAccessToken(1, "alice", "admin")
	time.Sleep(2 * time.Second)

	_, err := svc.ValidateAccessToken(tok)
	if !errors.Is(err, ErrExpiredToken) {
		t.Errorf("expected ErrExpiredToken, got %v", err)
	}
}

func TestValidateAccessToken_RejectsBadSignature(t *testing.T) {
	svc := newTestService(t, 15*time.Minute, 7*24*time.Hour)
	tok, _ := svc.GenerateAccessToken(1, "alice", "admin")

	// Tamper with the signature.
	tampered := tok[:len(tok)-2] + "XX"

	_, err := svc.ValidateAccessToken(tampered)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken, got %v", err)
	}
}

func TestValidateAccessToken_RejectsTokenSignedWithDifferentSecret(t *testing.T) {
	// A token signed with a different key must be rejected. This is
	// the property that makes the per-process dev key safe: two
	// instances with different keys cannot accept each other's tokens.
	other := &JWTService{
		secretKey:  []byte("a-completely-different-secret-key!"),
		issuer:     "mywebapp",
		accessTTL:  15 * time.Minute,
		refreshTTL: 7 * 24 * time.Hour,
	}
	tok, err := other.GenerateAccessToken(1, "alice", "admin")
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}

	svc := newTestService(t, 15*time.Minute, 7*24*time.Hour)
	if _, err := svc.ValidateAccessToken(tok); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken, got %v", err)
	}
}

func TestValidateRefreshToken_RejectsMissingJTI(t *testing.T) {
	svc := newTestService(t, 15*time.Minute, 7*24*time.Hour)

	// Construct a refresh token manually with no ID claim. Uses the
	// same signing method and secret as the service.
	now := time.Now()
	claims := &Claims{
		UserID:   1,
		Username: "alice",
		Role:     "admin",
		Typ:      TokenTypeRefresh,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "mywebapp",
			Subject:   "alice",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
			// ID deliberately omitted
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(devSecret))
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}

	_, err = svc.ValidateRefreshToken(signed)
	if !errors.Is(err, ErrMissingJTI) {
		t.Errorf("expected ErrMissingJTI, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// parseDurationEnv
// -----------------------------------------------------------------------------

func TestParseDurationEnv_DefaultsOnEmpty(t *testing.T) {
	t.Setenv("JWT_ACCESS_TTL", "")
	if got := parseDurationEnv("JWT_ACCESS_TTL", 15*time.Minute); got != 15*time.Minute {
		t.Errorf("expected default 15m, got %v", got)
	}
}

func TestParseDurationEnv_DefaultsOnInvalid(t *testing.T) {
	t.Setenv("JWT_ACCESS_TTL", "not-a-duration")
	if got := parseDurationEnv("JWT_ACCESS_TTL", 15*time.Minute); got != 15*time.Minute {
		t.Errorf("expected default 15m, got %v", got)
	}
}

func TestParseDurationEnv_CapsAtThirtyDays(t *testing.T) {
	t.Setenv("JWT_REFRESH_TTL", "8760h") // 1 year
	got := parseDurationEnv("JWT_REFRESH_TTL", 7*24*time.Hour)
	if got != 30*24*time.Hour {
		t.Errorf("expected 30-day cap, got %v", got)
	}
}
