package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" // RFC 6238 interoperability requires HMAC-SHA-1 by default.
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	totpPeriod = int64(30)
	totpDigits = 6
)

var base32NoPadding = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateTOTPSecret creates a 160-bit base32 secret.
func GenerateTOTPSecret() (string, error) {
	value := make([]byte, 20)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate TOTP secret: %w", err)
	}
	return base32NoPadding.EncodeToString(value), nil
}

// TOTPURI returns a standards-compatible authenticator enrollment URI.
func TOTPURI(secret, account, issuer string) string {
	label := url.PathEscape(issuer + ":" + account)
	query := url.Values{
		"secret":    {secret},
		"issuer":    {issuer},
		"algorithm": {"SHA1"},
		"digits":    {strconv.Itoa(totpDigits)},
		"period":    {strconv.FormatInt(totpPeriod, 10)},
	}
	return "otpauth://totp/" + label + "?" + query.Encode()
}

// VerifyTOTP accepts the current 30-second step and one step of clock skew in
// either direction.
func VerifyTOTP(secret, code string, now time.Time) bool {
	if len(code) != totpDigits || now.Unix() < totpPeriod {
		return false
	}
	for _, character := range code {
		if character < '0' || character > '9' {
			return false
		}
	}
	want, err := strconv.Atoi(code)
	if err != nil {
		return false
	}
	decoded, err := base32NoPadding.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil || len(decoded) < 16 {
		return false
	}
	step := now.Unix() / totpPeriod
	valid := 0
	for offset := int64(-1); offset <= 1; offset++ {
		valid |= subtleIntEqual(totpCode(decoded, uint64(step+offset)), want)
	}
	return valid == 1
}

func totpCode(secret []byte, counter uint64) int {
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], counter)
	digest := hmac.New(sha1.New, secret)
	_, _ = digest.Write(message[:])
	sum := digest.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	return int(value % 1_000_000)
}

func subtleIntEqual(left, right int) int {
	value := uint64(left ^ right)
	value |= 0 - value
	return int((value >> 63) ^ 1)
}
