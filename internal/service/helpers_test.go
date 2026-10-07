// internal/service/helpers_test.go
package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"mywebapp/internal/testutil"
)

// uniqueJTI returns a JTI that cannot collide with any other test's.
func uniqueJTI() string {
	return fmt.Sprintf("test-jti-%d", time.Now().UnixNano())
}

// seededAliceID returns the ID of the seeded alice user. Every test
// database run reseeds alice with id=1, so the helper is trivial;
// it exists as a single place to change if the seed order changes.
func seededAliceID(t *testing.T) int32 {
	t.Helper()
	pool := testutil.NewPool(t)
	var id int32
	err := pool.QueryRow(context.Background(),
		`SELECT id FROM users WHERE email = 'alice@example.com'`).Scan(&id)
	if err != nil {
		t.Fatalf("could not find seeded alice: %v", err)
	}
	return id
}
