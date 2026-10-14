package control

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

func installationAuth(t *testing.T, handler http.Handler) ([]*http.Cookie, string) {
	t.Helper()
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"installation-test-password"}`, nil, "")
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup=%d %s", setup.Code, setup.Body.String())
	}
	auth := decodeResponse[struct {
		CSRFToken string `json:"csrf_token"`
	}](t, setup)
	return []*http.Cookie{responseCookie(t, setup, sessionCookieName)}, auth.CSRFToken
}

func TestInstallationAPIAuthenticationCSRFValidationAndDTO(t *testing.T) {
	handler, _ := newTestAdminAPI(t, nil)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		response := performJSON(t, handler, method, "/api/v1/admin/installation", `{"control_url":"","agent_release_version":""}`, nil, "")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s=%d", method, response.Code)
		}
	}
	cookies, csrf := installationAuth(t, handler)
	for _, token := range []string{"", "wrong-csrf"} {
		response := performJSON(t, handler, http.MethodPut, "/api/v1/admin/installation", `{"control_url":"","agent_release_version":""}`, cookies, token)
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"csrf_invalid"`) {
			t.Fatalf("CSRF=%d %s", response.Code, response.Body.String())
		}
	}
	for _, body := range []string{`null`, `{}`, `{"control_url":null,"agent_release_version":""}`, `{"control_url":"","agent_release_version":null}`, `{"control_url":"https://localhost","agent_release_version":""}`, `{"control_url":"https://127.0.0.1","agent_release_version":""}`, `{"control_url":"https://[::1]","agent_release_version":""}`, `{"control_url":"https://host.localhost","agent_release_version":""}`, `{"control_url":"http://control.example","agent_release_version":""}`, `{"control_url":"","agent_release_version":"../x"}`, `{"control_url":"","agent_release_version":"DeV"}`, `{"control_url":"","agent_release_version":"unknown"}`, `{"control_url":"","agent_release_version":"","effective_agent_release_version":"x"}`, `{"control_url":"","agent_release_version":""} {}`} {
		response := performJSON(t, handler, http.MethodPut, "/api/v1/admin/installation", body, cookies, csrf)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid %s=%d %s", body, response.Code, response.Body.String())
		}
	}
	for _, address := range []string{"https://127.1", "https://2130706433", "https://0x7f000001", "https://0177.0.0.1"} {
		body, err := json.Marshal(map[string]string{"control_url": address, "agent_release_version": ""})
		if err != nil {
			t.Fatal(err)
		}
		response := performJSON(t, handler, http.MethodPut, "/api/v1/admin/installation", string(body), cookies, csrf)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_input"`) {
			t.Fatalf("numeric local setting %q=%d %s", address, response.Code, response.Body.String())
		}
	}
	initial := performJSON(t, handler, http.MethodGet, "/api/v1/admin/installation", "", cookies, "")
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(initial.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	if initial.Code != http.StatusOK || len(fields) != 5 || initial.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("DTO=%d %s", initial.Code, initial.Body.String())
	}
	dto := decodeResponse[installationDTO](t, initial)
	if dto.ControlURL != "" || dto.AgentReleaseVersion != "" || dto.EffectiveAgentReleaseVersion != "v1.2.3" || dto.AgentReleaseBaseURL != DefaultAgentReleaseBaseURL || !dto.ReleaseConfigured {
		t.Fatalf("initial=%+v", dto)
	}
	saved := performJSON(t, handler, http.MethodPut, "/api/v1/admin/installation", `{"control_url":" https://control.example/panel/ ","agent_release_version":" v2.0.0 "}`, cookies, csrf)
	dto = decodeResponse[installationDTO](t, saved)
	if saved.Code != http.StatusOK || dto.ControlURL != "https://control.example/panel" || dto.AgentReleaseVersion != "v2.0.0" || dto.EffectiveAgentReleaseVersion != "v2.0.0" {
		t.Fatalf("saved=%d %+v", saved.Code, dto)
	}
	script := performJSON(t, handler, http.MethodGet, agentInstallScriptPath, "", nil, "")
	if script.Code != http.StatusOK || !strings.Contains(script.Body.String(), "default_release_version='v2.0.0'") {
		t.Fatalf("compatibility script=%d %s", script.Code, script.Body.String())
	}
	reset := performJSON(t, handler, http.MethodPut, "/api/v1/admin/installation", `{"control_url":"","agent_release_version":""}`, cookies, csrf)
	dto = decodeResponse[installationDTO](t, reset)
	if reset.Code != http.StatusOK || dto.ControlURL != "" || dto.AgentReleaseVersion != "" || dto.EffectiveAgentReleaseVersion != "v1.2.3" {
		t.Fatalf("reset=%d %+v", reset.Code, dto)
	}
}

