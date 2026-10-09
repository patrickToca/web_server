// Package auth provides password hashing and JWT issuance for the
// application.
//
// Passwords are hashed with Argon2id, the winning algorithm of the
// 2015 Password Hashing Competition and the current OWASP recommendation
// for new applications. The parameters are chosen to target roughly
// 100-200ms per hash on commodity hardware, which is the OWASP
// performance envelope for interactive login.
//
// The hash format is the standard PHC string:
//
//	$argon2id$v=19$m=65536,t=3,p=2$<base64-salt>$<base64-hash>
//
// Every component needed to verify a password is in the hash string
// itself, so a parameter change does not require a schema migration:
// old hashes continue to verify with their original parameters, and new
// hashes use the current parameters. See NeedsRehash.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters. The names follow the PHC string format: m is
// memory in KiB, t is iterations, p is parallelism (threads).
//
// These values are the "second recommended" profile from OWASP's
// Password Storage Cheat Sheet as of 2024: 64 MiB of memory, 3
// iterations, 2 threads. They target ~100-200ms per hash on a modern
// laptop, which is the envelope OWASP recommends for interactive
// authentication.
//
// Do not reduce these without measuring. Argon2id's security comes
// from the memory cost; a low-memory configuration is no better than
// PBKDF2 with the same iteration count. If your hardware cannot
// afford 64 MiB per hash under load, lower the memory and raise the
// time parameter, and document the change here.
const (
	argon2Memory      uint32 = 64 * 1024 // 64 MiB, in KiB
	argon2Iterations  uint32 = 3
	argon2Parallelism uint8  = 2
	argon2SaltLength  uint32 = 16
	argon2KeyLength   uint32 = 32
)

// Argon2id version. Version 19 (0x13) is the current specification.
// Older hashes would carry a different version; none exist in this
// codebase, but the parse function preserves the field so a future
// downgrade is detectable.
const argon2Version = argon2.Version

// ErrInvalidHash is returned by CompareHashAndPassword when the stored
// hash is not a well-formed PHC string. It is exported so callers can
// distinguish "wrong password" from "corrupt hash".
var ErrInvalidHash = errors.New("argon2id: hash is not in the expected format")

// ErrIncompatibleVersion is returned when a stored hash uses a version
// of Argon2id this build does not implement.
var ErrIncompatibleVersion = errors.New("argon2id: incompatible version")

// HashPassword hashes a plaintext password with Argon2id using the
// package-level parameters, and returns the PHC-encoded result.
//
// The salt is generated with crypto/rand. It is 16 bytes, the size
// recommended by RFC 9106.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argon2SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("argon2id: generate salt: %w", err)
	}

	hash := argon2.IDKey(
		[]byte(password),
		salt,
		argon2Iterations,
		argon2Memory,
		argon2Parallelism,
		argon2KeyLength,
	)

	return encodeHash(salt, hash), nil
}

// CheckPasswordHash reports whether the plaintext password matches the
// stored PHC-encoded hash.
//
// It parses the parameters from the hash rather than using the
// package-level ones, so a password hashed under older parameters
// still verifies. The comparison is constant-time.
//
// Returns false for any parse error, an invalid version, or a
// mismatched hash. The distinction between "wrong password" and
// "corrupt hash" is available to callers that want it via
// VerifyPassword.
func CheckPasswordHash(password, encodedHash string) bool {
	ok, _ := VerifyPassword(password, encodedHash)
	return ok
}

// VerifyPassword is the error-returning form of CheckPasswordHash.
//
// Returns (true, nil) on a match, (false, nil) on a well-formed hash
// that does not match, and (false, err) when the hash is malformed or
// uses an unsupported version.
//
// Callers that only care whether the password is correct should use
// CheckPasswordHash. Callers that want to log corrupt hashes, or to
// distinguish a parse failure from an authentication failure, should
// use this function.
func VerifyPassword(password, encodedHash string) (bool, error) {
	params, salt, expected, err := decodeHash(encodedHash)
	if err != nil {
		return false, err
	}

	actual := argon2.IDKey(
		[]byte(password),
		salt,
		params.iterations,
		params.memory,
		params.parallelism,
		uint32(len(expected)),
	)

	if subtle.ConstantTimeCompare(expected, actual) == 1 {
		return true, nil
	}
	return false, nil
}

