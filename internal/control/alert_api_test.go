package control

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

func TestAlertAndChannelAPIRequiresCSRFAndNeverReadsBackSecrets(t *testing.T) {
	var store *controlstore.Store
	handler, _ := newTestAdminAPI(t, func(config *APIConfig) { store = config.Store })
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"very-long-test-password"}`, nil, "")
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup=%d %s", setup.Code, setup.Body.String())
	}
	var auth struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(setup.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}
	cookie := responseCookie(t, setup, sessionCookieName)
	secretBody := `{"name":"primary","kind":"webhook","url":"https://hooks.example/private/path","secret":"never-read-this-back"}`
	missing := performJSON(t, handler, http.MethodPost, "/api/v1/admin/notification-channels", secretBody, []*http.Cookie{cookie}, "")
	if missing.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF=%d", missing.Code)
	}
	created := performJSON(t, handler, http.MethodPost, "/api/v1/admin/notification-channels", secretBody, []*http.Cookie{cookie}, auth.CSRF)
	if created.Code != http.StatusCreated {
		t.Fatalf("create channel=%d %s", created.Code, created.Body.String())
	}
	if strings.Contains(created.Body.String(), "hooks.example") || strings.Contains(created.Body.String(), "never-read-this-back") {
		t.Fatalf("channel response leaked secret material: %s", created.Body.String())
	}
	var channel struct {
		ID         string `json:"id"`
		Configured bool   `json:"configured"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &channel); err != nil || channel.ID == "" || !channel.Configured {
		t.Fatalf("channel=%#v err=%v", channel, err)
	}
	stored, err := store.NotificationChannelByID(t.Context(), channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored.WebhookURLCipher, []byte("hooks.example")) || bytes.Contains(stored.WebhookSecretCipher, []byte("never-read-this-back")) {
		t.Fatal("notification secret was stored without encryption")
	}
	read := performJSON(t, handler, http.MethodGet, "/api/v1/admin/notification-channels/"+channel.ID, "", []*http.Cookie{cookie}, "")
	if read.Code != http.StatusOK || strings.Contains(read.Body.String(), "hooks.example") || strings.Contains(read.Body.String(), "never-read-this-back") {
		t.Fatalf("channel read leaked secret: %d %s", read.Code, read.Body.String())
	}
	telegramToken := "123456:telegram-secret-token"
	telegramChatID := "-100123456"
	telegramBody := `{"name":"telegram","kind":"telegram","bot_token":"` + telegramToken + `","chat_id":"` + telegramChatID + `"}`
	telegramCreated := performJSON(t, handler, http.MethodPost, "/api/v1/admin/notification-channels", telegramBody, []*http.Cookie{cookie}, auth.CSRF)
	if telegramCreated.Code != http.StatusCreated || strings.Contains(telegramCreated.Body.String(), telegramToken) || strings.Contains(telegramCreated.Body.String(), telegramChatID) {
		t.Fatalf("Telegram channel response leaked secret material: %d %s", telegramCreated.Code, telegramCreated.Body.String())
	}
	var telegramChannel struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(telegramCreated.Body.Bytes(), &telegramChannel); err != nil || telegramChannel.ID == "" {
		t.Fatalf("Telegram channel=%#v err=%v", telegramChannel, err)
	}
	telegramStored, err := store.NotificationChannelByID(t.Context(), telegramChannel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(telegramStored.TelegramTokenCipher, []byte(telegramToken)) || bytes.Contains(telegramStored.TelegramChatIDCipher, []byte(telegramChatID)) {
		t.Fatal("Telegram secret was stored without encryption")
	}
	telegramRead := performJSON(t, handler, http.MethodGet, "/api/v1/admin/notification-channels/"+telegramChannel.ID, "", []*http.Cookie{cookie}, "")
	if telegramRead.Code != http.StatusOK || strings.Contains(telegramRead.Body.String(), telegramToken) || strings.Contains(telegramRead.Body.String(), telegramChatID) {
		t.Fatalf("Telegram channel read leaked secret: %d %s", telegramRead.Code, telegramRead.Body.String())
	}

	enrollment := performJSON(t, handler, http.MethodPost, "/api/v1/admin/enrollments",
		`{"node_name":"edge","control_url":"https://control.example"}`, []*http.Cookie{cookie}, auth.CSRF)
	if enrollment.Code != http.StatusCreated {
		t.Fatalf("enrollment=%d %s", enrollment.Code, enrollment.Body.String())
	}
	var enrollmentBody struct {
		Token string `json:"enrollment_token"`
	}
	if err := json.Unmarshal(enrollment.Body.Bytes(), &enrollmentBody); err != nil {
		t.Fatal(err)
	}
	agentRequest := httptest.NewRequest(http.MethodPost, "/agent/v1/enroll", nil)
	agentRequest.Header.Set("Authorization", "Bearer "+enrollmentBody.Token)
	agentResponse := httptest.NewRecorder()
	handler.ServeHTTP(agentResponse, agentRequest)
	if agentResponse.Code != http.StatusCreated {
		t.Fatalf("agent enrollment=%d %s", agentResponse.Code, agentResponse.Body.String())
	}
	var enrolled struct {
		NodeID string `json:"node_id"`
	}
	if err := json.Unmarshal(agentResponse.Body.Bytes(), &enrolled); err != nil {
		t.Fatal(err)
	}
	ruleBody := `{"name":"CPU hot","scope_kind":"node","scope_value":"` + enrolled.NodeID + `","metric":"cpu","threshold":80,"duration_seconds":30,"recovery_hysteresis":5,"channel_ids":["` + channel.ID + `"]}`
	ruleResponse := performJSON(t, handler, http.MethodPost, "/api/v1/admin/alert-rules", ruleBody, []*http.Cookie{cookie}, auth.CSRF)
	if ruleResponse.Code != http.StatusCreated {
		t.Fatalf("create rule=%d %s", ruleResponse.Code, ruleResponse.Body.String())
	}
}

func TestAlertRuleErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "invalid configuration", err: controlstore.ErrInvalidAlertConfiguration, wantStatus: http.StatusBadRequest, wantCode: `"code":"invalid_input"`},
		{name: "unexpected storage failure", err: errors.New("storage failure"), wantStatus: http.StatusInternalServerError, wantCode: `"code":"internal_error"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			(&adminAPI{}).writeAlertRuleError(response, test.err)
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), test.wantCode) {
				t.Fatalf("response = %d %s, want status %d and code %s", response.Code, response.Body.String(), test.wantStatus, test.wantCode)
			}
		})
	}
}
