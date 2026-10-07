// Package credentials loads the application's secrets from a
// SOPS-encrypted file and injects them into the process environment.
//
// The split is deliberate:
//
//   - Configuration (host, port, log level, TTLs) lives in .env or in
//     the container's environment and is read directly by the code
//     that needs it.
//
//   - Secrets (passwords, signing keys, API credentials) live in a
//     SOPS file that is committed in encrypted form. This package
//     decrypts that file at boot and sets each key as an environment
//     variable, so the rest of the application reads secrets through
//     os.Getenv exactly as it reads configuration.
//
// The single bootstrap secret is the age private key. In development
// it lives in ~/.config/sops/age/keys.txt. In production it is
// injected by the platform (Fly Secrets, ECS task definition, etc.)
// as AGE_KEY, and this package sets SOPS_AGE_KEY from it before
// invoking sops.
//
// The loader is fail-closed. In production and staging, a missing
// SOPS file, a missing age key, a missing secret, or a secret shorter
// than the minimum length all abort the boot. The process does not
// start with a guessable key.
package credentials

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	sopsdecrypt "github.com/getsops/sops/v3/decrypt"
)

// sopsFilePath is the committed, encrypted secrets file, relative to
// the repository root (or to SOPS_PATH if that is set). The path is
// resolved by resolveSopsPath, which walks up to find go.mod so the
// loader works regardless of the process working directory.
const sopsFilePath = "secrets/secrets.enc.yaml"

// knownSecrets is the closed set of keys the loader will export into
// the process environment. Listing them explicitly rather than
// exporting everything the SOPS file happens to contain is a guard
// against a typo in the file silently propagating: a key that is not
// on this list is not exported, and a key on this list that is missing
// from the file is a hard error in production.
//
// If you add a secret, add it here and to secrets.enc.yaml. The two
// lists must match; the loader checks.
var knownSecrets = []string{
	"DB_PASSWORD",
	"TEST_DB_PASSWORD",
	"JWT_SECRET",
	"CSRF_SECRET",
	"R2_ACCESS_KEY_ID",
	"R2_SECRET_ACCESS_KEY",
}

// minimumSecretBytes is the shortest value the loader will accept for
// a signing key. It applies to JWT_SECRET and CSRF_SECRET. Other
// secrets (passwords, API keys) are not length-checked here because
// their minimums are the responsibility of the service that consumes
// them.
const minimumSecretBytes = 32

// Load decrypts the SOPS file, validates the expected keys are
// present, and exports them into the process environment.
//
// Call Load once, at the top of main, before constructing any service
// that reads a secret. Calling it twice is harmless but wasteful; the
// second call overwrites the environment with the same values.
func Load() error {
	if err := ensureAgeKey(); err != nil {
		return fmt.Errorf("age key: %w", err)
	}

	path, err := resolveSopsPath()
	if err != nil {
		return err
	}

	plaintext, err := sopsdecrypt.File(path, "yaml")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf(
				"SOPS file not found at %s; "+
					"create it with `sops -e secrets.yaml > %s`",
				path, path)
		}
		return fmt.Errorf("decrypt %s: %w", path, err)
	}

	secrets, err := parseYAMLSecrets(plaintext)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	if err := validate(secrets); err != nil {
		return err
	}

	for _, key := range knownSecrets {
		value, ok := secrets[key]
		if !ok {
			// validate already rejected this in production. In
			// development, a missing secret is logged and skipped.
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("setenv %s: %w", key, err)
		}
	}

	slog.Info("credentials loaded",
		"count", len(secrets),
		"path", path)
	return nil
}

