// internal/service/user_service_test.go
package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"mywebapp/internal/domain"
	"mywebapp/internal/repository"
	"mywebapp/internal/testutil"
)

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

func newUserSvc(t *testing.T) *UserService {
	t.Helper()
	pool := testutil.NewPool(t)
	return NewUserServiceWithPool(pool)
}

// newUserAndSessionSvc returns a UserService and a SessionService built
// on the same pool.
func newUserAndSessionSvc(t *testing.T) (*UserService, *SessionService) {
	t.Helper()
	pool := testutil.NewPool(t)
	return NewUserServiceWithPool(pool), NewSessionService(pool)
}

// createTestUser inserts a user owned by the calling test. The email
// and username carry a nanosecond suffix so reruns against a
// persistent database do not collide, and so two tests running in the
// same binary never see each other's rows.
//
// The caller owns the row: it may delete it, deactivate it, promote
// it, or leave it in place. No other test reads it, so none of those
// choices can affect a later test. This is the property the seeded
// alice fixture does not have, and the reason the delete tests failed
// against the seed.
//
// Passing role="" creates the user with the default role ("user").
// Passing "admin" or "moderator" promotes the new user immediately.
func createTestUser(t *testing.T, svc *UserService, role string) *domain.User {
	t.Helper()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	user, err := svc.CreateUser(context.Background(), domain.CreateUserRequest{
		Email:    "svc-" + suffix + "@example.test",
		Password: "password123",
		Username: "svc-" + suffix,
		FullName: "Service Test User",
	})
	if err != nil {
		t.Fatalf("createTestUser: %v", err)
	}

	if role != "" && role != "user" {
		updated, err := svc.UpdateUserRole(context.Background(), user.ID, role)
		if err != nil {
			t.Fatalf("promote to %s: %v", role, err)
		}
		return updated
	}
	return user
}

// createSessionFor inserts a session row for the given user and
// returns its JTI.
func createSessionFor(t *testing.T, sess *SessionService, userID int32) string {
	t.Helper()
	jti := uniqueJTI()
	if err := sess.Create(context.Background(), CreateSessionParams{
		JTI:       jti,
		UserID:    userID,
		ExpiresAt: time.Now().Add(time.Hour),
		UserAgent: "test",
		ClientIP:  "127.0.0.1",
	}); err != nil {
		t.Fatalf("Create session: %v", err)
	}
	return jti
}

// -----------------------------------------------------------------------------
// DeleteUser: transaction boundary
// -----------------------------------------------------------------------------

func TestUserService_DeleteUser_RevokesSessions(t *testing.T) {
	userSvc, sessSvc := newUserAndSessionSvc(t)
	ctx := context.Background()

	user := createTestUser(t, userSvc, "")
	jti1 := createSessionFor(t, sessSvc, user.ID)
	jti2 := createSessionFor(t, sessSvc, user.ID)

	if err := userSvc.DeleteUser(ctx, user.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	if _, err := userSvc.GetUserByID(ctx, user.ID); !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound after delete, got %v", err)
	}

	for _, jti := range []string{jti1, jti2} {
		if err := sessSvc.IsActive(ctx, jti); err == nil {
			t.Errorf("session %s still active after user delete", jti)
		}
	}
}

