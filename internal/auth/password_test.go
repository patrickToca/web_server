package auth

import (
	"errors"
	"strings"
	"testing"
)

// -----------------------------------------------------------------------------
// HashPassword
// -----------------------------------------------------------------------------

func TestHashPassword_ProducesPHCString(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	// The PHC prefix is the contract every consumer relies on. If it
	// changes, every stored hash becomes unverifiable.
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=") {
		t.Fatalf("hash does not start with the expected prefix: %q", hash)
	}

	// A well-formed PHC string has exactly six $-separated parts (the
	// first empty, from the leading $).
	if got := strings.Count(hash, "$"); got != 5 {
		t.Errorf("expected 5 separators, got %d in %q", got, hash)
	}
}

func TestHashPassword_UniqueSaltPerCall(t *testing.T) {
	// The same password must produce different hashes on each call.
	// If it did not, the salt would be broken or reused.
	const password = "same-password-every-time"

	a, err := HashPassword(password)
	if err != nil {
		t.Fatalf("first hash: %v", err)
	}
	b, err := HashPassword(password)
	if err != nil {
		t.Fatalf("second hash: %v", err)
	}

	if a == b {
		t.Fatal("two hashes of the same password are identical; salt is not random")
	}
}

func TestHashPassword_EmptyPasswordSucceeds(t *testing.T) {
	// An empty password is a valid input to the hashing function. The
	// decision to reject it belongs in the handler, not here. If this
	// ever errors, the contract has changed and handlers must be
	// updated.
	if _, err := HashPassword(""); err != nil {
		t.Fatalf("HashPassword(\"\") returned error: %v", err)
	}
}

// -----------------------------------------------------------------------------
// CheckPasswordHash
// -----------------------------------------------------------------------------

func TestCheckPasswordHash_AcceptsCorrectPassword(t *testing.T) {
	const password = "correct horse battery staple"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if !CheckPasswordHash(password, hash) {
		t.Fatal("correct password was rejected")
	}
}

func TestCheckPasswordHash_RejectsWrongPassword(t *testing.T) {
	hash, err := HashPassword("the-right-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if CheckPasswordHash("the-wrong-password", hash) {
		t.Fatal("wrong password was accepted")
	}
}

func TestCheckPasswordHash_RejectsEmptyAgainstNonEmptyHash(t *testing.T) {
	hash, err := HashPassword("nonempty")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if CheckPasswordHash("", hash) {
		t.Fatal("empty password was accepted against a non-empty hash")
	}
}

