// Package logging configures the process-wide slog logger.
//
// Controlled by two environment variables:
//
//	LOG_FORMAT = "text" (default) | "json"
//	LOG_LEVEL  = "debug" | "info" (default) | "warn" | "error"
//
// It also routes Gin's default writers through slog so any third-party
// Gin middleware that writes to gin.DefaultWriter lands in the same stream.
//
// Request-scoped logging (with request_id) is provided by the RequestID
// middleware in internal/middleware; use middleware.LoggerFromGin(c) inside
// handlers to get a logger that automatically includes request_id.
package logging

import (
	"context"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// Setup builds the process logger from LOG_FORMAT / LOG_LEVEL, installs it
// via slog.SetDefault, and wires Gin's default writers to it.
//
// Returns the logger and a cleanup function that closes the log file
// (when one is opened). Always call cleanup on shutdown.
func Setup() (*slog.Logger, func(), error) {
	if err := os.MkdirAll("logs", 0o755); err != nil {
		return nil, nil, err
	}

	f, err := os.OpenFile(filepath.Join("logs", "app.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}

	// Terminal on stderr → mirror to both; otherwise → file only.
	var dst io.Writer = f
	isTTY := false
	if stat, err := os.Stderr.Stat(); err == nil && (stat.Mode()&os.ModeCharDevice) != 0 {
		isTTY = true
		dst = io.MultiWriter(os.Stderr, f)
	}

	level := parseLevel(os.Getenv("LOG_LEVEL"))
	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: level == slog.LevelDebug,
	}

	format := strings.ToLower(strings.TrimSpace(os.Getenv("LOG_FORMAT")))

	var handler slog.Handler
	switch format {
	case "json":
		handler = slog.NewJSONHandler(dst, opts)
	case "text":
		handler = slog.NewTextHandler(dst, opts)
	default:
		// No explicit format: text on a terminal, JSON when redirected.
		if isTTY {
			handler = slog.NewTextHandler(dst, opts)
		} else {
			handler = slog.NewJSONHandler(dst, opts)
		}
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)

	// Route the stdlib `log` package through slog too.
	log.SetFlags(0)
	log.SetOutput(slog.NewLogLogger(handler, slog.LevelInfo).Writer())

	// Route Gin's default writers through slog as well. Note: these lines
	// cannot carry request_id because Gin's default writer interface has no
	// access to the request context. Use middleware.LoggerFromGin(c) inside
	// handlers/middleware for request-correlated logging.
	gin.DefaultWriter = &slogWriter{logger: logger, level: slog.LevelInfo}
	gin.DefaultErrorWriter = &slogWriter{logger: logger, level: slog.LevelError}

	cleanup := func() {
		_ = f.Close()
	}
	return logger, cleanup, nil
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// slogWriter adapts an *slog.Logger to io.Writer for Gin.
type slogWriter struct {
	logger *slog.Logger
	level  slog.Level
}

func (w *slogWriter) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\n")
	if msg == "" {
		return len(p), nil
	}
	w.logger.Log(context.Background(), w.level, msg, slog.String("component", "gin"))
	return len(p), nil
}
