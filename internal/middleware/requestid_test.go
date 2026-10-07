package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func buildRequestIDRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestID(slog.Default()))
	r.GET("/echo", func(c *gin.Context) {
		c.String(http.StatusOK, RequestIDFromContext(c.Request.Context()))
	})
	return r
}

func TestRequestID_GeneratesWhenAbsent(t *testing.T) {
	r := buildRequestIDRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/echo", nil)
	r.ServeHTTP(w, req)

	headerID := w.Header().Get(RequestIDHeader)
	bodyID := w.Body.String()

	if headerID == "" {
		t.Error("missing X-Request-ID header")
	}
	if len(headerID) != 32 {
		t.Errorf("ID length = %d, want 32", len(headerID))
	}
	if headerID != bodyID {
		t.Errorf("header %q != body %q", headerID, bodyID)
	}
}

func TestRequestID_HonoursInboundHeader(t *testing.T) {
	r := buildRequestIDRouter()

	const inbound = "my-fixed-request-id"
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/echo", nil)
	req.Header.Set(RequestIDHeader, inbound)
	r.ServeHTTP(w, req)

	if got := w.Header().Get(RequestIDHeader); got != inbound {
		t.Errorf("header = %q, want %q", got, inbound)
	}
	if got := w.Body.String(); got != inbound {
		t.Errorf("body = %q, want %q", got, inbound)
	}
}

func TestRequestIDFromContext_EmptyForBareContext(t *testing.T) {
	if id := RequestIDFromContext(context.Background()); id != "" {
		t.Errorf("expected empty, got %q", id)
	}
}

func TestLoggerFromContext_FallsBackToDefault(t *testing.T) {
	if LoggerFromContext(context.Background()) == nil {
		t.Error("fallback logger is nil")
	}
}

func TestRequestID_UniquePerRequest(t *testing.T) {
	r := buildRequestIDRouter()

	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/echo", nil)
		r.ServeHTTP(w, req)
		id := w.Body.String()
		if seen[id] {
			t.Fatalf("duplicate request ID: %q", id)
		}
		seen[id] = true
	}
}