func TestCheckPasswordHash_RejectsMalformedHash(t *testing.T) {
	// Every one of these must return false, not panic. The hash comes
	// from the database, which the application does not control in the
	// event of a compromise or a manual edit.
	cases := []struct {
		name string
		hash string
	}{
		{"empty", ""},
		{"not a phc string", "not-a-hash"},
		{"wrong algorithm", "$bcrypt$v=19$m=65536,t=3,p=2$c2FsdA$aGFzaA"},
		{"missing parts", "$argon2id$v=19$m=65536,t=3,p=2$c2FsdA"},
		{"too many parts", "$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$aGFzaA$extra"},
		{"bad version field", "$argon2id$v=abc$m=65536,t=3,p=2$c2FsdA$aGFzaA"},
		{"bad memory field", "$argon2id$v=19$m=xyz,t=3,p=2$c2FsdA$aGFzaA"},
		{"bad salt base64", "$argon2id$v=19$m=65536,t=3,p=2$!!!!$aGFzaA"},
		{"bad hash base64", "$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$!!!!"},
		{"empty salt", "$argon2id$v=19$m=65536,t=3,p=2$$aGFzaA"},
		{"empty hash", "$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if CheckPasswordHash("anything", tc.hash) {
				t.Errorf("malformed hash %q was accepted", tc.hash)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// VerifyPassword (the error-returning form)
// -----------------------------------------------------------------------------

func TestVerifyPassword_DistinguishesParseFailureFromMismatch(t *testing.T) {
	hash, err := HashPassword("the-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	// Wrong password against a valid hash: (false, nil).
	ok, err := VerifyPassword("not-the-password", hash)
	if ok {
		t.Error("wrong password was accepted")
	}
	if err != nil {
		t.Errorf("VerifyPassword returned an error for a valid hash: %v", err)
	}

	// Any password against a malformed hash: (false, ErrInvalidHash).
	ok, err = VerifyPassword("anything", "not-a-hash")
	if ok {
		t.Error("malformed hash was accepted")
	}
	if !errors.Is(err, ErrInvalidHash) {
		t.Errorf("expected ErrInvalidHash, got %v", err)
	}
}

func TestVerifyPassword_RejectsIncompatibleVersion(t *testing.T) {
	// A hash with a version this build does not implement must fail
	// with ErrIncompatibleVersion, not ErrInvalidHash. The distinction
	// matters: a future version-20 hash should be treated as "too new",
	// not "corrupt".
	const v20Hash = "$argon2id$v=20$m=65536,t=3,p=2$c2FsdA$aGFzaA"

	ok, err := VerifyPassword("anything", v20Hash)
	if ok {
		t.Error("version-20 hash was accepted")
	}
	if !errors.Is(err, ErrIncompatibleVersion) {
		t.Errorf("expected ErrIncompatibleVersion, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// NeedsRehash
// -----------------------------------------------------------------------------

func TestNeedsRehash_CurrentParams(t *testing.T) {
	hash, err := HashPassword("password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if NeedsRehash(hash) {
		t.Error("a hash produced by the current parameters should not need rehashing")
	}
}

func TestNeedsRehash_OlderParams(t *testing.T) {
	// A hash with lower memory than the current parameters must be
	// flagged for upgrade. This is the property that makes parameter
	// increases deployable without a schema migration.
	const oldHash = "$argon2id$v=19$m=32768,t=2,p=1$c2FsdA$aGFzaA"

	if !NeedsRehash(oldHash) {
		t.Error("a hash with lower memory should need rehashing")
	}
}

func TestNeedsRehash_Malformed(t *testing.T) {
	if !NeedsRehash("not-a-hash") {
		t.Error("a malformed hash should need rehashing")
	}
}

// -----------------------------------------------------------------------------
// Round-trip with varied inputs
// -----------------------------------------------------------------------------

func TestRoundTrip_VariedPasswords(t *testing.T) {
	// Passwords that have historically caused issues in naive
	// implementations: Unicode, long inputs, control characters.
	passwords := []string{
		"a",
		"short",
		"correct horse battery staple",
		"unicode: ☃ ️ 🔐",
		"with spaces",
		"with\ttabs\nand\nnewlines",
		"with\x00null",
		strings.Repeat("x", 1024),
		strings.Repeat("x", 4096),
	}

	for _, pw := range passwords {
		t.Run("", func(t *testing.T) {
			hash, err := HashPassword(pw)
			if err != nil {
				t.Fatalf("HashPassword(%q): %v", truncate(pw), err)
			}
			if !CheckPasswordHash(pw, hash) {
				t.Errorf("round-trip failed for %q", truncate(pw))
			}
			// A different password must not verify against the same hash.
			if CheckPasswordHash(pw+"x", hash) {
				t.Errorf("a modified password verified for %q", truncate(pw))
			}
		})
	}
}

func truncate(s string) string {
	if len(s) <= 32 {
		return s
	}
	return s[:32] + "..."
}

// -----------------------------------------------------------------------------
// Benchmarks
// -----------------------------------------------------------------------------

func BenchmarkHashPassword(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := HashPassword("benchmark-password"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCheckPasswordHash(b *testing.B) {
	hash, err := HashPassword("benchmark-password")
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CheckPasswordHash("benchmark-password", hash)
	}
}
