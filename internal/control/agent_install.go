package control

import (
	_ "embed"
	"errors"
	"net/http"
	"net/url"
	"strings"

	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
	"github.com/senhao-xu/vps-status/internal/versioninfo"
)

const (
	// DefaultAgentReleaseBaseURL is the directory below which versioned GitHub
	// Release assets are published.
	DefaultAgentReleaseBaseURL = "https://github.com/senhao-xu/vps-status/releases/download"
	agentInstallScriptPath     = "/install-agent.sh"
)

func normalizeAgentReleaseConfig(baseURL, version string) (string, string, error) {
	if baseURL == "" {
		baseURL = DefaultAgentReleaseBaseURL
	}
	if version == "" {
		version = versioninfo.Version
	}
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawPath != "" {
		return "", "", errors.New("Agent release base URL must be an HTTPS URL without credentials, query, or fragment")
	}
	if !controlstore.ValidAgentReleaseVersion(version) {
		return "", "", errors.New("Agent release version contains unsupported characters")
	}
	return parsed.String(), version, nil
}

func agentInstallCommand(controlURL, token, baseURL, version string) string {
	scriptURL := strings.TrimRight(baseURL, "/") + "/" + version + "/bootstrap-agent.sh"
	return "curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 " + shellQuote(scriptURL) +
		" | sudo sh -s -- " + shellQuote(controlURL) + " " + shellQuote(token) + " " + shellQuote(version) + " " + shellQuote(baseURL)
}

//go:embed bootstrap-agent.sh
var agentBootstrapScript string

func buildAgentInstallScript(baseURL, version string) string {
	defaults := "default_release_base=" + shellQuote(baseURL) + "\ndefault_release_version=" + shellQuote(version) + "\n"
	return strings.Replace(agentBootstrapScript, "#!/bin/sh\n", "#!/bin/sh\n"+defaults, 1)
}

func (api *adminAPI) installAgentScript(response http.ResponseWriter, request *http.Request) {
	saved, err := api.config.Store.InstallationConfig(request.Context())
	if err != nil {
		writeInternal(response)
		return
	}
	version := api.installationDTO(saved).EffectiveAgentReleaseVersion
	response.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Disposition", `inline; filename="install-agent.sh"`)
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte(buildAgentInstallScript(api.config.AgentReleaseBaseURL, version)))
}
