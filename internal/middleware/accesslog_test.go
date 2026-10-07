package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func captureLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(h), &buf
}

func TestAccessLog_EmitsOneJSONLinePerRequest(t *testing.T) {
	logger, buf := captureLogger()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestID(logger))
	r.Use(AccessLog(logger))
	r.GET("/hello", func(c *gin.Context) { c.String(http.StatusOK, "hi") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/hello", nil)
	r.ServeHTTP(w, req)

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d\n%s", len(lines), buf.String())
	}

	var entry map[string]any
	if err := json.Unmarshal(lines[0], &entry); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, lines[0])
	}
	if entry["msg"] != "http_request" {
		t.Errorf("msg = %v", entry["msg"])
	}
	if entry["method"] != "GET" {
		t.Errorf("method = %v", entry["method"])
	}
	if entry["path"] != "/hello" {
		t.Errorf("path = %v", entry["path"])
	}
	if v, _ := entry["status"].(float64); v != 200 {
		t.Errorf("status = %v", entry["status"])
	}
	if _, ok := entry["request_id"]; !ok {
		t.Error("missing request_id")
	}
}

func TestAccessLog_4xxIsWarn(t *testing.T) {
	logger, buf := captureLogger()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestID(logger))
	r.Use(AccessLog(logger))
	r.GET("/missing", func(c *gin.Context) { c.Status(http.StatusNotFound) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	r.ServeHTTP(w, req)

	var entry map[string]any
	_ = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry)
	if entry["level"] != "WARN" {
		t.Errorf("level = %v, want WARN", entry["level"])
	}
}

func TestAccessLog_5xxIsError(t *testing.T) {
	logger, buf := captureLogger()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestID(logger))
	r.Use(AccessLog(logger))
	r.GET("/boom", func(c *gin.Context) { c.Status(http.StatusInternalServerError) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	r.ServeHTTP(w, req)

	var entry map[string]any
	_ = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry)
	if entry["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", entry["level"])
	}
}

func TestAccessLog_IncludesQuery(t *testing.T) {
	logger, buf := captureLogger()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestID(logger))
	r.Use(AccessLog(logger))
	r.GET("/search", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/search?q=hello&page=2", nil)
	r.ServeHTTP(w, req)

	var entry map[string]any
	_ = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry)
	if q, _ := entry["query"].(string); q == "" {
		t.Errorf("query not populated: %v", entry)
	}
}
