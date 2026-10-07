package testutil

import (
	"errors"
	"os"
	"path/filepath"
)

// findRepoRoot walks up from the current working directory until it
// finds go.mod. Returns an error if the walk reaches the filesystem
// root without success.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found in any parent directory")
		}
		dir = parent
	}
}
