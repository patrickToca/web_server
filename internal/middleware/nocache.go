// internal/middleware/nocache.go
package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// NoCache sets Cache-Control headers on dynamically-generated responses
// so browsers never serve a stale page from cache.
//
// Why this exists: pages rendered from templates depend on the session
// cookie and on database state that changes between requests. Without
// explicit cache headers, browsers apply heuristic caching (RFC 7234
// §4.2.2) and may serve a stale response to a subsequent request to the
// same URL. In an HTMX app this produces a specific failure: the
// hx-trigger="load" request fires immediately after the page loads, hits
// the browser cache, receives a full page instead of a partial, and
// swaps it into a container that expects a partial — producing nested
// containers and a permanently spinning loader.
//
// Static assets under /static/ are exempt: they are effectively
// immutable for the life of a deployment and benefit from caching.
func NoCache() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/static/") {
			c.Next()
			return
		}

		// no-store: never write to cache, never read from cache.
		// no-cache: write, but revalidate before every use.
		// private: only the browser, not shared proxies, may cache.
		// must-revalidate: once stale, don't serve without revalidation.
		//
		// no-store alone is sufficient and strictest; the others are
		// belt-and-braces for intermediaries that only implement one.
		c.Header("Cache-Control", "no-store, no-cache, must-revalidate, private")
		c.Header("Pragma", "no-cache") // HTTP/1.0
		c.Header("Expires", "0")       // HTTP/1.0

		c.Next()
	}
}
