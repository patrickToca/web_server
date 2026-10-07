// Package db provides PostgreSQL connection-pool configuration and
// construction for the mywebapp service.
package db

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

// -----------------------------------------------------------------------------
// Config
// -----------------------------------------------------------------------------

// Config holds the PostgreSQL connection and pool settings. The same
// struct is used for production and test databases; the difference is
// which set of environment variables populates it.
type Config struct {
	// Connection
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	SSLMode  string

	// Pool tuning
	MaxConns          int32
	MinConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration

	// Per-connection timeouts
	ConnectTimeout   time.Duration
	StatementTimeout time.Duration
}

// LoadConfig reads configuration from the environment, loading .env
// first if it can be found. It reads the DB_* variables.
func LoadConfig() *Config {
	loadDotEnv()

	cfg := &Config{
		Host:     getEnv("DB_HOST", "localhost"),
		Port:     getEnv("DB_PORT", "5432"),
		User:     getEnv("DB_USER", "postgres"),
		Password: getEnv("DB_PASSWORD", "postgres"),
		DBName:   getEnv("DB_NAME", "mywebapp"),
		SSLMode:  getEnv("DB_SSLMODE", "disable"),

		MaxConns:          int32(getEnvInt("DB_MAX_CONNS", 25)),
		MinConns:          int32(getEnvInt("DB_MIN_CONNS", 5)),
		MaxConnLifetime:   getEnvDuration("DB_MAX_CONN_LIFETIME", time.Hour),
		MaxConnIdleTime:   getEnvDuration("DB_MAX_CONN_IDLE_TIME", 30*time.Minute),
		HealthCheckPeriod: getEnvDuration("DB_HEALTH_CHECK_PERIOD", time.Minute),

		ConnectTimeout:   getEnvDuration("DB_CONNECT_TIMEOUT", 5*time.Second),
		StatementTimeout: getEnvDuration("DB_STATEMENT_TIMEOUT", 30*time.Second),
	}

	cfg.sanitize()
	return cfg
}

// LoadTestConfig reads the test database configuration from the
// environment (TEST_DB_* variables). It deliberately reads a
// different set of variables than LoadConfig so that a mistake in
// one cannot affect the other.
//
// It returns (nil, false) when:
//
//   - TEST_DB_NAME is empty, or
//   - the resulting config does not pass IsTestConfig.
//
// Callers should skip their tests in that case.
func LoadTestConfig() (*Config, bool) {
	loadDotEnv()

	name := stripQuotes(os.Getenv("TEST_DB_NAME"))
	if name == "" {
		return nil, false
	}

	cfg := &Config{
		Host:     getEnv("TEST_DB_HOST", "localhost"),
		Port:     getEnv("TEST_DB_PORT", "5432"),
		User:     getEnv("TEST_DB_USER", "postgres"),
		Password: getEnv("TEST_DB_PASSWORD", "postgres"),
		DBName:   name,
		SSLMode:  getEnv("TEST_DB_SSLMODE", "disable"),

		MaxConns:          int32(getEnvInt("TEST_DB_MAX_CONNS", 10)),
		MinConns:          int32(getEnvInt("TEST_DB_MIN_CONNS", 1)),
		MaxConnLifetime:   getEnvDuration("TEST_DB_MAX_CONN_LIFETIME", time.Hour),
		MaxConnIdleTime:   getEnvDuration("TEST_DB_MAX_CONN_IDLE_TIME", 10*time.Minute),
		HealthCheckPeriod: getEnvDuration("TEST_DB_HEALTH_CHECK_PERIOD", time.Minute),

		ConnectTimeout:   getEnvDuration("TEST_DB_CONNECT_TIMEOUT", 5*time.Second),
		StatementTimeout: getEnvDuration("TEST_DB_STATEMENT_TIMEOUT", 30*time.Second),
	}

	cfg.sanitize()

	if !IsTestConfig(cfg) {
		return nil, false
	}
	return cfg, true
}

// IsTestConfig reports whether cfg describes a database that is safe
// to truncate. It returns false when the test database name matches
// the production database name, which would mean the environment is
// misconfigured.
//
// The check is deliberately strict: safety over convenience. A
// developer who genuinely wants to run tests against a database
// named the same as production can override with
// TEST_DB_ALLOW_SHARED=1, but the default refuses.
func IsTestConfig(cfg *Config) bool {
	if cfg == nil || cfg.DBName == "" {
		return false
	}

	// Explicit escape hatch for unusual local setups. Never set this
	// in CI or in a committed .env.
	if os.Getenv("TEST_DB_ALLOW_SHARED") == "1" {
		return true
	}

	prod := stripQuotes(os.Getenv("DB_NAME"))
	if prod != "" && cfg.DBName == prod {
		return false
	}

	// Additionally require that the name looks like a test database.
	// This catches the case where DB_NAME is not set in the
	// environment at all, and TEST_DB_NAME is identical to what
	// production would use by default.
	lower := strings.ToLower(cfg.DBName)
	for _, marker := range []string{"test", "ci", "tmp", "scratch"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func (c *Config) sanitize() {
	if c.MaxConns < 1 {
		slog.Warn("DB_MAX_CONNS invalid, forcing default", "value", c.MaxConns, "forced", 25)
		c.MaxConns = 25
	}
	if c.MinConns < 0 {
		slog.Warn("DB_MIN_CONNS invalid, forcing default", "value", c.MinConns, "forced", 0)
		c.MinConns = 0
	}
	if c.MinConns > c.MaxConns {
		slog.Warn("DB_MIN_CONNS > DB_MAX_CONNS, forcing MinConns=5",
			"min", c.MinConns, "max", c.MaxConns)
		c.MinConns = 5
		if c.MinConns > c.MaxConns {
			c.MinConns = c.MaxConns
		}
	}
	if c.MaxConnLifetime <= 0 {
		c.MaxConnLifetime = time.Hour
	}
	if c.MaxConnIdleTime <= 0 {
		c.MaxConnIdleTime = 30 * time.Minute
	}
	if c.HealthCheckPeriod <= 0 {
		c.HealthCheckPeriod = time.Minute
	}
	if c.ConnectTimeout <= 0 {
		c.ConnectTimeout = 5 * time.Second
	}
	if c.StatementTimeout <= 0 {
		c.StatementTimeout = 30 * time.Second
	}
}

func (c *Config) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.DBName, c.SSLMode,
	)
}

