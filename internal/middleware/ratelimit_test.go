package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// newTestLimiter constructs a limiter with a large maxClients cap so
// the map-size behaviour does not interfere with tests that are
// focused on the token bucket. Cleanup is registered automatically.
func newTestLimiter(t *testing.T, rps float64, burst int, ttl time.Duration) *RateLimiter {
	t.Helper()
	rl := NewRateLimiter(rps, burst, ttl, 1000)
	t.Cleanup(rl.Stop)
	return rl
}

// buildRateLimitRouter builds a router with the limiter and a trivial
// handler.
func buildRateLimitRouter(rl *RateLimiter) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(rl.Middleware())
	r.GET("/ok", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return r
}

// -----------------------------------------------------------------------------
// Token bucket behaviour
// -----------------------------------------------------------------------------

func TestRateLimiter_AllowsWithinBurst(t *testing.T) {
	rl := newTestLimiter(t, 10, 5, time.Minute)
	r := buildRateLimitRouter(rl)

	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/ok", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: got %d", i+1, w.Code)
		}
	}
}

func TestRateLimiter_BlocksWhenBurstExhausted(t *testing.T) {
	rl := newTestLimiter(t, 0.001, 2, time.Minute)
	r := buildRateLimitRouter(rl)

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/ok", nil)
		req.RemoteAddr = "10.0.0.2:1234"
		r.ServeHTTP(w, req)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.RemoteAddr = "10.0.0.2:1234"
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After")
	}
	if w.Header().Get("X-RateLimit-Limit") == "" {
		t.Error("expected X-RateLimit-Limit")
	}
}

func TestRateLimiter_RetryAfterReflectsRate(t *testing.T) {
	// A 5-per-minute limiter refills one token every 12 seconds.
	rl := newTestLimiter(t, 5.0/60.0, 1, 15*time.Minute)
	r := buildRateLimitRouter(rl)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.RemoteAddr = "10.0.1.1:1234"
	r.ServeHTTP(w, req)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.RemoteAddr = "10.0.1.1:1234"
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "12" {
		t.Errorf("Retry-After = %q, want \"12\"", got)
	}
}

func TestRateLimiter_RefillsOverTime(t *testing.T) {
	rl := newTestLimiter(t, 50, 1, time.Minute)
	r := buildRateLimitRouter(rl)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.RemoteAddr = "10.0.0.50:1234"
	r.ServeHTTP(w, req)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.RemoteAddr = "10.0.0.50:1234"
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}

	time.Sleep(100 * time.Millisecond)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.RemoteAddr = "10.0.0.50:1234"
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 after refill, got %d", w.Code)
	}
}

func TestRateLimiter_SeparateBucketsPerClient(t *testing.T) {
	rl := newTestLimiter(t, 0.001, 1, time.Minute)
	r := buildRateLimitRouter(rl)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.RemoteAddr = "10.0.0.10:1234"
	r.ServeHTTP(w, req)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.RemoteAddr = "10.0.0.11:1234"
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("client B limited by A's bucket; got %d", w.Code)
	}
}

// -----------------------------------------------------------------------------
// Response shape
// -----------------------------------------------------------------------------

func TestRateLimiter_HTMXGetsJSON(t *testing.T) {
	rl := newTestLimiter(t, 0.001, 1, time.Minute)
	r := buildRateLimitRouter(rl)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.RemoteAddr = "10.0.0.3:1234"
	r.ServeHTTP(w, req)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.RemoteAddr = "10.0.0.3:1234"
	req.Header.Set("HX-Request", "true")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "json") {
		t.Errorf("HTMX 429 content-type = %q", ct)
	}
}

func TestRateLimiter_APIGetsJSON(t *testing.T) {
	rl := newTestLimiter(t, 0.001, 1, time.Minute)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(rl.Middleware())
	r.GET("/api/v1/anything", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil)
	req.RemoteAddr = "10.0.0.30:1234"
	r.ServeHTTP(w, req)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil)
	req.RemoteAddr = "10.0.0.30:1234"
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "json") {
		t.Errorf("API 429 content-type = %q", ct)
	}
}

