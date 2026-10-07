package testutil

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"mywebapp/pkg/db"
)

// runMigrations applies every migration in ./migrations to the test
// database. It is invoked once per test binary from Setup.
//
// The migrations directory is resolved relative to the repository
// root, which we locate by walking up from the current directory
// until go.mod is found. That is robust against being run from
// inside any package directory.
//
// migrate.New requires the database argument to be a URL, not the
// key-value form pgx accepts. cfg.URL() supplies the URL form;
// cfg.DSN() supplies the key-value form.
func runMigrations(ctx context.Context, cfg *db.Config) error {
	root, err := findRepoRoot()
	if err != nil {
		return err
	}
	migrationsPath := "file://" + root + "/migrations"

	m, err := migrate.New(migrationsPath, cfg.URL())
	if err != nil {
		return fmt.Errorf("migrate.New: %w", err)
	}
	defer func() {
		_, _ = m.Close()
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate.Up: %w", err)
	}
	return nil
}
