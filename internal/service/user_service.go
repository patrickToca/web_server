package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"mywebapp/internal/auth"
	"mywebapp/internal/domain"
	"mywebapp/internal/repository"
	"mywebapp/pkg/db"
)

type UserService struct {
	queries *repository.Queries
	pool    *pgxpool.Pool
}

// revokeAllSessionsHook, when non-nil, replaces the call to
// q.RevokeAllUserSessions inside DeleteUser and UpdateUserRole. It
// exists so the rollback tests can force the revoke step to fail and
// assert that the user mutation is rolled back.
//
// Nil in production. The hook takes the *repository.Queries from the
// active transaction so a test that wants to inspect the transaction
// state can do so.
var revokeAllSessionsHook func(ctx context.Context, q *repository.Queries, userID int32) error

// revokeAllSessions calls the hook when one is installed, otherwise
// calls the real query. Kept as a method so the call sites read
// uniformly and so a future change (e.g. logging, metrics) has a
// single place to live.
func (s *UserService) revokeAllSessions(ctx context.Context, q *repository.Queries, userID int32) error {
	if revokeAllSessionsHook != nil {
		return revokeAllSessionsHook(ctx, q, userID)
	}
	return q.RevokeAllUserSessions(ctx, userID)
}

// NewUserService keeps the existing signature working by accepting only
// queries, but if you want transactions you should use
// NewUserServiceWithPool.
func NewUserService(queries *repository.Queries) *UserService {
	return &UserService{queries: queries}
}

// NewUserServiceWithPool is the preferred constructor: it enables WithTx.
func NewUserServiceWithPool(pool *pgxpool.Pool) *UserService {
	return &UserService{
		queries: repository.New(pool),
		pool:    pool,
	}
}

// WithTx executes fn inside a transaction. Returns an error if the
// service was constructed without a pool.
func (s *UserService) WithTx(
	ctx context.Context,
	fn func(q *repository.Queries) error,
) error {
	if s.pool == nil {
		return errors.New("service has no pool; use NewUserServiceWithPool")
	}
	return db.WithTx(ctx, s.pool, fn)
}

// =============================================================================
// Mutations
// =============================================================================

// CreateUser creates a new account. The role is always "user": the
// request DTO cannot carry a role, and this method does not accept one.
// Promotion happens through UpdateUserRole, which is admin-gated.
func (s *UserService) CreateUser(ctx context.Context, req domain.CreateUserRequest) (*domain.User, error) {
	hashedPassword, err := auth.HashPassword(req.Password)
	if err != nil {
		return nil, err
	}

	fullName := pgtype.Text{String: req.FullName, Valid: true}
	role := pgtype.Text{String: "user", Valid: true}
	isActive := pgtype.Bool{Bool: true, Valid: true}

	user, err := s.queries.CreateUser(ctx, repository.CreateUserParams{
		Email:        req.Email,
		PasswordHash: hashedPassword,
		Username:     req.Username,
		FullName:     fullName,
		Role:         role,
		IsActive:     isActive,
	})
	if err != nil {
		if mapped := mapPgConflict(err); mapped != nil {
			return nil, mapped
		}
		return nil, err
	}

	return s.mapCreateUserRowToDomain(&user), nil
}

