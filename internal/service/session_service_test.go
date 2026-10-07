// internal/service/session_service_test.go
package service

import (
	"context"
	"testing"
	"time"

	"mywebapp/internal/testutil"
)

func newSessionSvc(t *testing.T) *SessionService {
	t.Helper()
	pool := testutil.NewPool(t)
	return NewSessionService(pool)
}

func TestSessionService_CreateAndIsActive(t *testing.T) {
	svc := newSessionSvc(t)
	ctx := context.Background()

	jti := uniqueJTI()
	userID := seededAliceID(t)

	if err := svc.Create(ctx, CreateSessionParams{
		JTI:       jti,
		UserID:    userID,
		ExpiresAt: time.Now().Add(time.Hour),
		UserAgent: "test",
		ClientIP:  "127.0.0.1",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.IsActive(ctx, jti); err != nil {
		t.Errorf("IsActive returned error for active session: %v", err)
	}
}

func TestSessionService_IsActiveUnknownJTI(t *testing.T) {
	svc := newSessionSvc(t)

	err := svc.IsActive(context.Background(), "definitely-not-a-real-jti")
	if err == nil {
		t.Error("IsActive should fail for an unknown JTI")
	}
}

func TestSessionService_Revoke(t *testing.T) {
	svc := newSessionSvc(t)
	ctx := context.Background()

	jti := uniqueJTI()
	userID := seededAliceID(t)

	if err := svc.Create(ctx, CreateSessionParams{
		JTI:       jti,
		UserID:    userID,
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.Revoke(ctx, jti); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if err := svc.IsActive(ctx, jti); err == nil {
		t.Error("IsActive should fail after Revoke")
	}
}

func TestSessionService_RevokeAllForUser(t *testing.T) {
	svc := newSessionSvc(t)
	ctx := context.Background()

	userID := seededAliceID(t)

	jti1, jti2 := uniqueJTI(), uniqueJTI()
	for _, jti := range []string{jti1, jti2} {
		if err := svc.Create(ctx, CreateSessionParams{
			JTI:       jti,
			UserID:    userID,
			ExpiresAt: time.Now().Add(time.Hour),
		}); err != nil {
			t.Fatalf("Create %s: %v", jti, err)
		}
	}

	if err := svc.RevokeAllForUser(ctx, userID); err != nil {
		t.Fatalf("RevokeAllForUser: %v", err)
	}

	for _, jti := range []string{jti1, jti2} {
		if err := svc.IsActive(ctx, jti); err == nil {
			t.Errorf("session %s should be revoked", jti)
		}
	}
}
