// Package testutil provides a self-contained test database harness.
//
// It reads TEST_DB_* environment variables, refuses to run against a
// database whose name matches DB_NAME, and hands out a pool plus a
// cleanup function. The pool is created once per test binary
// (per package, in effect) and reused across tests within that
// package.
//
// The package is deliberately separate from the production code so
// that nothing in the shipped binary imports it. It uses the same
// migration and seed logic the production path uses, so a schema
// change is exercised by the tests automatically.
package testutil

import (
	"context"
	"fmt"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mywebapp/internal/credentials"
	"mywebapp/pkg/db"
)

var (
	poolMu     sync.Mutex
	sharedPool *pgxpool.Pool
	sharedCfg  *db.Config
	sharedOK   bool

	// setupCalled records whether Setup has been invoked at all,
	// regardless of whether it succeeded in opening the pool. NewPool
	// uses it to distinguish two failure modes that otherwise produce
	// the same skip message:
	//
	//   - Setup was never called: the test package is missing its
	//     TestMain. That is a bug in the test suite, and NewPool fails
	//     the test rather than skipping it.
	//
	//   - Setup was called but the database was unavailable: the
	//     developer has not configured TEST_DB_*. That is an
	//     environment limitation, and NewPool skips.
	//
	// Without this distinction, a package that forgets TestMain skips
	// every test with a message about the environment, which sends the
	// reader looking in the wrong place.
	setupCalled bool
)

// Setup must be called from the TestMain of any package that uses
// testutil. It loads the test database configuration, resets the
// schema, applies migrations, and seeds the demo users.
//
// The returned cleanup function should be deferred from TestMain so
// the pool is closed after every test in the package has run.
//
// If no test database is available — either because TEST_DB_NAME is
// unset or because the name fails the safety check — Setup logs a
// message and returns a no-op cleanup. Tests that need the pool call
// NewPool, which skips them with a clear message.
func Setup() func() {
	poolMu.Lock()
	setupCalled = true
	poolMu.Unlock()

	// Load secrets from the SOPS file before reading TEST_DB_*.
	// The test database password is not in .env; it lives in
	// secrets.enc.yaml alongside the production password. Without
	// this call, os.Getenv("TEST_DB_PASSWORD") returns the empty
	// string and the pool fails to authenticate.
	//
	// In development this reads ~/.config/sops/age/keys.txt.
	// In CI it reads AGE_KEY. If neither is present, the loader
	// returns an error, the log line below explains why, and every
	// DB test skips. That is the correct failure mode: no
	// credentials means no tests, not wrong credentials means
	// mysterious authentication errors.
	if err := credentials.Load(); err != nil {
		log.Printf("testutil: credentials load failed: %v; DB tests will skip", err)
		return func() {}
	}

	cfg, ok := db.LoadTestConfig()
	if !ok || cfg == nil {
		log.Printf("testutil: no test database configured; DB-dependent tests will skip")
		return func() {}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DSN())
	if err != nil {
		log.Printf("testutil: could not open pool: %v", err)
		return func() {}
	}

	if err := pool.Ping(ctx); err != nil {
		log.Printf("testutil: test database unreachable: %v", err)
		pool.Close()
		return func() {}
	}

	if err := resetSchema(ctx, pool); err != nil {
		log.Printf("testutil: schema reset failed: %v", err)
		pool.Close()
		return func() {}
	}

	if err := runMigrations(ctx, cfg); err != nil {
		log.Printf("testutil: migration failed: %v", err)
		pool.Close()
		return func() {}
	}

	if err := Seed(ctx, pool); err != nil {
		log.Printf("testutil: seed failed: %v", err)
		pool.Close()
		return func() {}
	}

	poolMu.Lock()
	sharedPool = pool
	sharedCfg = cfg
	sharedOK = true
	poolMu.Unlock()

	log.Printf("testutil: test database ready (%s@%s:%s/%s)",
		cfg.User, cfg.Host, cfg.Port, cfg.DBName)

	return func() {
		poolMu.Lock()
		defer poolMu.Unlock()
		if sharedPool != nil {
			sharedPool.Close()
			sharedPool = nil
			sharedOK = false
		}
	}
}

// NewPool returns the shared test pool. If the test database was not
// available, it calls t.Skip with a clear message.
//
// If Setup was never called, NewPool fails the test instead of
// skipping it. A missing TestMain is a bug in the test suite, not an
// environment limitation, and the two cases must not be confused: a
// skipped test is a missing signal, and a failed test is a signal.
func NewPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	poolMu.Lock()
	p, ok := sharedPool, sharedOK
	called := setupCalled
	poolMu.Unlock()

	if !called {
		t.Fatal("testutil.Setup was not called from TestMain. " +
			"Add a TestMain to this package that calls testutil.Setup " +
			"(see internal/handler/main_test.go for the pattern).")
	}
	if !ok || p == nil {
		t.Skip("test database not available; set TEST_DB_* in .env")
	}
	return p
}

// Config returns the test database configuration.
func Config(t *testing.T) *db.Config {
	t.Helper()

	poolMu.Lock()
	defer poolMu.Unlock()

	if !setupCalled {
		t.Fatal("testutil.Setup was not called from TestMain. " +
			"Add a TestMain to this package that calls testutil.Setup " +
			"(see internal/handler/main_test.go for the pattern).")
	}
	if !sharedOK {
		t.Skip("test database not available; set TEST_DB_* in .env")
	}
	return sharedCfg
}

// resetSchema drops and recreates the public schema so each test
// binary starts from a clean database. This is safer than TRUNCATE
// because it removes any leftover state from previous runs,
// including tables added by a migration that has since been removed.
//
// The guard in LoadTestConfig has already ensured cfg.DBName is not
// the production database, so this is safe to run unconditionally.
func resetSchema(ctx context.Context, pool *pgxpool.Pool) error {
	stmts := []string{
		`DROP SCHEMA IF EXISTS public CASCADE`,
		`CREATE SCHEMA public`,
		`GRANT ALL ON SCHEMA public TO PUBLIC`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			return fmt.Errorf("%s: %w", s, err)
		}
	}
	return nil
}
