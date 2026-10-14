package control

import (
	"errors"
	"net/http"
	"strings"

	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

type installationRequest struct {
	ControlURL          *string `json:"control_url"`
	AgentReleaseVersion *string `json:"agent_release_version"`
}

type installationDTO struct {
	ControlURL                   string `json:"control_url"`
	AgentReleaseVersion          string `json:"agent_release_version"`
	EffectiveAgentReleaseVersion string `json:"effective_agent_release_version"`
	AgentReleaseBaseURL          string `json:"agent_release_base_url"`
	ReleaseConfigured            bool   `json:"release_configured"`
}

func releaseConfigured(version string) bool {
	return controlstore.PublishedAgentReleaseVersion(version)
}

func (api *adminAPI) installationDTO(saved controlstore.Installation) installationDTO {
	version := saved.AgentReleaseVersion
	if version == "" {
		version = api.config.AgentReleaseVersion
	}
	return installationDTO{ControlURL: saved.ControlURL, AgentReleaseVersion: saved.AgentReleaseVersion, EffectiveAgentReleaseVersion: version, AgentReleaseBaseURL: api.config.AgentReleaseBaseURL, ReleaseConfigured: releaseConfigured(version)}
}

func (api *adminAPI) installation(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	saved, err := api.config.Store.InstallationConfig(request.Context())
	if err != nil {
		writeInternal(response)
		return
	}
	writeJSON(response, http.StatusOK, api.installationDTO(saved))
}

func (api *adminAPI) updateInstallation(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	var input installationRequest
	if !decodeJSON(response, request, &input) {
		return
	}
	if input.ControlURL == nil || input.AgentReleaseVersion == nil {
		writeError(response, http.StatusBadRequest, "invalid_input", "Provide the Control address and release version, using empty strings for defaults.")
		return
	}
	address := strings.TrimSpace(*input.ControlURL)
	version := strings.TrimSpace(*input.AgentReleaseVersion)
	if address != "" {
		var err error
		address, err = validateControlURL(address)
		if err != nil {
			writeError(response, http.StatusBadRequest, "invalid_input", "Use a remote HTTPS Control address and a valid release version.")
			return
		}
	}
	saved := controlstore.Installation{ControlURL: address, AgentReleaseVersion: version}
	err := api.config.Store.UpdateInstallation(request.Context(), saved)
	if errors.Is(err, controlstore.ErrInvalidInstallation) {
		writeError(response, http.StatusBadRequest, "invalid_input", "Use a remote HTTPS Control address and a valid release version.")
		return
	}
	if err != nil {
		writeInternal(response)
		return
	}
	writeJSON(response, http.StatusOK, api.installationDTO(saved))
}
