// Package logging configures the process-wide slog logger.
//
// Controlled by environment variables:
//
//	LOG_FORMAT = "text" (default) | "json"
//	LOG_LEVEL  = "debug" | "info" (default) | "warn" | "error"
//	LOG_FILE   = path to a log file (optional; empty means stderr only)
//
// In a container, leave LOG_FILE unset. The process writes to stderr
// and the platform captures it. Fly shows those lines with `fly logs`;
// Docker shows them with `docker logs`. Writing to a file inside the
// container is the wrong pattern: the file is destroyed on the next
// deploy and nothing outside the container can read it.
package logging

import (
	"context"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// Setup builds the process logger from LOG_FORMAT / LOG_LEVEL /
// LOG_FILE, installs it via slog.SetDefault, and wires Gin's default
// writers to it.
//
// Returns the logger and a cleanup function that closes the log file
// when one was opened. Always call cleanup on shutdown.
//
// When LOG_FILE is empty (the container default), the logger writes to
// os.Stderr and the cleanup is a no-op.
func Setup() (*slog.Logger, func(), error) {
	level := parseLevel(os.Getenv("LOG_LEVEL"))
	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: level == slog.LevelDebug,
	}

	format := strings.ToLower(strings.TrimSpace(os.Getenv("LOG_FORMAT")))

	var dst io.Writer = os.Stderr
	var cleanup = func() {}

	if path := strings.TrimSpace(os.Getenv("LOG_FILE")); path != "" {
		// File logging was explicitly requested. Create the
		// directory and open the file. This path is for development
		// where the operator wants a persistent log; in a container
		// LOG_FILE is unset and this branch is skipped.
		if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
			return nil, nil, err
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, nil, err
		}
		dst = io.MultiWriter(os.Stderr, f)
		cleanup = func() { _ = f.Close() }
	}

	var handler slog.Handler
	switch format {
	case "json":
		handler = slog.NewJSONHandler(dst, opts)
	case "text":
		handler = slog.NewTextHandler(dst, opts)
	default:
		// Auto-detect. In a container, LOG_FORMAT is set explicitly,
		// so this branch is only reached in development.
		if isTerminal(os.Stderr) {
			handler = slog.NewTextHandler(dst, opts)
		} else {
			handler = slog.NewJSONHandler(dst, opts)
		}
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)

	log.SetFlags(0)
	log.SetOutput(slog.NewLogLogger(handler, slog.LevelInfo).Writer())

	gin.DefaultWriter = &slogWriter{logger: logger, level: slog.LevelInfo}
	gin.DefaultErrorWriter = &slogWriter{logger: logger, level: slog.LevelError}

	return logger, cleanup, nil
}

func dirOf(path string) string {
	i := strings.LastIndexByte(path, '/')
	if i < 0 {
		return "."
	}
	return path[:i]
}

func isTerminal(f *os.File) bool {
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
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