// CreateUserAndTouchLogin is an example of a multi-statement operation
// that benefits from a transaction. Replace or extend as needed.
func (s *UserService) CreateUserAndTouchLogin(
	ctx context.Context,
	req domain.CreateUserRequest,
) (*domain.User, error) {
	var created *domain.User

	err := s.WithTx(ctx, func(q *repository.Queries) error {
		hashed, err := auth.HashPassword(req.Password)
		if err != nil {
			return err
		}

		row, err := q.CreateUser(ctx, repository.CreateUserParams{
			Email:        req.Email,
			PasswordHash: hashed,
			Username:     req.Username,
			FullName:     pgtype.Text{String: req.FullName, Valid: true},
			Role:         pgtype.Text{String: "user", Valid: true},
			IsActive:     pgtype.Bool{Bool: true, Valid: true},
		})
		if err != nil {
			if mapped := mapPgConflict(err); mapped != nil {
				return mapped
			}
			return err
		}

		if err := q.UpdateUserLastLogin(ctx, row.ID); err != nil {
			return err
		}

		created = s.mapCreateUserRowToDomain(&row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// UpdateUser updates the mutable profile fields of an account. The
// role is not among them: this method preserves the existing role
// value. A separate, admin-only UpdateUserRole handles promotion and
// demotion.
//
// A NULL or empty existing role is coerced to "user" before being
// written back, so an account with a legacy NULL role cannot propagate
// it further. The column is nullable in the schema, so this is a real
// (if rare) path.
func (s *UserService) UpdateUser(ctx context.Context, id int32, req domain.UpdateUserRequest) (*domain.User, error) {
	existing, err := s.queries.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, err
	}

	fullName := pgtype.Text{String: req.FullName, Valid: true}

	// Preserve the current role. Coerce NULL/empty to "user" so a
	// legacy row cannot lock itself out of role checks.
	role := existing.Role
	if !role.Valid || role.String == "" {
		role = pgtype.Text{String: "user", Valid: true}
	}

	isActive := pgtype.Bool{Bool: true, Valid: true}
	if req.IsActive != nil {
		isActive.Bool = *req.IsActive
	}

	user, err := s.queries.UpdateUser(ctx, repository.UpdateUserParams{
		ID:       id,
		Username: req.Username,
		FullName: fullName,
		Email:    req.Email,
		Role:     role,
		IsActive: isActive,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		if mapped := mapPgConflict(err); mapped != nil {
			return nil, mapped
		}
		return nil, err
	}

	deactivated := existing.IsActive.Bool && !isActive.Bool
	if deactivated {
		if err := s.queries.RevokeAllUserSessions(ctx, id); err != nil {
			slog.Warn("failed to revoke sessions after user deactivation",
				"user_id", id, "error", err)
		}
	}

	return s.mapUpdateUserRowToDomain(&user), nil
}

// UpdateUserRole changes a user's role. This is the only path in the
// service layer that writes a non-default role. It is admin-gated at
// the route layer.
//
// The role change and the session revocation run in a single
// transaction. If the revoke fails, the role change is rolled back:
// a demoted admin must not retain a live refresh token, and a partial
// commit would leave exactly that state.
func (s *UserService) UpdateUserRole(ctx context.Context, id int32, role string) (*domain.User, error) {
	if !domain.IsValidRole(role) {
		return nil, domain.ErrInvalidInput
	}

	var updated *repository.UpdateUserRoleRow

	err := s.WithTx(ctx, func(q *repository.Queries) error {
		row, err := q.UpdateUserRole(ctx, repository.UpdateUserRoleParams{
			ID:   id,
			Role: pgtype.Text{String: role, Valid: true},
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
				return domain.ErrUserNotFound
			}
			return err
		}

		if err := s.revokeAllSessions(ctx, q, id); err != nil {
			return fmt.Errorf("revoke sessions after role change: %w", err)
		}

		updated = &row
		return nil
	})
	if err != nil {
		return nil, err
	}

	return s.mapUpdateUserRoleRowToDomain(updated), nil
}

// DeleteUser removes an account and revokes all of its sessions in a
// single transaction.
func (s *UserService) DeleteUser(ctx context.Context, id int32) error {
	return s.WithTx(ctx, func(q *repository.Queries) error {
		rows, err := q.DeleteUser(ctx, id)
		if err != nil {
			return fmt.Errorf("delete user: %w", err)
		}
		if rows == 0 {
			return domain.ErrUserNotFound
		}
		if err := s.revokeAllSessions(ctx, q, id); err != nil {
			return fmt.Errorf("revoke sessions after delete: %w", err)
		}
		return nil
	})
}

func (s *UserService) UpdateUserLastLogin(ctx context.Context, id int32) error {
	return s.queries.UpdateUserLastLogin(ctx, id)
}

// =============================================================================
// Queries
// =============================================================================

func (s *UserService) GetUserByID(ctx context.Context, id int32) (*domain.User, error) {
	user, err := s.queries.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, err
	}
	return s.mapGetUserByIDRowToDomain(&user), nil
}

func (s *UserService) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	user, err := s.queries.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, err
	}
	return s.mapGetUserByEmailRowToDomain(&user), nil
}

func (s *UserService) ListUsers(ctx context.Context, limit, offset int32) ([]domain.User, error) {
	users, err := s.queries.ListUsers(ctx, repository.ListUsersParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, err
	}

	result := make([]domain.User, len(users))
	for i, user := range users {
		result[i] = *s.mapListUsersRowToDomain(&user)
	}
	return result, nil
}

func (s *UserService) ListUsersByID(ctx context.Context, limit, offset int32) ([]domain.User, error) {
	users, err := s.queries.ListUsersByID(ctx, repository.ListUsersByIDParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, err
	}

	result := make([]domain.User, len(users))
	for i, user := range users {
		result[i] = *s.mapListUsersByIDRowToDomain(&user)
	}
	return result, nil
}

func (s *UserService) SearchUsers(ctx context.Context, search string, limit, offset int32) ([]domain.User, error) {
	users, err := s.queries.SearchUsers(ctx, repository.SearchUsersParams{
		Column1: pgtype.Text{String: search, Valid: true},
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		return nil, err
	}

	result := make([]domain.User, len(users))
	for i, user := range users {
		result[i] = *s.mapSearchUsersRowToDomain(&user)
	}
	return result, nil
}

func (s *UserService) CountUsers(ctx context.Context) (int64, error) {
	return s.queries.CountUsers(ctx)
}

func (s *UserService) CountActiveUsers(ctx context.Context) (int64, error) {
	return s.queries.CountActiveUsers(ctx)
}

// =============================================================================
// Helpers
// =============================================================================

func mapPgConflict(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return nil
	}
	if pgErr.Code != "23505" {
		return nil
	}
	constraint := strings.ToLower(pgErr.ConstraintName)
	switch {
	case strings.Contains(constraint, "email"):
		return domain.ErrEmailExists
	case strings.Contains(constraint, "username"):
		return domain.ErrUsernameExists
	}
	return err
}

func timestamptzToUTCTime(t pgtype.Timestamptz) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time.UTC()
}

