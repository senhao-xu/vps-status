package control

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/senhao-xu/vps-status/internal/versioninfo"
)

const (
	// DefaultAgentReleaseBaseURL is the directory below which versioned GitHub
	// Release assets are published.
	DefaultAgentReleaseBaseURL = "https://github.com/senhao-xu/vps-status/releases/download"
	agentInstallScriptPath     = "/install-agent.sh"
)

var agentReleaseVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func normalizeAgentReleaseConfig(baseURL, version string) (string, string, error) {
	if baseURL == "" {
		baseURL = DefaultAgentReleaseBaseURL
	}
	if version == "" {
		version = versioninfo.Version
	}
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" {
		return "", "", errors.New("Agent release base URL must be an HTTPS URL without credentials, query, or fragment")
	}
	if !agentReleaseVersionPattern.MatchString(version) {
		return "", "", errors.New("Agent release version contains unsupported characters")
	}
	return parsed.String(), version, nil
}

func agentInstallCommand(controlURL, token string) string {
	scriptURL := strings.TrimRight(controlURL, "/") + agentInstallScriptPath
	curl := "curl -fsSL"
	if strings.HasPrefix(scriptURL, "https://") {
		curl += " --proto '=https' --tlsv1.2"
	}
	return curl + " " + shellQuote(scriptURL) + " | sudo sh -s -- " + shellQuote(controlURL) + " " + shellQuote(token)
}

func buildAgentInstallScript(baseURL, version string) string {
	script := `#!/bin/sh
set -eu
umask 077

release_base=@@RELEASE_BASE@@
release_version=@@RELEASE_VERSION@@

if [ "$#" -ne 2 ]; then
  echo "usage: install-agent.sh CONTROL_URL ENROLLMENT_TOKEN" >&2
  exit 2
fi
if [ "$(id -u)" -ne 0 ]; then
  echo "installation requires root; the installed Agent service runs non-root" >&2
  exit 1
fi
if [ "$(uname -s)" != "Linux" ]; then
  echo "only Linux is supported" >&2
  exit 1
fi
case "$(uname -m)" in
  x86_64|amd64) agent_arch=amd64 ;;
  aarch64|arm64) agent_arch=arm64 ;;
  *) echo "unsupported architecture; expected amd64 or arm64" >&2; exit 1 ;;
esac
for required_command in curl sha256sum awk systemctl; do
  if ! command -v "$required_command" >/dev/null 2>&1; then
    echo "required command is unavailable: $required_command" >&2
    exit 1
  fi
done

control_url=$1
enrollment_token=$2
agent_file="vps-status-agent-linux-$agent_arch"
release_url="${release_base%/}/$release_version"
temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/vps-status-agent.XXXXXX")
cleanup() {
  rm -rf "$temporary_directory"
}
trap cleanup EXIT HUP INT TERM

download() {
  curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 \
    --output "$temporary_directory/$1" "$release_url/$1"
}
verify_file() {
  file=$1
  if ! checksum=$(awk -v wanted="$file" '$2 == wanted { if (seen) exit 2; value=$1; seen=1 } END { if (!seen) exit 1; print value }' "$temporary_directory/SHA256SUMS"); then
    echo "release checksum is missing or duplicated for $file" >&2
    exit 1
  fi
  if [ "${#checksum}" -ne 64 ]; then
    echo "release checksum is invalid for $file" >&2
    exit 1
  fi
  case "$checksum" in
    *[!0-9a-fA-F]*) echo "release checksum is invalid for $file" >&2; exit 1 ;;
  esac
  printf '%s  %s\n' "$checksum" "$temporary_directory/$file" | sha256sum -c -
}

download SHA256SUMS
download "$agent_file"
download install-agent.sh
download vps-status-agent.service
verify_file "$agent_file"
verify_file install-agent.sh
verify_file vps-status-agent.service
chmod 0700 "$temporary_directory/install-agent.sh"
"$temporary_directory/install-agent.sh" "$temporary_directory/$agent_file" "$control_url" "$enrollment_token"
unset enrollment_token
`
	script = strings.ReplaceAll(script, "@@RELEASE_BASE@@", shellQuote(baseURL))
	return strings.ReplaceAll(script, "@@RELEASE_VERSION@@", shellQuote(version))
}

func (api *adminAPI) installAgentScript(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	response.Header().Set("Cache-Control", "public, max-age=300")
	response.Header().Set("Content-Disposition", `inline; filename="install-agent.sh"`)
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte(buildAgentInstallScript(api.config.AgentReleaseBaseURL, api.config.AgentReleaseVersion)))
}
