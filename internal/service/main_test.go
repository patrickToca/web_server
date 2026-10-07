package service

import (
	"os"
	"testing"

	"mywebapp/internal/testutil"
)

// TestMain wires the test database for every test in this package.
//
// testutil.NewPool reads a package-level flag that is only set by
// testutil.Setup. Without this call, every test that needs a pool
// skips with "test database not available", which is misleading: the
// test database is available, it just was never opened. Calling
// Setup here is the contract the testutil package documents.
//
// Setup performs the schema reset, migrations, and seed once per
// binary, then hands out a shared pool for the rest of the run. The
// returned cleanup closes that pool after every test has finished.
func TestMain(m *testing.M) {
	cleanup := testutil.Setup()
	code := m.Run()
	cleanup()
	os.Exit(code)
}
