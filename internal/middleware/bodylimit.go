// internal/middleware/bodylimit.go
package middleware

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// defaultMaxRequestBodyBytes caps request bodies at 1 MiB. Every form
// in this application posts a handful of short text fields; the largest
// legitimate body is well under 4 KiB. 1 MiB leaves generous headroom
// for a future file-upload endpoint while still refusing a body that
// could exhaust memory.
const defaultMaxRequestBodyBytes int64 = 1 << 20 // 1 MiB

// BodyLimit returns middleware that wraps the request body in an
// http.MaxBytesReader. A request whose body exceeds the cap fails when
// the handler reads it: the read returns an error, and Gin's form
// parsing propagates it.
//
// The cap is read from MAX_REQUEST_BODY_BYTES at construction time.
// A missing, non-numeric, or non-positive value falls back to the
// default. The value is a byte count, not a human-readable string:
// "1048576", not "1MB".
//
// This middleware must be registered before any route that reads the
// request body. It does not read the body itself, so it is safe to
// place early in the chain.
func BodyLimit() gin.HandlerFunc {
	limit := loadMaxBodyBytes()
	return func(c *gin.Context) {
		// http.MaxBytesReader takes the ResponseWriter so it can
		// request connection close when the limit is hit mid-stream.
		// Passing c.Writer is correct for Gin.
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

// BodyLimitBytes returns middleware with an explicit cap. Useful in
// tests and for routes that need a different limit than the global one.
func BodyLimitBytes(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

// loadMaxBodyBytes reads MAX_REQUEST_BODY_BYTES, falling back to the
// default on absence or parse failure.
func loadMaxBodyBytes() int64 {
	raw := strings.TrimSpace(os.Getenv("MAX_REQUEST_BODY_BYTES"))
	if raw == "" {
		return defaultMaxRequestBodyBytes
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return defaultMaxRequestBodyBytes
	}
	return n
}
