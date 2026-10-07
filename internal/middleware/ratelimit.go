// Package middleware provides reusable Gin middleware for the mywebapp
// HTTP layer: gzip compression, per-client rate limiting, request IDs,
// access logging, security headers, and CSRF protection.
package middleware

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// clientLimiter is a token-bucket limiter plus the last time it was
// seen, used to evict idle clients from the map.
type clientLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiter is a per-client-IP token bucket. It is safe for
// concurrent use. Buckets are garbage-collected after ttl of
// inactivity.
//
// Keying uses Gin's ClientIP(), which honours the trusted-proxy
// configuration set on the engine. Callers MUST call
// router.SetTrustedProxies(...) before serving traffic: Gin's default
// trusts every proxy, which makes ClientIP() return the value the
// client supplied in X-Forwarded-For.
type RateLimiter struct {
	mu      sync.Mutex
	clients map[string]*clientLimiter

	// maxClients caps the number of tracked keys. When the map is at
	// capacity, new clients share a single fallback bucket so the
	// limiter cannot be turned into a memory-exhaustion vector by
	// feeding it unique keys. Zero means no cap.
	maxClients int

	r   rate.Limit // tokens added per second
	b   int        // burst size (max tokens)
	ttl time.Duration

	stop chan struct{}
	once sync.Once
}

// NewRateLimiter builds a limiter allowing rps requests/second with a
// burst of burst. Idle clients are evicted after ttl. maxClients caps
// the map; pass 0 for no cap.
func NewRateLimiter(rps float64, burst int, ttl time.Duration, maxClients int) *RateLimiter {
	rl := &RateLimiter{
		clients:    make(map[string]*clientLimiter),
		r:          rate.Limit(rps),
		b:          burst,
		ttl:        ttl,
		maxClients: maxClients,
		stop:       make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

// Stop terminates the background cleanup goroutine. Call from tests or
// from a shutdown hook so the goroutine does not leak.
func (rl *RateLimiter) Stop() {
	rl.once.Do(func() { close(rl.stop) })
}

// clientKey returns a stable key for the requester.
//
// It relies on Gin's ClientIP(), which resolves the client address
// according to the engine's trusted-proxy configuration. Direct parsing
// of X-Forwarded-For or X-Real-IP is deliberately avoided: those headers
// are attacker-controlled unless a trusted proxy has been declared.
//
// IPv6 addresses are keyed by /64 prefix, so a single client cannot
// obtain a fresh bucket by rotating through its allocation.
func (rl *RateLimiter) clientKey(c *gin.Context) string {
	ip := c.ClientIP()
	if ip == "" {
		return "unknown"
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ip
	}
	if v4 := parsed.To4(); v4 != nil {
		return v4.String()
	}
	// IPv6: mask to /64.
	masked := parsed.Mask(net.CIDRMask(64, 128))
	return masked.String()
}

// fallbackKey is used when the map is at capacity. New clients beyond
// the cap share one bucket, so the limiter still applies pressure
// instead of silently allowing unlimited new keys.
const fallbackKey = "__over_capacity__"

func (rl *RateLimiter) getLimiter(key string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cl, ok := rl.clients[key]
	if !ok {
		if rl.maxClients > 0 && len(rl.clients) >= rl.maxClients {
			// Map is full. Route the request to the shared fallback
			// bucket instead of allocating a new entry.
			key = fallbackKey
			cl, ok = rl.clients[key]
			if !ok {
				cl = &clientLimiter{limiter: rate.NewLimiter(rl.r, rl.b)}
				rl.clients[key] = cl
			}
		} else {
			cl = &clientLimiter{limiter: rate.NewLimiter(rl.r, rl.b)}
			rl.clients[key] = cl
		}
	}
	cl.lastSeen = time.Now()
	return cl.limiter
}

// retryAfterSeconds returns the number of seconds a client should wait
// before the next attempt can succeed, given the limiter's rate.
func (rl *RateLimiter) retryAfterSeconds() int {
	if rl.r <= 0 {
		return 60
	}
	// One token costs 1/r seconds. Round up to the next whole second.
	secs := int(1.0 / float64(rl.r))
	if secs < 1 {
		secs = 1
	}
	return secs
}

// Middleware returns the gin handler.
//
// On limit exceeded it responds 429 with an accurate Retry-After and a
// body appropriate for the caller: a small JSON object for HTMX and API
// requests, an HTML fragment for browser navigation.
func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := rl.clientKey(c)
		if !rl.getLimiter(key).Allow() {
			retry := rl.retryAfterSeconds()
			c.Header("Retry-After", fmt.Sprintf("%d", retry))
			c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", rl.b))

			if isHTMX(c) {
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error": "Too many requests. Please slow down.",
				})
				return
			}
			if strings.HasPrefix(c.Request.URL.Path, "/api/") {
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error": "rate limit exceeded",
				})
				return
			}
			// Browser navigation: a minimal HTML fragment.
			c.Header("Content-Type", "text/html; charset=utf-8")
			c.Status(http.StatusTooManyRequests)
			_, _ = c.Writer.WriteString(
				`<div class="p-4 bg-yellow-50 border border-yellow-200 rounded">` +
					`<p>Too many requests. Please wait a moment and try again.</p>` +
					`</div>`)
			return
		}
		c.Next()
	}
}

// cleanupLoop evicts idle clients so the map cannot grow without bound.
// It exits when Stop is called.
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.ttl)
	defer ticker.Stop()
	for {
		select {
		case <-rl.stop:
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-rl.ttl)
			rl.mu.Lock()
			for k, v := range rl.clients {
				if v.lastSeen.Before(cutoff) {
					delete(rl.clients, k)
				}
			}
			rl.mu.Unlock()
		}
	}
}

// isHTMX duplicates web.IsHTMX to avoid an import cycle between the
// middleware and web packages.
func isHTMX(c *gin.Context) bool {
	return c.GetHeader("HX-Request") != ""
}
