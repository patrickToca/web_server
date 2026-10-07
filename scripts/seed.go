package main

import (
	"context"
	"log/slog"
	"os"

	"mywebapp/internal/auth"
	"mywebapp/internal/logging"
	"mywebapp/pkg/db"
)

func main() {
	logger, cleanup, err := logging.Setup()
	if err != nil {
		slog.Error("logging setup failed", "error", err)
		os.Exit(1)
	}
	defer cleanup()

	config := db.LoadConfig()
	pool, err := db.NewPool(context.Background(), config)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	hashed, err := auth.HashPassword("password123")
	if err != nil {
		logger.Error("failed to hash password", "error", err)
		os.Exit(1)
	}

	sql := `
	INSERT INTO users (username, email, full_name, role, is_active, password_hash) VALUES
		('alice', 'alice@example.com', 'Alice Johnson', 'admin', true, $1),
		('bob', 'bob@example.com', 'Bob Smith', 'user', true, $1),
		('carol', 'carol@example.com', 'Carol Williams', 'moderator', true, $1),
		('dave', 'dave@example.com', 'Dave Brown', 'user', false, $1),
		('eve', 'eve@example.com', 'Eve Davis', 'user', true, $1)
	ON CONFLICT (username) DO UPDATE SET
		email = EXCLUDED.email,
		full_name = EXCLUDED.full_name,
		role = EXCLUDED.role,
		is_active = EXCLUDED.is_active,
		password_hash = EXCLUDED.password_hash,
		updated_at = CURRENT_TIMESTAMP;
	`

	logger.Info("seeding database", "password", "password123")

	if _, err := pool.Exec(context.Background(), sql, hashed); err != nil {
		logger.Error("failed to seed database", "error", err)
		os.Exit(1)
	}

	logger.Info("database seeded successfully",
		"demo_login", "alice@example.com",
		"demo_password", "password123")
}
