package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// AccessLog returns a Gin middleware that emits one structured log line per
// request via slog. Use it instead of gin.Logger().
//
// It pulls the request-scoped logger from the context (installed by the
// RequestID middleware), so every line automatically carries request_id.
//
// Example output (text):
//
//	time=... level=INFO msg=http_request request_id=... method=GET path=/web/users status=200 latency=3.2ms client_ip=127.0.0.1 size=1234
func AccessLog(fallback *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		// Prefer the request-scoped logger so request_id is attached.
		// Fall back to the caller-provided logger if RequestID middleware
		// wasn't installed (e.g. in tests).
		logger := LoggerFromGin(c)
		if logger == slog.Default() && fallback != nil {
			logger = fallback
		}

		attrs := []slog.Attr{
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.Int("status", status),
			slog.Duration("latency", latency),
			slog.String("client_ip", c.ClientIP()),
			slog.Int("size", c.Writer.Size()),
		}
		if raw != "" {
			attrs = append(attrs, slog.String("query", raw))
		}
		if len(c.Errors) > 0 {
			attrs = append(attrs,
				slog.String("errors", c.Errors.ByType(gin.ErrorTypePrivate).String()))
		}

		level := slog.LevelInfo
		switch {
		case status >= 500:
			level = slog.LevelError
		case status >= 400:
			level = slog.LevelWarn
		}

		logger.LogAttrs(c.Request.Context(), level, "http_request", attrs...)
	}
}
