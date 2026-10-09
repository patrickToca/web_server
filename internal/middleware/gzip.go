// internal/middleware/gzip.go
package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// gzipWriter wraps gin.ResponseWriter so every Write is compressed.
// It sets the Content-Encoding header lazily, on the first Write,
// so responses with no body (204, 304) never carry the header.
type gzipWriter struct {
	gin.ResponseWriter
	writer *gzip.Writer
	wrote  bool
}

func (g *gzipWriter) WriteHeader(code int) {
	// 204 and 304 never carry a body. Do not advertise compression
	// for them. Write through to the underlying writer so the
	// status is recorded correctly.
	if code == http.StatusNoContent || code == http.StatusNotModified {
		g.ResponseWriter.WriteHeader(code)
		return
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipWriter) Write(data []byte) (int, error) {
	if !g.wrote {
		g.wrote = true
		h := g.Header()
		h.Set("Content-Encoding", "gzip")
		h.Del("Content-Length")
	}
	return g.writer.Write(data)
}

func (g *gzipWriter) WriteString(s string) (int, error) {
	if !g.wrote {
		g.wrote = true
		h := g.Header()
		h.Set("Content-Encoding", "gzip")
		h.Del("Content-Length")
	}
	return g.writer.Write([]byte(s))
}

var gzipPool = sync.Pool{
	New: func() any {
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
		return w
	},
}

// Gzip compresses responses for clients that advertise gzip support.
//
// Behaviour:
//   - Skips when the client does not send Accept-Encoding: gzip.
//   - Skips WebSocket / SSE upgrade requests (they must not be buffered).
//   - Skips already-compressed responses (Content-Encoding already set).
//   - Skips responses with no body (204, 304) because the header is
//     only emitted on the first Write.
//   - Sets Content-Encoding and Vary headers correctly.
func Gzip() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. Client must accept gzip.
		if !strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") {
			c.Next()
			return
		}

		// 2. Never compress protocol upgrades (WS, SSE, HTTP/2 push).
		if strings.EqualFold(c.GetHeader("Connection"), "Upgrade") ||
			strings.EqualFold(c.GetHeader("Upgrade"), "websocket") ||
			strings.Contains(c.GetHeader("Accept"), "text/event-stream") {
			c.Next()
			return
		}

		// 3. Don't double-compress if an upstream already did.
		if c.GetHeader("Content-Encoding") != "" {
			c.Next()
			return
		}

		gz := gzipPool.Get().(*gzip.Writer)
		gz.Reset(c.Writer)
		defer func() {
			_ = gz.Close()
			gzipPool.Put(gz)
		}()

		// Advertise that the response varies by Accept-Encoding so
		// caches don't serve a gzipped body to a client that can't
		// decode it. Vary is safe to set upfront: it does not promise
		// a compressed body, it just tells caches the response
		// depends on the header.
		c.Header("Vary", "Accept-Encoding")

		origWriter := c.Writer
		gzw := &gzipWriter{ResponseWriter: origWriter, writer: gz}
		c.Writer = gzw

		c.Next()

		// Restore the original writer so subsequent middleware (none
		// in this app) see the normal interface.
		c.Writer = origWriter
	}
}
