package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"mywebapp/internal/testutil"
)

func buildHealthRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	pool := testutil.NewPool(t)
	h := NewHealthHandler(pool)

	r := gin.New()
	r.GET("/health", h.Health)
	r.GET("/health/ready", h.Ready)
	r.GET("/admin/health/detail", h.Detail)
	return r
}

// openClosedPool returns a pool that has been closed. Any Ping on it
// fails immediately. It is a separate pool from the shared test pool,
// so closing it does not affect the rest of the package.
func openClosedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg := testutil.Config(t)
	pool, err := pgxpool.New(context.Background(), cfg.DSN())
	if err != nil {
		t.Fatalf("open secondary pool: %v", err)
	}
	pool.Close()
	return pool
}

func TestHealth_NoDatabaseCall(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewHealthHandler(openClosedPool(t))
	r := gin.New()
	r.GET("/health", h.Health)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))

	if w.Code != http.StatusOK {
		t.Errorf("liveness should return 200 regardless of DB state; got %d", w.Code)
	}
	if body := w.Body.String(); body != `{"status":"ok"}` {
		t.Errorf("liveness body = %q", body)
	}
}

func TestHealth_HasNoPoolInternals(t *testing.T) {
	r := buildHealthRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))

	body := w.Body.String()
	for _, forbidden := range []string{"pool", "idle", "acquired", "acquire_count"} {
		if contains(body, forbidden) {
			t.Errorf("liveness response must not contain %q: %s", forbidden, body)
		}
	}
}

func TestReady_ReportsOkWhenDatabaseReachable(t *testing.T) {
	r := buildHealthRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d\nbody: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); body != `{"status":"ok"}` {
		t.Errorf("readiness body = %q", body)
	}
}

func TestReady_Reports503WhenDatabaseUnreachable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewHealthHandler(openClosedPool(t))
	r := gin.New()
	r.GET("/health/ready", h.Ready)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
	if body := w.Body.String(); body != `{"status":"unhealthy"}` {
		t.Errorf("readiness body = %q", body)
	}
}

func TestReady_HasNoPoolInternals(t *testing.T) {
	r := buildHealthRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	body := w.Body.String()
	for _, forbidden := range []string{"pool", "idle", "acquired", "host", "database"} {
		if contains(body, forbidden) {
			t.Errorf("readiness response must not contain %q: %s", forbidden, body)
		}
	}
}

func TestDetail_ReturnsPoolStatistics(t *testing.T) {
	r := buildHealthRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/health/detail", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"pool", "config", "uptime_sec", "acquire_count"} {
		if !contains(body, want) {
			t.Errorf("detail response missing %q: %s", want, body)
		}
	}
}

// contains is a tiny helper so the tests do not depend on strings.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
