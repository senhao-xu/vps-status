package control

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
)

func TestAgentInstallScriptEndpointIsPublicAndPinned(t *testing.T) {
	handler, _ := newTestAdminAPI(t, func(config *APIConfig) {
		config.AgentReleaseBaseURL = "https://releases.example/vps-status"
		config.AgentReleaseVersion = "v1.2.3"
	})
	request := httptest.NewRequest(http.MethodGet, agentInstallScriptPath, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("installer status = %d; want 200", response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "text/x-shellscript; charset=utf-8" {
		t.Fatalf("installer Content-Type = %q", contentType)
	}
	script := response.Body.String()
	for _, expected := range []string{
		"release_base='https://releases.example/vps-status'",
		"release_version='v1.2.3'",
		"x86_64|amd64) agent_arch=amd64",
		"aarch64|arm64) agent_arch=arm64",
		"download SHA256SUMS",
		"verify_file \"$agent_file\"",
		"verify_file install-agent.sh",
		"verify_file vps-status-agent.service",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("installer script does not contain %q", expected)
		}
	}
	command := exec.Command("sh", "-n")
	command.Stdin = strings.NewReader(script)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("installer shell syntax: %v: %s", err, output)
	}
}

func TestAgentReleaseConfigurationRejectsCommandInjection(t *testing.T) {
	for _, test := range []struct {
		name    string
		baseURL string
		version string
	}{
		{name: "plaintext source", baseURL: "http://releases.example/vps-status", version: "v1.2.3"},
		{name: "source query", baseURL: "https://releases.example/vps-status?file=x", version: "v1.2.3"},
		{name: "version shell syntax", baseURL: "https://releases.example/vps-status", version: "v1.2.3;id"},
		{name: "version path", baseURL: "https://releases.example/vps-status", version: "../v1.2.3"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := normalizeAgentReleaseConfig(test.baseURL, test.version); err == nil {
				t.Fatal("unsafe Agent release configuration was accepted")
			}
		})
	}
}
