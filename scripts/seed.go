// Command seed populates the development database with demo users.
//
// It is idempotent: running it twice leaves the same rows in place,
// because every INSERT has an ON CONFLICT (username) clause that
// updates the mutable fields. The password hash is regenerated on
// every run, which means every invocation produces a fresh Argon2id
// hash even for an unchanged password. That is intentional — the
// alternative is to check whether the existing hash already matches
// the demo password, which adds a verify step for no benefit on a
// development fixture.
//
// Secrets are read from secrets/secrets.enc.yaml via
// internal/credentials. The program does not depend on the makefile
// having exported DB_PASSWORD, so it can be run directly with
// `go run scripts/seed.go` as well as through `make seed`.
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"mywebapp/internal/auth"
	"mywebapp/internal/credentials"
	"mywebapp/internal/logging"
	"mywebapp/pkg/db"
)

// demoPassword is the plaintext password hashed for every seeded
// account. It is a fixture, not a secret. The value is chosen to
// match what the test suite seeds (internal/testutil/seed.go) so a
// developer who switches between the two does not have to remember
// two passwords.
const demoPassword = "password123"

func main() {
	logger, cleanup, err := logging.Setup()
	if err != nil {
		slog.Error("logging setup failed", "error", err)
		os.Exit(1)
	}
	defer cleanup()

	// Load secrets before opening the pool. The credentials loader
	// reads secrets/secrets.enc.yaml, decrypts it with the age key,
	// and exports DB_PASSWORD and TEST_DB_PASSWORD into the process
	// environment. Without this call, db.LoadConfig reads an empty
	// DB_PASSWORD and Postgres rejects the connection with
	// "password authentication failed".
	//
	// In development the age key lives at
	// ~/.config/sops/age/keys.txt. In a container it is supplied via
	// the AGE_KEY environment variable. Either path works here.
	if err := credentials.Load(); err != nil {
		slog.Error("failed to load credentials", "error", err)
		os.Exit(1)
	}

	connectCtx, connectCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer connectCancel()

	pool, err := db.NewPool(connectCtx, db.LoadConfig())
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Hash the demo password once. Every seeded account shares the
	// same hash value. That is correct for a fixture and saves four
	// Argon2id operations per run. In a real seeding operation with
	// per-user passwords, each would be hashed independently.
	hashed, err := auth.HashPassword(demoPassword)
	if err != nil {
		logger.Error("failed to hash password", "error", err)
		os.Exit(1)
	}

	const sql = `
		INSERT INTO users (username, email, full_name, role, is_active, password_hash)
		VALUES
			('alice', 'alice@example.com', 'Alice Johnson',  'admin',     true,  $1),
			('bob',   'bob@example.com',   'Bob Smith',      'user',      true,  $1),
			('carol', 'carol@example.com', 'Carol Williams', 'moderator', true,  $1),
			('dave',  'dave@example.com',  'Dave Brown',     'user',      false, $1),
			('eve',   'eve@example.com',   'Eve Davis',      'user',      true,  $1)
		ON CONFLICT (username) DO UPDATE SET
			email         = EXCLUDED.email,
			full_name     = EXCLUDED.full_name,
			role          = EXCLUDED.role,
			is_active     = EXCLUDED.is_active,
			password_hash = EXCLUDED.password_hash,
			updated_at    = CURRENT_TIMESTAMP
	`

	logger.Info("seeding database")

	execCtx, execCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer execCancel()

	if _, err := pool.Exec(execCtx, sql, hashed); err != nil {
		logger.Error("failed to seed database", "error", err)
		os.Exit(1)
	}

	logger.Info("database seeded successfully",
		"demo_login", "alice@example.com",
		"demo_password", demoPassword)
}
