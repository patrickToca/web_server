package web

import (
	"os"
	"testing"

	"mywebapp/internal/testutil"
)

// TestMain wires the test database for every test in this package.
//
// See the same file in internal/service for the rationale. The short
// version: testutil.NewPool skips unless testutil.Setup has been
// called, and Setup must be called once per test binary from
// TestMain.
func TestMain(m *testing.M) {
	cleanup := testutil.Setup()
	code := m.Run()
	cleanup()
	os.Exit(code)
}
