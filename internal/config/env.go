// Package config centralises environment-driven decisions that are
// shared across packages. Keeping the "are we in production?" answer
// in one place avoids the failure mode where one subsystem reads
// APP_ENV and another reads GO_ENV and a third reads ENVIRONMENT, and
// only one of them defaults safely.
package config

import (
	"os"
	"strings"
)

// Environment is the deployment environment the process is running in.
type Environment string

const (
	// EnvDevelopment is the default when APP_ENV is unset. It enables
	// the developer conveniences that must never reach production:
	// fallback secrets, permissive cookie attributes, report-only CSP.
	EnvDevelopment Environment = "development"

	// EnvStaging behaves like production for security purposes but
	// allows a report-only CSP so violations can be observed before
	// they are enforced.
	EnvStaging Environment = "staging"

	// EnvProduction enforces every security control and refuses to
	// start if a required secret is missing.
	EnvProduction Environment = "production"
)

// Current returns the environment named by APP_ENV, defaulting to
// development. Unknown values are treated as production: an operator
// who typos APP_ENV=prod gets the strict behaviour, not the lenient
// one.
func Current() Environment {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV"))) {
	case "", "development", "dev", "local":
		return EnvDevelopment
	case "staging", "stage":
		return EnvStaging
	case "production", "prod":
		return EnvProduction
	default:
		// Fail closed. A mistyped value is not an excuse to disable
		// security controls.
		return EnvProduction
	}
}

// IsProduction reports whether the process is running in production.
// Callers use this to decide whether a missing secret is fatal.
func IsProduction() bool {
	return Current() == EnvProduction
}

// IsStaging reports whether the process is running in staging. Staging
// is treated as production for secret requirements but may relax CSP
// to report-only.
func IsStaging() bool {
	return Current() == EnvStaging
}

// IsDevelopment reports whether the process is running in development.
func IsDevelopment() bool {
	return Current() == EnvDevelopment
}