func timestamptzToUTCTimePtr(t pgtype.Timestamptz) *domain.UTCTime {
	if !t.Valid {
		return nil
	}
	utcTime := domain.UTCTime(t.Time.UTC())
	return &utcTime
}

func pgtypeFromTime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: !t.IsZero()}
}

func nullableString(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

// =============================================================================
// Row -> Domain mappers
// =============================================================================

func (s *UserService) mapCreateUserRowToDomain(row *repository.CreateUserRow) *domain.User {
	return &domain.User{
		ID:           row.ID,
		Email:        row.Email,
		Username:     row.Username,
		FullName:     row.FullName.String,
		PasswordHash: row.PasswordHash,
		IsActive:     row.IsActive.Bool,
		Role:         row.Role.String,
		LastLogin:    timestamptzToUTCTimePtr(row.LastLogin),
		CreatedAt:    domain.UTCTime(timestamptzToUTCTime(row.CreatedAt)),
		UpdatedAt:    domain.UTCTime(timestamptzToUTCTime(row.UpdatedAt)),
	}
}

func (s *UserService) mapGetUserByIDRowToDomain(row *repository.GetUserByIDRow) *domain.User {
	return &domain.User{
		ID:           row.ID,
		Email:        row.Email,
		Username:     row.Username,
		FullName:     row.FullName.String,
		PasswordHash: row.PasswordHash,
		IsActive:     row.IsActive.Bool,
		Role:         row.Role.String,
		LastLogin:    timestamptzToUTCTimePtr(row.LastLogin),
		CreatedAt:    domain.UTCTime(timestamptzToUTCTime(row.CreatedAt)),
		UpdatedAt:    domain.UTCTime(timestamptzToUTCTime(row.UpdatedAt)),
	}
}

func (s *UserService) mapGetUserByEmailRowToDomain(row *repository.GetUserByEmailRow) *domain.User {
	return &domain.User{
		ID:           row.ID,
		Email:        row.Email,
		Username:     row.Username,
		FullName:     row.FullName.String,
		PasswordHash: row.PasswordHash,
		IsActive:     row.IsActive.Bool,
		Role:         row.Role.String,
		LastLogin:    timestamptzToUTCTimePtr(row.LastLogin),
		CreatedAt:    domain.UTCTime(timestamptzToUTCTime(row.CreatedAt)),
		UpdatedAt:    domain.UTCTime(timestamptzToUTCTime(row.UpdatedAt)),
	}
}

func (s *UserService) mapUpdateUserRowToDomain(row *repository.UpdateUserRow) *domain.User {
	return &domain.User{
		ID:           row.ID,
		Email:        row.Email,
		Username:     row.Username,
		FullName:     row.FullName.String,
		PasswordHash: row.PasswordHash,
		IsActive:     row.IsActive.Bool,
		Role:         row.Role.String,
		LastLogin:    timestamptzToUTCTimePtr(row.LastLogin),
		CreatedAt:    domain.UTCTime(timestamptzToUTCTime(row.CreatedAt)),
		UpdatedAt:    domain.UTCTime(timestamptzToUTCTime(row.UpdatedAt)),
	}
}

// mapUpdateUserRoleRowToDomain handles the row returned by the
// :one UpdateUserRole query.
func (s *UserService) mapUpdateUserRoleRowToDomain(row *repository.UpdateUserRoleRow) *domain.User {
	return &domain.User{
		ID:           row.ID,
		Email:        row.Email,
		Username:     row.Username,
		FullName:     row.FullName.String,
		PasswordHash: row.PasswordHash,
		IsActive:     row.IsActive.Bool,
		Role:         row.Role.String,
		LastLogin:    timestamptzToUTCTimePtr(row.LastLogin),
		CreatedAt:    domain.UTCTime(timestamptzToUTCTime(row.CreatedAt)),
		UpdatedAt:    domain.UTCTime(timestamptzToUTCTime(row.UpdatedAt)),
	}
}

func (s *UserService) mapListUsersRowToDomain(row *repository.ListUsersRow) *domain.User {
	return &domain.User{
		ID:        row.ID,
		Email:     row.Email,
		Username:  row.Username,
		FullName:  row.FullName.String,
		IsActive:  row.IsActive.Bool,
		Role:      row.Role.String,
		LastLogin: timestamptzToUTCTimePtr(row.LastLogin),
		CreatedAt: domain.UTCTime(timestamptzToUTCTime(row.CreatedAt)),
		UpdatedAt: domain.UTCTime(timestamptzToUTCTime(row.UpdatedAt)),
	}
}

func (s *UserService) mapListUsersByIDRowToDomain(row *repository.ListUsersByIDRow) *domain.User {
	return &domain.User{
		ID:        row.ID,
		Email:     row.Email,
		Username:  row.Username,
		FullName:  row.FullName.String,
		IsActive:  row.IsActive.Bool,
		Role:      row.Role.String,
		LastLogin: timestamptzToUTCTimePtr(row.LastLogin),
		CreatedAt: domain.UTCTime(timestamptzToUTCTime(row.CreatedAt)),
		UpdatedAt: domain.UTCTime(timestamptzToUTCTime(row.UpdatedAt)),
	}
}

func (s *UserService) mapSearchUsersRowToDomain(row *repository.SearchUsersRow) *domain.User {
	return &domain.User{
		ID:        row.ID,
		Email:     row.Email,
		Username:  row.Username,
		FullName:  row.FullName.String,
		IsActive:  row.IsActive.Bool,
		Role:      row.Role.String,
		LastLogin: timestamptzToUTCTimePtr(row.LastLogin),
		CreatedAt: domain.UTCTime(timestamptzToUTCTime(row.CreatedAt)),
		UpdatedAt: domain.UTCTime(timestamptzToUTCTime(row.UpdatedAt)),
	}
}