func TestEnrollmentInstallationPriorityOriginAndLegacyDevelopment(t *testing.T) {
	handler, _ := newTestAdminAPI(t, func(config *APIConfig) { config.AgentReleaseBaseURL = "https://releases.example/agents" })
	cookies, csrf := installationAuth(t, handler)
	requestEnrollment := func(body, origin string) *httptest.ResponseRecorder {
		requestURL := "https://control.example/api/v1/admin/enrollments"
		if origin != "" {
			requestURL = origin + "/api/v1/admin/enrollments"
		}
		request := httptest.NewRequest(http.MethodPost, requestURL, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(csrfHeaderName, csrf)
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		for _, cookie := range cookies {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	for _, origin := range []string{"", "https://localhost", "http://control.example", "https://127.1", "https://2130706433", "https://0x7f000001", "https://0177.0.0.1"} {
		response := requestEnrollment(`{"node_name":"edge"}`, origin)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"control_url_required"`) {
			t.Fatalf("invalid/missing Origin %q=%d %s", origin, response.Code, response.Body.String())
		}
	}
	origin := requestEnrollment(`{"node_name":"edge"}`, "https://control.example")
	if origin.Code != http.StatusCreated || !strings.Contains(origin.Body.String(), "https://control.example") || !strings.Contains(origin.Body.String(), "https://releases.example/agents/v1.2.3/bootstrap-agent.sh") {
		t.Fatalf("origin=%d %s", origin.Code, origin.Body.String())
	}
	legacy := requestEnrollment(`{"node_name":"local","control_url":"http://localhost:8080"}`, "")
	if legacy.Code != http.StatusCreated || !strings.Contains(legacy.Body.String(), "http://localhost:8080") {
		t.Fatalf("legacy=%d %s", legacy.Code, legacy.Body.String())
	}
	saved := performJSON(t, handler, http.MethodPut, "/api/v1/admin/installation", `{"control_url":"https://remote.example/panel","agent_release_version":"v3.4.5"}`, cookies, csrf)
	if saved.Code != http.StatusOK {
		t.Fatalf("save=%d %s", saved.Code, saved.Body.String())
	}
	priority := requestEnrollment(`{"node_name":"edge","control_url":"http://localhost:8080"}`, "https://control.example")
	if priority.Code != http.StatusCreated || !strings.Contains(priority.Body.String(), "https://remote.example/panel") || strings.Contains(priority.Body.String(), "localhost") || !strings.Contains(priority.Body.String(), "/v3.4.5/bootstrap-agent.sh") {
		t.Fatalf("priority=%d %s", priority.Code, priority.Body.String())
	}
}

func TestEnrollmentDevelopmentReleaseGuardDoesNotCreateToken(t *testing.T) {
	for _, version := range []string{"dev", "unknown", "DEV", "Unknown"} {
		t.Run(version, func(t *testing.T) {
			var store *controlstore.Store
			handler, _ := newTestAdminAPI(t, func(config *APIConfig) { config.AgentReleaseVersion = version; store = config.Store })
			cookies, csrf := installationAuth(t, handler)
			db, err := sql.Open("sqlite", store.Path())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var before, after int
			if err := db.QueryRow(`SELECT COUNT(*) FROM enrollment_tokens`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			response := performJSON(t, handler, http.MethodPost, "/api/v1/admin/enrollments", `{"node_name":"edge","control_url":"https://control.example"}`, cookies, csrf)
			if err := db.QueryRow(`SELECT COUNT(*) FROM enrollment_tokens`).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"agent_release_required"`) || strings.Contains(response.Body.String(), "enrollment_token") || before != after {
				t.Fatalf("guard=%d %s before=%+v after=%+v", response.Code, response.Body.String(), before, after)
			}
			saved := performJSON(t, handler, http.MethodPut, "/api/v1/admin/installation", `{"control_url":"https://control.example","agent_release_version":"v1.2.3"}`, cookies, csrf)
			if saved.Code != http.StatusOK {
				t.Fatalf("save=%d %s", saved.Code, saved.Body.String())
			}
			response = performJSON(t, handler, http.MethodPost, "/api/v1/admin/enrollments", `{"node_name":"edge"}`, cookies, csrf)
			if response.Code != http.StatusCreated {
				t.Fatalf("override=%d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestInstallationStorageFailureIsRedacted(t *testing.T) {
	var store *controlstore.Store
	handler, _ := newTestAdminAPI(t, func(config *APIConfig) { store = config.Store })
	cookies, csrf := installationAuth(t, handler)
	// Session authentication needs the store, so exercise the authenticated handlers
	// directly after closing it to isolate installation failure mapping.
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	api := &adminAPI{config: APIConfig{Store: store, AgentReleaseVersion: "v1.2.3", AgentReleaseBaseURL: DefaultAgentReleaseBaseURL}}
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		request := httptest.NewRequest(method, "/api/v1/admin/installation", strings.NewReader(`{"control_url":"","agent_release_version":""}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(csrfHeaderName, csrf)
		for _, cookie := range cookies {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		if method == http.MethodGet {
			api.installation(response, request, authenticatedRequest{})
		} else {
			api.updateInstallation(response, request, authenticatedRequest{})
		}
		if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), `"code":"internal_error"`) || strings.Contains(response.Body.String(), store.Path()) || strings.Contains(response.Body.String(), "UPDATE installation") {
			t.Fatalf("storage failure=%d %s", response.Code, response.Body.String())
		}
	}
}