// NeedsRehash reports whether the given PHC-encoded hash was produced
// with parameters different from the current package-level ones.
//
// Callers may use this after a successful login to upgrade an old hash
// in place:
//
//	if auth.CheckPasswordHash(plaintext, user.PasswordHash) {
//	    if auth.NeedsRehash(user.PasswordHash) {
//	        newHash, _ := auth.HashPassword(plaintext)
//	        // persist newHash for user
//	    }
//	    // issue tokens
//	}
//
// In this codebase there is only one set of parameters, so the
// function always returns false for hashes produced by HashPassword.
// It exists so the migration path is available when the parameters
// are eventually raised.
func NeedsRehash(encodedHash string) bool {
	params, _, _, err := decodeHash(encodedHash)
	if err != nil {
		return true
	}
	return params.memory != argon2Memory ||
		params.iterations != argon2Iterations ||
		params.parallelism != argon2Parallelism ||
		params.version != argon2Version
}

// -----------------------------------------------------------------------------
// PHC encoding and decoding
// -----------------------------------------------------------------------------
//
// The format is defined by the Password Hashing Competition and used by
// every modern password hashing library:
//
//	$argon2id$v=<version>$m=<memory>,t=<iterations>,p=<parallelism>$<salt>$<hash>
//
// All numeric fields are decimal. The salt and hash are base64 with
// the standard alphabet and no padding, which is the PHC convention
// (RawStdEncoding, not StdEncoding).
//
// Example:
//
//	$argon2id$v=19$m=65536,t=3,p=2$c29tZXNhbHQ$RdescudvJCsgt3ub+b+dWRWJTmaaJObG
//
// encodeHash builds the string from a raw salt and raw hash.
func encodeHash(salt, hash []byte) string {
	b64 := base64.RawStdEncoding
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2Version,
		argon2Memory, argon2Iterations, argon2Parallelism,
		b64.EncodeToString(salt),
		b64.EncodeToString(hash),
	)
}

// hashParams holds the parameters extracted from a PHC string. They
// are the values needed to recompute the hash and compare.
type hashParams struct {
	version     int
	memory      uint32
	iterations  uint32
	parallelism uint8
}

// decodeHash parses a PHC string into its parameters, salt, and hash.
//
// Returns ErrInvalidHash for any structural problem and
// ErrIncompatibleVersion for an Argon2id version this build does not
// implement.
func decodeHash(encodedHash string) (params hashParams, salt, hash []byte, err error) {
	parts := strings.Split(encodedHash, "$")
	// The leading "$" produces an empty first element, so a
	// well-formed string has exactly six parts:
	//   ["", "argon2id", "v=19", "m=...,t=...,p=...", salt, hash]
	if len(parts) != 6 {
		return params, nil, nil, ErrInvalidHash
	}
	if parts[1] != "argon2id" {
		return params, nil, nil, ErrInvalidHash
	}

	// Version.
	if _, err := fmt.Sscanf(parts[2], "v=%d", &params.version); err != nil {
		return params, nil, nil, ErrInvalidHash
	}
	if params.version != argon2Version {
		return params, nil, nil, ErrIncompatibleVersion
	}

	// Parameters.
	if _, err := fmt.Sscanf(
		parts[3],
		"m=%d,t=%d,p=%d",
		&params.memory, &params.iterations, &params.parallelism,
	); err != nil {
		return params, nil, nil, ErrInvalidHash
	}

	// Salt.
	b64 := base64.RawStdEncoding
	salt, err = b64.DecodeString(parts[4])
	if err != nil {
		return params, nil, nil, ErrInvalidHash
	}
	if len(salt) == 0 {
		return params, nil, nil, ErrInvalidHash
	}

	// Hash.
	hash, err = b64.DecodeString(parts[5])
	if err != nil {
		return params, nil, nil, ErrInvalidHash
	}
	if len(hash) == 0 {
		return params, nil, nil, ErrInvalidHash
	}

	return params, salt, hash, nil
}
