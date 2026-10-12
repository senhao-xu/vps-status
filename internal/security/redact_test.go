package security

import (
	"strings"
	"testing"
)

func TestRedactCredentialForms(t *testing.T) {
	secrets := []string{"header-secret", "query-secret", "user-secret", "telegram-secret"}
	input := "Authorization: Bearer header-secret https://host/path?token=query-secret&ok=1 https://user-secret@host api.telegram.org/bottelegram-secret/sendMessage"
	redacted := Redact(input)
	for _, secret := range secrets {
		if strings.Contains(redacted, secret) {
			t.Fatalf("Redact() leaked %q in %q", secret, redacted)
		}
	}
	if !strings.Contains(redacted, "ok=1") {
		t.Fatalf("Redact() removed non-secret context: %q", redacted)
	}
}
