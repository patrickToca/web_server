package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mywebapp/internal/repository"
)

type SessionService struct {
	queries *repository.Queries
	pool    *pgxpool.Pool
}

func NewSessionService(pool *pgxpool.Pool) *SessionService {
	return &SessionService{
		queries: repository.New(pool),
		pool:    pool,
	}
}

type CreateSessionParams struct {
	JTI       string
	UserID    int32
	ExpiresAt time.Time
	UserAgent string
	ClientIP  string
}

func (s *SessionService) Create(ctx context.Context, p CreateSessionParams) error {
	_, err := s.queries.CreateSession(ctx, repository.CreateSessionParams{
		Jti:       p.JTI,
		UserID:    p.UserID,
		ExpiresAt: pgtypeFromTime(p.ExpiresAt),
		UserAgent: nullableString(p.UserAgent),
		ClientIp:  nullableString(p.ClientIP),
	})
	return err
}

func (s *SessionService) IsActive(ctx context.Context, jti string) error {
	row, err := s.queries.GetSessionByJTI(ctx, jti)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return ErrSessionNotFound
		}
		return err
	}
	if row.RevokedAt.Valid {
		return ErrSessionRevoked
	}
	if row.ExpiresAt.Valid && row.ExpiresAt.Time.Before(time.Now()) {
		return ErrSessionExpired
	}
	return nil
}

func (s *SessionService) Revoke(ctx context.Context, jti string) error {
	return s.queries.RevokeSession(ctx, jti)
}

func (s *SessionService) RevokeAllForUser(ctx context.Context, userID int32) error {
	return s.queries.RevokeAllUserSessions(ctx, userID)
}

func (s *SessionService) CleanupExpired(ctx context.Context) error {
	return s.queries.DeleteExpiredSessions(ctx)
}

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionRevoked  = errors.New("session revoked")
	ErrSessionExpired  = errors.New("session expired")
)
