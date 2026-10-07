package testutil

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"mywebapp/internal/auth"
)

// Seed inserts the demo users tests depend on. It is idempotent:
// running it twice leaves the same rows in place.
//
// The emails and usernames match what scripts/seed.go inserts for
// the production demo data, so a test that assumes
// alice@example.com / password123 works identically here.
func Seed(ctx context.Context, pool *pgxpool.Pool) error {
	type seedUser struct {
		email    string
		username string
		fullName string
		role     string
		password string
	}

	users := []seedUser{
		{
			email:    "alice@example.com",
			username: "alice",
			fullName: "Alice Johnson",
			role:     "admin",
			password: "password123",
		},
		{
			email:    "bob@example.com",
			username: "bob",
			fullName: "Bob Smith",
			role:     "user",
			password: "password123",
		},
		{
			email:    "carol@example.com",
			username: "carol",
			fullName: "Carol White",
			role:     "moderator",
			password: "password123",
		},
	}

	for _, u := range users {
		hash, err := auth.HashPassword(u.password)
		if err != nil {
			return fmt.Errorf("hash password for %s: %w", u.email, err)
		}
		_, err = pool.Exec(ctx, `
			INSERT INTO users (email, password_hash, username, full_name, is_active, role)
			VALUES ($1, $2, $3, $4, true, $5)
			ON CONFLICT (email) DO NOTHING
		`, u.email, hash, u.username, u.fullName, u.role)
		if err != nil {
			return fmt.Errorf("insert %s: %w", u.email, err)
		}
	}
	return nil
}
