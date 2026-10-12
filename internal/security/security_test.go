package security

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testPasswordParams = PasswordParams{
	MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
}

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple", testPasswordParams)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if strings.Contains(hash, "correct horse") {
		t.Fatal("password hash contains plaintext")
	}
	if ok, err := VerifyPassword("correct horse battery staple", hash); err != nil || !ok {
		t.Fatalf("VerifyPassword(correct) = %v, %v; want true, nil", ok, err)
	}
	if ok, err := VerifyPassword("wrong", hash); err != nil || ok {
		t.Fatalf("VerifyPassword(wrong) = %v, %v; want false, nil", ok, err)
	}
	if _, err := VerifyPassword("password", "$argon2id$broken"); err == nil {
		t.Fatal("VerifyPassword(malformed) error = nil")
	}
	malformedParams := strings.Replace(hash, "p=1$", "p=1trailing$", 1)
	if _, err := VerifyPassword("correct horse battery staple", malformedParams); err == nil {
		t.Fatal("VerifyPassword() accepted trailing Argon2id parameter data")
	}
}

func TestPasswordParametersAreBounded(t *testing.T) {
	params := testPasswordParams
	params.MemoryKiB = 1024
	if _, err := HashPassword("password", params); err == nil {
		t.Fatal("HashPassword() accepted an unsafe memory cost")
	}
}

func TestTOTPAndRecoveryCodes(t *testing.T) {
	// RFC 6238 SHA-1 vector, truncated to the six digits used by this service.
	const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	if !VerifyTOTP(secret, "287082", time.Unix(59, 0)) {
		t.Fatal("VerifyTOTP() rejected RFC 6238 vector")
	}
	if VerifyTOTP(secret, "287083", time.Unix(59, 0)) || VerifyTOTP(secret, "12ab56", time.Unix(59, 0)) {
		t.Fatal("VerifyTOTP() accepted an invalid code")
	}

	codes, err := GenerateRecoveryCodes(10)
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes() error = %v", err)
	}
	seen := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		normalized := NormalizeRecoveryCode(code)
		if len(normalized) != 26 {
			t.Fatalf("normalized recovery code length = %d; want 26", len(normalized))
		}
		if _, duplicate := seen[normalized]; duplicate {
			t.Fatalf("duplicate recovery code %q", code)
		}
		seen[normalized] = struct{}{}
		lowerSpaced := strings.ToLower(strings.ReplaceAll(code, "-", " "))
		if DigestRecoveryCode(code) != DigestRecoveryCode(lowerSpaced) {
			t.Fatal("recovery-code digest is not normalization-stable")
		}
	}
}

func TestMasterKeyEncryptsAndEnforcesPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "master.key")
	key, err := LoadOrCreateMasterKey(path)
	if err != nil {
		t.Fatalf("LoadOrCreateMasterKey() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(master key) error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("master key mode = %o; want 600", got)
	}

	plaintext := []byte("telegram-token-that-must-not-leak")
	envelope, err := key.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if bytes.Contains(envelope, plaintext) {
		t.Fatal("encrypted envelope contains plaintext")
	}
	decrypted, err := key.Decrypt(envelope)
	if err != nil || !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("Decrypt() = %q, %v; want original plaintext", decrypted, err)
	}
	envelope[len(envelope)-1] ^= 1
	if _, err := key.Decrypt(envelope); err == nil {
		t.Fatal("Decrypt() accepted a modified envelope")
	}

	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("Chmod(master key) error = %v", err)
	}
	if _, err := LoadOrCreateMasterKey(path); err == nil {
		t.Fatal("LoadOrCreateMasterKey() accepted group/world-readable key")
	}
}

func TestLimiterIsBoundedAndResets(t *testing.T) {
	limiter := NewLimiter(2, time.Minute, 1)
	now := time.Unix(1_700_000_000, 0)
	limiter.now = func() time.Time { return now }
	if allowed, _ := limiter.Allow("first"); !allowed {
		t.Fatal("first request was rejected")
	}
	if allowed, _ := limiter.Allow("first"); !allowed {
		t.Fatal("second request was rejected")
	}
	if allowed, retry := limiter.Allow("first"); allowed || retry != time.Minute {
		t.Fatalf("third request = (%v, %v); want (false, 1m)", allowed, retry)
	}
	if allowed, _ := limiter.Allow("overflow"); !allowed {
		t.Fatal("first overflow-bucket request was rejected")
	}
	if len(limiter.entries) != 1 {
		t.Fatalf("limiter entry count = %d; want hard bound 1", len(limiter.entries))
	}
	now = now.Add(time.Minute)
	if allowed, _ := limiter.Allow("first"); !allowed {
		t.Fatal("request was not allowed after window reset")
	}
}

func TestTokensAreRandomAndDomainSeparated(t *testing.T) {
	first, err := RandomToken()
	if err != nil {
		t.Fatalf("RandomToken() error = %v", err)
	}
	second, err := RandomToken()
	if err != nil {
		t.Fatalf("RandomToken() error = %v", err)
	}
	if first == second || len(first) != 43 {
		t.Fatalf("RandomToken() values have unexpected uniqueness/length: %q, %q", first, second)
	}
	if DigestToken(first) == DigestCSRF(first) || DigestToken(first) == DigestRecoveryCode(first) {
		t.Fatal("token digests are not domain separated")
	}
}

func TestMasterKeyRejectsInvalidLength(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := LoadOrCreateMasterKey(path); err == nil {
		t.Fatal("LoadOrCreateMasterKey() accepted an invalid key length")
	}
}
