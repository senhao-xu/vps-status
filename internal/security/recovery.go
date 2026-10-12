package security

import (
	"crypto/rand"
	"fmt"
	"strings"
)

// GenerateRecoveryCodes creates one-time, 128-bit recovery codes.
func GenerateRecoveryCodes(count int) ([]string, error) {
	if count < 1 || count > 100 {
		return nil, fmt.Errorf("recovery code count is outside allowed range")
	}
	codes := make([]string, count)
	for index := range codes {
		value := make([]byte, 16)
		if _, err := rand.Read(value); err != nil {
			return nil, fmt.Errorf("generate recovery code: %w", err)
		}
		raw := base32NoPadding.EncodeToString(value)
		groups := make([]string, 0, 7)
		for len(raw) > 4 {
			groups = append(groups, raw[:4])
			raw = raw[4:]
		}
		if raw != "" {
			groups = append(groups, raw)
		}
		codes[index] = strings.Join(groups, "-")
	}
	return codes, nil
}

// NormalizeRecoveryCode makes manual code entry insensitive to case, spaces,
// and display hyphens.
func NormalizeRecoveryCode(value string) string {
	replacer := strings.NewReplacer("-", "", " ", "")
	return strings.ToUpper(replacer.Replace(strings.TrimSpace(value)))
}
