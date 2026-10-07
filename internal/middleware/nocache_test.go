package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func buildNoCacheRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(NoCache())
	r.GET("/page", func(c *gin.Context) { c.String(http.StatusOK, "hi") })
	r.GET("/static/css/output.css", func(c *gin.Context) { c.String(http.StatusOK, "css") })
	return r
}

func TestNoCache_SetsHeadersOnDynamic(t *testing.T) {
	r := buildNoCacheRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/page", nil)
	r.ServeHTTP(w, req)

	cc := w.Header().Get("Cache-Control")
	for _, want := range []string{"no-store", "no-cache", "must-revalidate"} {
		if !strings.Contains(cc, want) {
			t.Errorf("Cache-Control missing %q: %s", want, cc)
		}
	}
	if got := w.Header().Get("Pragma"); got != "no-cache" {
		t.Errorf("Pragma = %q", got)
	}
	if got := w.Header().Get("Expires"); got != "0" {
		t.Errorf("Expires = %q", got)
	}
}

func TestNoCache_ExemptsStatic(t *testing.T) {
	r := buildNoCacheRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/css/output.css", nil)
	r.ServeHTTP(w, req)

	if cc := w.Header().Get("Cache-Control"); strings.Contains(cc, "no-store") {
		t.Errorf("static asset got no-store: %s", cc)
	}
}
