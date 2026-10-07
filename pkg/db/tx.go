package db

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mywebapp/internal/repository"
)

// WithTx runs fn inside a database transaction, committing on success and
// rolling back on error or panic.
//
// Usage:
//
//	err := db.WithTx(ctx, pool, func(q *repository.Queries) error {
//	    if _, err := q.CreateUser(ctx, params); err != nil {
//	        return err
//	    }
//	    return q.UpdateUserLastLogin(ctx, userID)
//	})
func WithTx(
	ctx context.Context,
	pool *pgxpool.Pool,
	fn func(q *repository.Queries) error,
) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}

	// Rollback is a no-op if Commit already succeeded.
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()

	if err := fn(repository.New(tx)); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}
