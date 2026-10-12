package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// RandomToken creates a 256-bit URL-safe secret.
func RandomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

// DigestToken returns the one-way representation stored by Control.
func DigestToken(value string) [32]byte {
	return sha256.Sum256([]byte("vps-status/token/v1\x00" + value))
}

// DigestEnrollmentToken returns the domain-separated representation of an
// Agent enrollment token stored by Control.
func DigestEnrollmentToken(value string) [32]byte {
	return sha256.Sum256([]byte("vps-status/enrollment/v1\x00" + value))
}

// DigestNodeCredential returns the domain-separated representation of a
// node credential stored by Control.
func DigestNodeCredential(value string) [32]byte {
	return sha256.Sum256([]byte("vps-status/node-credential/v1\x00" + value))
}

// DigestCSRF returns the one-way CSRF token representation stored by Control.
func DigestCSRF(value string) [32]byte {
	return sha256.Sum256([]byte("vps-status/csrf/v1\x00" + value))
}

// DigestRecoveryCode returns the one-way representation stored by Control.
func DigestRecoveryCode(value string) [32]byte {
	return sha256.Sum256([]byte("vps-status/recovery/v1\x00" + NormalizeRecoveryCode(value)))
}
