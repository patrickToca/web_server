package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestBodyLimit_RejectsOversizedBody(t *testing.T) {
	router := gin.New()
	router.Use(BodyLimitBytes(16))
	router.POST("/echo", func(c *gin.Context) {
		// Reading the body is what triggers the MaxBytesReader error.
		_, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.String(http.StatusRequestEntityTooLarge, "too large")
			return
		}
		c.String(http.StatusOK, "ok")
	})

	body := strings.Repeat("a", 64)
	req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestBodyLimit_AllowsBodyUnderLimit(t *testing.T) {
	router := gin.New()
	router.Use(BodyLimitBytes(1024))
	router.POST("/echo", func(c *gin.Context) {
		b, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.String(http.StatusInternalServerError, "read error")
			return
		}
		c.String(http.StatusOK, string(b))
	})

	body := "hello"
	req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != body {
		t.Fatalf("body = %q, want %q", rec.Body.String(), body)
	}
}

func TestBodyLimit_AllowsExactlyAtLimit(t *testing.T) {
	// http.MaxBytesReader allows exactly N bytes; the error is
	// triggered on the read that would exceed N.
	router := gin.New()
	router.Use(BodyLimitBytes(8))
	router.POST("/echo", func(c *gin.Context) {
		_, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.String(http.StatusRequestEntityTooLarge, "too large")
			return
		}
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewBufferString("12345678"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestLoadMaxBodyBytes_DefaultsAndOverrides(t *testing.T) {
	t.Setenv("MAX_REQUEST_BODY_BYTES", "")
	if got := loadMaxBodyBytes(); got != defaultMaxRequestBodyBytes {
		t.Fatalf("default = %d, want %d", got, defaultMaxRequestBodyBytes)
	}

	t.Setenv("MAX_REQUEST_BODY_BYTES", "4096")
	if got := loadMaxBodyBytes(); got != 4096 {
		t.Fatalf("override = %d, want 4096", got)
	}

	// Non-numeric and non-positive values fall back to the default.
	t.Setenv("MAX_REQUEST_BODY_BYTES", "not-a-number")
	if got := loadMaxBodyBytes(); got != defaultMaxRequestBodyBytes {
		t.Fatalf("invalid value = %d, want default %d", got, defaultMaxRequestBodyBytes)
	}

	t.Setenv("MAX_REQUEST_BODY_BYTES", "-1")
	if got := loadMaxBodyBytes(); got != defaultMaxRequestBodyBytes {
		t.Fatalf("negative value = %d, want default %d", got, defaultMaxRequestBodyBytes)
	}
}