func TestRateLimiter_BrowserGetsHTML(t *testing.T) {
	rl := newTestLimiter(t, 0.001, 1, time.Minute)
	r := buildRateLimitRouter(rl)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.RemoteAddr = "10.0.0.4:1234"
	r.ServeHTTP(w, req)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.RemoteAddr = "10.0.0.4:1234"
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("browser 429 content-type = %q, want text/html", ct)
	}
	if !strings.Contains(w.Body.String(), "Too many requests") {
		t.Errorf("browser 429 body = %q", w.Body.String())
	}
}

// -----------------------------------------------------------------------------
// Trusted proxy and header spoofing
// -----------------------------------------------------------------------------

func TestRateLimiter_HonoursXForwardedForViaTrustedProxy(t *testing.T) {
	// When the immediate peer is a trusted proxy, Gin uses the
	// left-most X-Forwarded-For entry as the client address.
	rl := newTestLimiter(t, 0.001, 1, time.Minute)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	if err := r.SetTrustedProxies([]string{"127.0.0.1/32"}); err != nil {
		t.Fatalf("SetTrustedProxies: %v", err)
	}
	r.RemoteIPHeaders = []string{"X-Forwarded-For"}
	r.Use(rl.Middleware())
	r.GET("/ok", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	// First request from XFF=1.2.3.4.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first request: %d", w.Code)
	}

	// Same XFF, second request: bucket exhausted.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("same XFF should share the bucket; got %d", w.Code)
	}

	// Different XFF: fresh bucket.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "5.6.7.8")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("different XFF should get a fresh bucket; got %d", w.Code)
	}
}

func TestRateLimiter_IgnoresForwardedForFromUntrustedPeer(t *testing.T) {
	// When the immediate peer is not a trusted proxy, Gin ignores
	// X-Forwarded-For. The bucket is keyed by the socket address, so
	// rotating the header value does not grant a fresh bucket.
	rl := newTestLimiter(t, 0.001, 1, time.Minute)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	if err := r.SetTrustedProxies(nil); err != nil {
		t.Fatalf("SetTrustedProxies: %v", err)
	}
	r.RemoteIPHeaders = []string{"X-Forwarded-For"}
	r.Use(rl.Middleware())
	r.GET("/ok", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.RemoteAddr = "203.0.113.5:1234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first request: got %d", w.Code)
	}

	// Same socket, different XFF. The header is ignored, so this
	// shares the first request's bucket and must be limited.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.RemoteAddr = "203.0.113.5:1234"
	req.Header.Set("X-Forwarded-For", "5.6.7.8")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 (XFF ignored without trusted proxy), got %d", w.Code)
	}
}

// -----------------------------------------------------------------------------
// Map size cap
// -----------------------------------------------------------------------------

func TestRateLimiter_MapSizeCapRoutesToFallback(t *testing.T) {
	// With maxClients=2, the third distinct key is routed to a shared
	// fallback bucket. The first fallback request succeeds; the
	// second, from yet another key, is limited because the fallback
	// bucket is exhausted.
	rl := NewRateLimiter(0.001, 1, time.Minute, 2)
	t.Cleanup(rl.Stop)
	r := buildRateLimitRouter(rl)

	send := func(addr string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/ok", nil)
		req.RemoteAddr = addr
		r.ServeHTTP(w, req)
		return w.Code
	}

	if got := send("10.0.0.1:1234"); got != http.StatusOK {
		t.Fatalf("first key: %d", got)
	}
	if got := send("10.0.0.2:1234"); got != http.StatusOK {
		t.Fatalf("second key: %d", got)
	}
	if got := send("10.0.0.3:1234"); got != http.StatusOK {
		t.Fatalf("third key (first fallback use): %d", got)
	}
	if got := send("10.0.0.4:1234"); got != http.StatusTooManyRequests {
		t.Errorf("fourth key should hit the exhausted fallback; got %d", got)
	}
}

// -----------------------------------------------------------------------------
// Goroutine lifecycle
// -----------------------------------------------------------------------------

func TestRateLimiter_StopIsIdempotent(t *testing.T) {
	rl := NewRateLimiter(1, 1, time.Minute, 0)
	rl.Stop()
	rl.Stop() // must not panic
}
