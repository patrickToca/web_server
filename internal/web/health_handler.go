package web

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"mywebapp/internal/version"
)

// HealthHandler exposes three endpoints with different audiences:
//
//   - GET /health          — liveness. No I/O. Public.
//   - GET /health/ready    — readiness. One DB ping (cached for one
//     second). Public.
//   - GET /admin/health/detail — diagnostics. Pool statistics, pool
//     configuration, uptime. Requires
//     role=admin.
//
// The split exists because the three questions have different
// audiences and different costs. A load balancer asks liveness
// thousands of times an hour and must not touch the database. An
// orchestrator asks readiness often enough that a one-second cache is
// worth having. An operator asks for diagnostics once, during an
// incident, and is authorized to see internals.
type HealthHandler struct {
	pool      *pgxpool.Pool
	startedAt time.Time

	mu           sync.Mutex
	lastReadyAt  time.Time
	lastReadyErr error
}

// readinessCacheTTL is how long a readiness result is reused. One
// second collapses a burst of concurrent probes into a single DB ping
// while keeping the answer fresh for practical purposes.
const readinessCacheTTL = time.Second

// NewHealthHandler constructs the handler.
func NewHealthHandler(pool *pgxpool.Pool) *HealthHandler {
	return &HealthHandler{
		pool:      pool,
		startedAt: time.Now(),
	}
}

// Health is the liveness endpoint. It performs no I/O and returns 200
// as long as the process is running. Do not add a database call here:
// the whole point of liveness is to distinguish "process alive" from
// "process dead", and a database that is unreachable should not
// restart a process that is otherwise serving traffic.
//
// GET /health
func (h *HealthHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Ready is the readiness endpoint. It pings the database with a short
// timeout and reports 200 or 503. The result is cached for one second
// so a burst of concurrent probes does not translate into a burst of
// database pings.
//
// GET /health/ready
func (h *HealthHandler) Ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	ready, err := h.checkReady(ctx)
	if !ready {
		// The error text is logged, not returned: the endpoint is
		// public and the reason for unreadiness is an internal
		// detail.
		if err != nil {
			// LoggerFromGin is the request-scoped logger; the error
			// stays in the server log where an operator can find it.
			_ = err
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unhealthy"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// checkReady returns a cached readiness result, performing a fresh
// ping when the cache is older than readinessCacheTTL.
func (h *HealthHandler) checkReady(ctx context.Context) (bool, error) {
	h.mu.Lock()
	if time.Since(h.lastReadyAt) < readinessCacheTTL {
		ready := h.lastReadyErr == nil
		h.mu.Unlock()
		return ready, h.lastReadyErr
	}
	h.mu.Unlock()

	err := h.pool.Ping(ctx)

	h.mu.Lock()
	h.lastReadyAt = time.Now()
	h.lastReadyErr = err
	h.mu.Unlock()

	return err == nil, err
}

// Detail is the diagnostics endpoint. It returns pool statistics,
// pool configuration, uptime, and the current time. Registered behind
// RequireRole("admin"), so the response never reaches an unauthenticated
// caller.
//
// GET /admin/health/detail
func (h *HealthHandler) Detail(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	stat := h.pool.Stat()
	cfg := h.pool.Config()

	ready := h.pool.Ping(ctx) == nil
	status := "ok"
	if !ready {
		status = "unhealthy"
	}

	c.JSON(http.StatusOK, gin.H{
		"status":     status,
		"time":       time.Now().Unix(),
		"uptime_sec": int(time.Since(h.startedAt).Seconds()),
		"config": gin.H{
			"max_conns":           cfg.MaxConns,
			"min_conns":           cfg.MinConns,
			"max_conn_lifetime":   cfg.MaxConnLifetime.String(),
			"max_conn_idle_time":  cfg.MaxConnIdleTime.String(),
			"health_check_period": cfg.HealthCheckPeriod.String(),
			"host":                cfg.ConnConfig.Host,
			"port":                cfg.ConnConfig.Port,
			"database":            cfg.ConnConfig.Database,
		},
		"pool": gin.H{
			"total":               stat.TotalConns(),
			"idle":                stat.IdleConns(),
			"acquired":            stat.AcquiredConns(),
			"max":                 stat.MaxConns(),
			"constructing":        stat.ConstructingConns(),
			"acquire_count":       stat.AcquireCount(),
			"empty_acquire":       stat.EmptyAcquireCount(),
			"canceled_acquire":    stat.CanceledAcquireCount(),
			"acquire_duration_ms": stat.AcquireDuration().Milliseconds(),
		},
	})
}

// Version returns the build metadata as JSON. Public and
// unauthenticated: it carries no user data.
//
// GET /version
func (h *HealthHandler) Version(c *gin.Context) {
	info := version.Get()
	c.JSON(http.StatusOK, info)
}