func (c *Config) String() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s db=%s sslmode=%s pool[max=%d min=%d]",
		c.Host, c.Port, c.User, c.DBName, c.SSLMode, c.MaxConns, c.MinConns,
	)
}

// URL returns the connection string in URL form, suitable for
// libraries that parse it as a URL (for example golang-migrate).
// DSN() returns the key-value form that pgx accepts; the two are
// not interchangeable.
func (c *Config) URL() string {
	// percent-encode any reserved characters that might appear in
	// the password. The common cases (alphanumerics and simple
	// punctuation) pass through unmodified.
	user := url.QueryEscape(c.User)
	pass := url.QueryEscape(c.Password)
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		user, pass, c.Host, c.Port, c.DBName, c.SSLMode,
	)
}

// -----------------------------------------------------------------------------
// Pool construction
// -----------------------------------------------------------------------------

func NewPool(ctx context.Context, cfg *Config) (*pgxpool.Pool, error) {
	if cfg == nil {
		return nil, fmt.Errorf("db: nil config")
	}
	if cfg.MaxConns < 1 {
		return nil, fmt.Errorf("db: DB_MAX_CONNS must be >= 1, got %d", cfg.MaxConns)
	}
	if cfg.MinConns < 0 {
		return nil, fmt.Errorf("db: DB_MIN_CONNS must be >= 0, got %d", cfg.MinConns)
	}
	if cfg.MinConns > cfg.MaxConns {
		return nil, fmt.Errorf("db: DB_MIN_CONNS (%d) > DB_MAX_CONNS (%d)",
			cfg.MinConns, cfg.MaxConns)
	}

	connConfig, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("db: parse DSN: %w", err)
	}

	connConfig.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	connConfig.ConnConfig.RuntimeParams["application_name"] = "mywebapp"
	connConfig.ConnConfig.RuntimeParams["statement_timeout"] =
		strconv.FormatInt(cfg.StatementTimeout.Milliseconds(), 10)

	connConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET application_name = 'mywebapp'")
		return err
	}

	connConfig.MaxConns = cfg.MaxConns
	connConfig.MinConns = cfg.MinConns
	connConfig.MaxConnLifetime = cfg.MaxConnLifetime
	connConfig.MaxConnIdleTime = cfg.MaxConnIdleTime
	connConfig.HealthCheckPeriod = cfg.HealthCheckPeriod

	pool, err := pgxpool.NewWithConfig(ctx, connConfig)
	if err != nil {
		return nil, fmt.Errorf("db: create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}

	slog.Info("pgx pool ready",
		"host", cfg.Host,
		"port", cfg.Port,
		"user", cfg.User,
		"db", cfg.DBName,
		"sslmode", cfg.SSLMode,
		"max_conns", cfg.MaxConns,
		"min_conns", cfg.MinConns,
	)
	return pool, nil
}

// -----------------------------------------------------------------------------
// .env loading
// -----------------------------------------------------------------------------

// loadDotEnv walks up from the current working directory until it
// finds go.mod, then loads .env next to it. Called from LoadConfig
// and LoadTestConfig so that both paths see the same environment.
// Idempotent: calling it twice is harmless.
func loadDotEnv() {
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			envPath := filepath.Join(dir, ".env")
			if _, err := os.Stat(envPath); err == nil {
				if err := godotenv.Load(envPath); err != nil {
					slog.Warn("godotenv load failed", "path", envPath, "error", err)
				}
			}
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}

// -----------------------------------------------------------------------------
// Environment helpers
// -----------------------------------------------------------------------------

func stripQuotes(s string) string {
	s = strings.TrimSpace(s)
	for len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') ||
			(s[0] == '\'' && s[len(s)-1] == '\'') {
			s = strings.TrimSpace(s[1 : len(s)-1])
			continue
		}
		break
	}
	return s
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return stripQuotes(v)
	}
	return def
}

func getEnvInt(key string, def int) int {
	v := stripQuotes(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		slog.Warn("env var is not an integer, using default",
			"key", key, "value", v, "default", def)
		return def
	}
	return n
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	v := stripQuotes(os.Getenv(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		slog.Warn("env var is not a duration, using default",
			"key", key, "value", v, "default", def.String())
		return def
	}
	return d
}
