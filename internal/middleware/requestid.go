package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"

	"github.com/gin-gonic/gin"
)

// RequestIDHeader is the canonical header used to propagate a request ID
// across services. If an inbound request already carries it, we honour it.
const RequestIDHeader = "X-Request-ID"

// requestIDKey is the unexported context key. Unexported so nobody outside
// this package can accidentally clobber it.
type requestIDKey struct{}

// loggerKey is the key for the request-scoped *slog.Logger.
type loggerKey struct{}

// newRequestID returns a 16-byte hex-encoded random ID (32 chars).
// crypto/rand is cheap enough per request and avoids collisions.
func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Extremely unlikely; fall back to a fixed marker rather than panic.
		return "req-unknown"
	}
	return hex.EncodeToString(b[:])
}

// RequestID is a Gin middleware that:
//
//  1. Reads X-Request-ID from the incoming request, or generates a new one.
//  2. Stores the raw ID in the context (accessible via RequestIDFromContext).
//  3. Sets the ID on the response header so clients can correlate.
//  4. Builds a child *slog.Logger with request_id attached and stores it
//     in the context (accessible via LoggerFromContext).
//
// Downstream middleware/handlers should call LoggerFromContext(c) to get a
// logger whose lines already carry request_id.
func RequestID(base *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(RequestIDHeader)
		if id == "" {
			id = newRequestID()
		}

		// Echo it back so the client can log/trace it too.
		c.Header(RequestIDHeader, id)

		// Request-scoped logger. Every line emitted through this logger
		// carries request_id, no matter where it's called from.
		reqLogger := base.With(slog.String("request_id", id))

		ctx := c.Request.Context()
		ctx = context.WithValue(ctx, requestIDKey{}, id)
		ctx = context.WithValue(ctx, loggerKey{}, reqLogger)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}

// RequestIDFromContext returns the request ID stored in ctx, or "" if none.
// Works for both *gin.Context and a raw context.Context.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

// LoggerFromContext returns the request-scoped logger, falling back to the
// process default slog logger if the middleware wasn't installed.
func LoggerFromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// LoggerFromGin is a convenience for handlers that have a *gin.Context.
func LoggerFromGin(c *gin.Context) *slog.Logger {
	return LoggerFromContext(c.Request.Context())
}
