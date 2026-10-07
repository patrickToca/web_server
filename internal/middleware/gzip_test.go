package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func buildGzipRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Gzip())
	r.GET("/text", func(c *gin.Context) {
		c.Header("Content-Type", "text/plain")
		c.String(http.StatusOK, strings.Repeat("hello world ", 200))
	})
	return r
}

func decodeGzip(t *testing.T, body io.Reader) string {
	t.Helper()
	gr, err := gzip.NewReader(body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gr.Close()
	out, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("read gzip body: %v", err)
	}
	return string(out)
}

func TestGzip_CompressesWhenAccepted(t *testing.T) {
	r := buildGzipRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/text", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Errorf("Content-Encoding = %q, want gzip", got)
	}
	if got := w.Header().Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Errorf("Vary = %q, want Accept-Encoding", got)
	}
	if !strings.Contains(decodeGzip(t, w.Body), "hello world") {
		t.Error("decompressed body missing expected text")
	}
}

func TestGzip_SkipsWhenNotAccepted(t *testing.T) {
	r := buildGzipRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/text", nil)
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q, want empty", got)
	}
}

func TestGzip_SkipsWebSocketUpgrade(t *testing.T) {
	r := buildGzipRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/text", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("WebSocket upgrade must not be compressed; got %q", got)
	}
}

func TestGzip_SkipsSSE(t *testing.T) {
	r := buildGzipRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/text", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Accept", "text/event-stream")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("SSE must not be compressed; got %q", got)
	}
}

func TestGzip_SkipsAlreadyEncoded(t *testing.T) {
	r := buildGzipRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/text", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Content-Encoding", "br")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got == "gzip" {
		t.Error("already-encoded response must not be re-compressed")
	}
}

func TestGzip_NoContentOmitsEncoding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Gzip())
	r.GET("/empty", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/empty", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("204 must not carry Content-Encoding; got %q", got)
	}
}