func TestUserService_DeleteUser_UnknownUser(t *testing.T) {
	userSvc := newUserSvc(t)

	err := userSvc.DeleteUser(context.Background(), 2147483647)
	if !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestUserService_DeleteUser_RollsBackOnRevokeFailure(t *testing.T) {
	userSvc, sessSvc := newUserAndSessionSvc(t)
	ctx := context.Background()

	user := createTestUser(t, userSvc, "")
	jti := createSessionFor(t, sessSvc, user.ID)

	forced := errors.New("forced revoke failure")
	prev := revokeAllSessionsHook
	revokeAllSessionsHook = func(ctx context.Context, q *repository.Queries, userID int32) error {
		return forced
	}
	t.Cleanup(func() { revokeAllSessionsHook = prev })

	err := userSvc.DeleteUser(ctx, user.ID)
	if !errors.Is(err, forced) {
		t.Fatalf("expected forced error to propagate, got %v", err)
	}

	if _, err := userSvc.GetUserByID(ctx, user.ID); err != nil {
		t.Fatalf("user row was deleted despite rollback: %v", err)
	}

	if err := sessSvc.IsActive(ctx, jti); err != nil {
		t.Fatalf("session was revoked despite rollback: %v", err)
	}
}

// -----------------------------------------------------------------------------
// UpdateUserRole: transaction boundary
// -----------------------------------------------------------------------------

func TestUserService_UpdateUserRole_PromotesAndDemotes(t *testing.T) {
	userSvc := newUserSvc(t)
	ctx := context.Background()

	user := createTestUser(t, userSvc, "")

	promoted, err := userSvc.UpdateUserRole(ctx, user.ID, "admin")
	if err != nil {
		t.Fatalf("promote to admin: %v", err)
	}
	if promoted.Role != "admin" {
		t.Errorf("role after promote = %q, want admin", promoted.Role)
	}

	demoted, err := userSvc.UpdateUserRole(ctx, user.ID, "user")
	if err != nil {
		t.Fatalf("demote to user: %v", err)
	}
	if demoted.Role != "user" {
		t.Errorf("role after demote = %q, want user", demoted.Role)
	}
}

func TestUserService_UpdateUserRole_RevokesSessions(t *testing.T) {
	userSvc, sessSvc := newUserAndSessionSvc(t)
	ctx := context.Background()

	user := createTestUser(t, userSvc, "admin")

	jti1 := createSessionFor(t, sessSvc, user.ID)
	jti2 := createSessionFor(t, sessSvc, user.ID)

	if _, err := userSvc.UpdateUserRole(ctx, user.ID, "user"); err != nil {
		t.Fatalf("demote to user: %v", err)
	}

	for _, jti := range []string{jti1, jti2} {
		if err := sessSvc.IsActive(ctx, jti); err == nil {
			t.Errorf("session %s still active after role change", jti)
		}
	}
}

func TestUserService_UpdateUserRole_InvalidRoleRejected(t *testing.T) {
	userSvc := newUserSvc(t)
	ctx := context.Background()

	user := createTestUser(t, userSvc, "")

	if _, err := userSvc.UpdateUserRole(ctx, user.ID, "superuser"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}

	reloaded, err := userSvc.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if reloaded.Role != user.Role {
		t.Errorf("role changed despite invalid input: %q -> %q", user.Role, reloaded.Role)
	}
}

func TestUserService_UpdateUserRole_UnknownUser(t *testing.T) {
	userSvc := newUserSvc(t)

	_, err := userSvc.UpdateUserRole(context.Background(), 2147483647, "user")
	if !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestUserService_UpdateUserRole_RollsBackOnRevokeFailure(t *testing.T) {
	userSvc, sessSvc := newUserAndSessionSvc(t)
	ctx := context.Background()

	user := createTestUser(t, userSvc, "admin")
	jti := createSessionFor(t, sessSvc, user.ID)

	forced := errors.New("forced revoke failure")
	prev := revokeAllSessionsHook
	revokeAllSessionsHook = func(ctx context.Context, q *repository.Queries, userID int32) error {
		return forced
	}
	t.Cleanup(func() { revokeAllSessionsHook = prev })

	if _, err := userSvc.UpdateUserRole(ctx, user.ID, "user"); !errors.Is(err, forced) {
		t.Fatalf("expected forced error to propagate, got %v", err)
	}

	reloaded, err := userSvc.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if reloaded.Role != "admin" {
		t.Errorf("role = %q after rollback, want admin", reloaded.Role)
	}

	if err := sessSvc.IsActive(ctx, jti); err != nil {
		t.Fatalf("session was revoked despite rollback: %v", err)
	}
}

// -----------------------------------------------------------------------------
// UpdateUser: deactivation revokes sessions
// -----------------------------------------------------------------------------

func TestUserService_UpdateUser_DeactivationRevokesSessions(t *testing.T) {
	userSvc, sessSvc := newUserAndSessionSvc(t)
	ctx := context.Background()

	user := createTestUser(t, userSvc, "")
	jti := createSessionFor(t, sessSvc, user.ID)

	inactive := false
	updated, err := userSvc.UpdateUser(ctx, user.ID, domain.UpdateUserRequest{
		Email:    user.Email,
		Username: user.Username,
		FullName: user.FullName,
		IsActive: &inactive,
	})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if updated.IsActive {
		t.Fatal("user is still active after deactivation")
	}

	if err := sessSvc.IsActive(ctx, jti); err == nil {
		t.Error("session still active after deactivation")
	}
}

// -----------------------------------------------------------------------------
// CreateUser: conflict mapping
// -----------------------------------------------------------------------------

func TestUserService_CreateUser_EmailConflict(t *testing.T) {
	userSvc := newUserSvc(t)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	email := "conflict-" + suffix + "@example.test"

	first := domain.CreateUserRequest{
		Email:    email,
		Password: "password123",
		Username: "conflict-a-" + suffix,
		FullName: "Conflict A",
	}
	if _, err := userSvc.CreateUser(ctx, first); err != nil {
		t.Fatalf("first create: %v", err)
	}

	second := domain.CreateUserRequest{
		Email:    email,
		Password: "password123",
		Username: "conflict-b-" + suffix,
		FullName: "Conflict B",
	}
	if _, err := userSvc.CreateUser(ctx, second); !errors.Is(err, domain.ErrEmailExists) {
		t.Fatalf("expected ErrEmailExists, got %v", err)
	}
}

func TestUserService_CreateUser_UsernameConflict(t *testing.T) {
	userSvc := newUserSvc(t)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	username := "conflict-" + suffix

	first := domain.CreateUserRequest{
		Email:    "a-" + suffix + "@example.test",
		Password: "password123",
		Username: username,
		FullName: "Conflict A",
	}
	if _, err := userSvc.CreateUser(ctx, first); err != nil {
		t.Fatalf("first create: %v", err)
	}

	second := domain.CreateUserRequest{
		Email:    "b-" + suffix + "@example.test",
		Password: "password123",
		Username: username,
		FullName: "Conflict B",
	}
	if _, err := userSvc.CreateUser(ctx, second); !errors.Is(err, domain.ErrUsernameExists) {
		t.Fatalf("expected ErrUsernameExists, got %v", err)
	}
}