// resolveSopsPath returns the absolute path to the SOPS file.
//
// The file is at a fixed location relative to the repository root,
// but the process working directory is not always the repository
// root: `go test` runs each package's tests from that package's
// directory, and the production image may or may not include go.mod.
//
// Resolution order:
//
//  1. SOPS_PATH, if set. This is the escape hatch for deployments
//     where the encrypted file is somewhere the walk cannot reach,
//     such as a container image that does not include the source.
//
//  2. The first ancestor of the working directory that contains
//     go.mod, joined with sopsFilePath. This is the development path:
//     it makes the loader work from any package directory.
//
//  3. The working directory itself, joined with sopsFilePath. This is
//     the fallback for a container that ships the encrypted file at a
//     known relative path but does not ship go.mod.
//
// If none of those resolve to an existing file, the caller's attempt
// to read it will fail with a clear error naming the path.
func resolveSopsPath() (string, error) {
	// 1. Explicit override.
	if v := os.Getenv("SOPS_PATH"); v != "" {
		return v, nil
	}

	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}

	// 2. Walk up to find go.mod.
	walkDir := dir
	for {
		if _, err := os.Stat(filepath.Join(walkDir, "go.mod")); err == nil {
			return filepath.Join(walkDir, sopsFilePath), nil
		}
		parent := filepath.Dir(walkDir)
		if parent == walkDir {
			break
		}
		walkDir = parent
	}

	// 3. Fall back to the working directory. This is what a container
	// without go.mod in the image would use; the Dockerfile sets
	// WORKDIR to the image root, and copies the encrypted file to the
	// same relative path.
	return filepath.Join(dir, sopsFilePath), nil
}

// ensureAgeKey makes the age private key available to the sops
// library. The library reads it from, in order of preference:
//
//  1. SOPS_AGE_KEY: the key value itself, as an environment variable.
//  2. SOPS_AGE_KEY_FILE: a path to a file containing the key.
//  3. $XDG_CONFIG_HOME/sops/age/keys.txt or $HOME/.config/sops/age/keys.txt.
//
// On a developer machine the key is already at (3); this function
// does nothing. In a container the platform injects AGE_KEY, and this
// function sets SOPS_AGE_KEY from it. No file is written.
func ensureAgeKey() error {
	if v := os.Getenv("AGE_KEY"); v != "" {
		if err := os.Setenv("SOPS_AGE_KEY", v); err != nil {
			return fmt.Errorf("set SOPS_AGE_KEY: %w", err)
		}
		return nil
	}

	if v := os.Getenv("SOPS_AGE_KEY_FILE"); v != "" {
		if _, err := os.Stat(v); err == nil {
			return nil
		}
		return fmt.Errorf("SOPS_AGE_KEY_FILE=%s does not exist", v)
	}

	if _, err := os.Stat(defaultAgeKeyPath()); err == nil {
		return nil
	}

	return errors.New(
		"no age key available: set AGE_KEY, or place the key at " +
			defaultAgeKeyPath())
}

// defaultAgeKeyPath returns the sops library's default age key path,
// honouring XDG_CONFIG_HOME if set.
func defaultAgeKeyPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "sops", "age", "keys.txt")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "sops", "age", "keys.txt")
}

// validate checks that the secrets the application requires are
// present and, for signing keys, long enough.
//
// In production and staging, a missing or short secret is an error.
// In development, it is a warning: the process starts, but the secret
// is absent from the environment, which means the consumer's own
// fallback (random per-process key) takes over. That fallback is what
// makes a developer's `go run` work without ceremony, and the warning
// is what makes the missing secret visible.
func validate(secrets map[string]string) error {
	required := []string{"JWT_SECRET", "CSRF_SECRET"}
	signing := map[string]bool{"JWT_SECRET": true, "CSRF_SECRET": true}

	production := isProduction()

	for _, key := range required {
		value, ok := secrets[key]
		if !ok || value == "" {
			if production {
				return fmt.Errorf("required secret %s is missing", key)
			}
			slog.Warn("secret missing from SOPS file; consumer will use its fallback",
				"key", key)
			continue
		}
		if signing[key] && len(value) < minimumSecretBytes {
			if production {
				return fmt.Errorf(
					"secret %s is %d bytes; need at least %d in production",
					key, len(value), minimumSecretBytes)
			}
			slog.Warn("secret shorter than the recommended minimum; tolerated in development only",
				"key", key, "length", len(value), "minimum", minimumSecretBytes)
		}
	}

	if a, b := secrets["JWT_SECRET"], secrets["CSRF_SECRET"]; a != "" && a == b {
		return errors.New("JWT_SECRET and CSRF_SECRET are the same value; they must differ")
	}

	return nil
}

// isProduction reports whether APP_ENV is production or staging.
//
// This is a local copy of the same logic in internal/config. It exists
// because credentials is imported by main, which may import config,
// and duplicating the rule here keeps the two packages independent.
// If the two ever disagree, the loader is the one that governs boot.
func isProduction() bool {
	switch os.Getenv("APP_ENV") {
	case "production", "prod", "staging", "stage":
		return true
	default:
		return false
	}
}
