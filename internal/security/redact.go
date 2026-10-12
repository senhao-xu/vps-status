package security

import "regexp"

var secretPatterns = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`(?i)(authorization\s*[:=]\s*bearer\s+)[^\s,;]+`), `${1}[REDACTED]`},
	{regexp.MustCompile(`(?i)([?&](?:token|secret|key|credential|password)=)[^&#\s]+`), `${1}[REDACTED]`},
	{regexp.MustCompile(`(?i)(https?://)[^/@\s]+@`), `${1}[REDACTED]@`},
	{regexp.MustCompile(`(?i)(api\.telegram\.org/bot)[^/\s]+`), `${1}[REDACTED]`},
}

// Redact removes common credential-bearing URL/header forms before an error
// reaches a terminal or structured logger. Callers must still avoid logging
// full protocol frames, request bodies, and configuration records.
func Redact(value string) string {
	for _, item := range secretPatterns {
		value = item.pattern.ReplaceAllString(value, item.replacement)
	}
	return value
}
