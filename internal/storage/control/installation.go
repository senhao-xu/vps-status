package control

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// ErrInvalidInstallation indicates an invalid remote address or release version.
var ErrInvalidInstallation = errors.New("invalid installation configuration")

var releaseVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidAgentReleaseVersion reports whether a version is a bounded, safe path segment.
func ValidAgentReleaseVersion(version string) bool { return releaseVersionPattern.MatchString(version) }

// PublishedAgentReleaseVersion excludes placeholder versions used by source builds.
// It describes configuration eligibility, not whether a remote Release exists.
func PublishedAgentReleaseVersion(version string) bool {
	return ValidAgentReleaseVersion(version) && !strings.EqualFold(version, "dev") && !strings.EqualFold(version, "unknown")
}

// Installation contains the singleton administrator-saved installation defaults.
// Empty values use the current remote page address and the startup release version.
type Installation struct {
	ControlURL          string
	AgentReleaseVersion string
}

// ValidateRemoteControlURL normalizes an HTTPS address suitable for remote nodes.
// It does not resolve DNS; private-network HTTPS deployments remain supported.
func ValidateRemoteControlURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil || len(value) > 2048 || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", ErrInvalidInstallation
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	// IPv6 zone identifiers do not change whether the underlying IP is loopback.
	ip := net.ParseIP(strings.SplitN(host, "%", 2)[0])
	if ip == nil {
		ip = numericIPv4Alias(host)
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || ip != nil && (ip.IsLoopback() || ip.IsUnspecified()) {
		return "", ErrInvalidInstallation
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

// numericIPv4Alias recognizes the shortened, integer, octal and hexadecimal
// IPv4 forms normalized by browsers. Never resolve DNS while validating settings.
func numericIPv4Alias(host string) net.IP {
	parts := strings.Split(host, ".")
	if len(parts) > 4 {
		return nil
	}
	var address uint64
	for index, part := range parts {
		base := 10
		if strings.HasPrefix(part, "0x") {
			base, part = 16, part[2:]
		} else if len(part) > 1 && part[0] == '0' {
			base, part = 8, part[1:]
		}
		// Browser IPv4 parsing treats an empty digit sequence after a prefix as 0.
		if part == "" && base != 10 {
			part = "0"
		}
		number, err := strconv.ParseUint(part, base, 32)
		if err != nil {
			return nil
		}
		if index == len(parts)-1 {
			if number >= uint64(1)<<(8*(5-len(parts))) {
				return nil
			}
			address += number
		} else {
			if number > 255 {
				return nil
			}
			address += number << (8 * (3 - index))
		}
	}
	return net.IPv4(byte(address>>24), byte(address>>16), byte(address>>8), byte(address))
}

// InstallationConfig returns saved defaults without runtime-derived values.
func (s *Store) InstallationConfig(ctx context.Context) (Installation, error) {
	var value Installation
	if err := s.db.QueryRowContext(ctx, `SELECT control_url,agent_release_version FROM installation WHERE singleton_id=1`).Scan(&value.ControlURL, &value.AgentReleaseVersion); err != nil {
		return Installation{}, fmt.Errorf("read installation: %w", err)
	}
	return value, nil
}

// UpdateInstallation validates and atomically replaces the installation defaults.
func (s *Store) UpdateInstallation(ctx context.Context, value Installation) error {
	if value.ControlURL != "" {
		normalized, err := ValidateRemoteControlURL(value.ControlURL)
		if err != nil || normalized != value.ControlURL {
			return ErrInvalidInstallation
		}
	}
	if value.AgentReleaseVersion != "" && !PublishedAgentReleaseVersion(value.AgentReleaseVersion) {
		return ErrInvalidInstallation
	}
	result, err := s.db.ExecContext(ctx, `UPDATE installation SET control_url=?,agent_release_version=? WHERE singleton_id=1`, value.ControlURL, value.AgentReleaseVersion)
	if err != nil {
		return fmt.Errorf("update installation: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read installation update result: %w", err)
	}
	if rows != 1 {
		return ErrNotFound
	}
	return nil
}
