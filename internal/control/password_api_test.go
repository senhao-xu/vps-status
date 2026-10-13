package control

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/security"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

func passwordSetup(t *testing.T, handler http.Handler, password string) (*http.Cookie, string) {
	t.Helper()
	response := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", passwordJSON(t, map[string]string{"username": "owner", "password": password}), nil, "")
	if response.Code != http.StatusCreated {
		t.Fatalf("setup = %d %s", response.Code, response.Body.String())
	}
	return responseCookie(t, response, sessionCookieName), decodeResponse[struct {
		CSRF string `json:"csrf_token"`
	}](t, response).CSRF
}

func passwordJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPasswordChangeRejectsInvalidInputAndKeepsSession(t *testing.T) {
	cases := []struct {
		name, body, code string
		status           int
	}{
		{"wrong_current", `{"current_password":"wrong-secret","new_password":"next-secret"}`, "current_password_invalid", http.StatusBadRequest},
		{"empty_current_wrong", `{"current_password":"","new_password":"next"}`, "current_password_invalid", http.StatusBadRequest},
		{"missing_current", `{"new_password":"next"}`, "invalid_input", http.StatusBadRequest},
		{"missing_new", `{"current_password":"old-secret"}`, "invalid_input", http.StatusBadRequest},
		{"null_current", `{"current_password":null,"new_password":"next"}`, "invalid_input", http.StatusBadRequest},
		{"null_new", `{"current_password":"old-secret","new_password":null}`, "invalid_input", http.StatusBadRequest},
		{"empty_object", `{}`, "invalid_input", http.StatusBadRequest},
		{"null_body", `null`, "invalid_input", http.StatusBadRequest},
		{"unknown_field", `{"current_password":"old-secret","new_password":"next","confirm_password":"next"}`, "json_invalid", http.StatusBadRequest},
		{"non_string", `{"current_password":"old-secret","new_password":12}`, "json_invalid", http.StatusBadRequest},
		{"trailing_json", `{"current_password":"old-secret","new_password":"next"} {}`, "json_invalid", http.StatusBadRequest},
		{"malformed", `{`, "json_invalid", http.StatusBadRequest},
		{"oversized", `{"current_password":"old-secret","new_password":"` + strings.Repeat("x", maxJSONBody) + `"}`, "json_invalid", http.StatusBadRequest},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var store *controlstore.Store
			handler, _ := newTestAdminAPI(t, func(config *APIConfig) { store = config.Store })
			cookie, csrf := passwordSetup(t, handler, "old-secret")
			before, err := store.AdminByID(t.Context(), 1)
			if err != nil {
				t.Fatal(err)
			}
			response := performJSON(t, handler, http.MethodPost, "/api/v1/admin/password", test.body, []*http.Cookie{cookie}, csrf)
			if response.Code != test.status || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("change = %d %s", response.Code, response.Body.String())
			}
			if len(response.Result().Cookies()) != 0 || strings.Contains(response.Body.String(), "secret") {
				t.Fatalf("error cleared cookies or leaked secrets: %s", response.Body.String())
			}
			after, err := store.AdminByID(t.Context(), 1)
			if err != nil || after.PasswordHash != before.PasswordHash || !after.UpdatedAt.Equal(before.UpdatedAt) {
				t.Fatalf("failed change mutated password: %v", err)
			}
			if response := performJSON(t, handler, http.MethodGet, "/api/v1/admin/session", "", []*http.Cookie{cookie}, ""); response.Code != http.StatusOK {
				t.Fatalf("failed change invalidated session: %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestPasswordChangeRequiresAuthenticationCSRFAndSameOrigin(t *testing.T) {
	handler, _ := newTestAdminAPI(t, nil)
	cookie, csrf := passwordSetup(t, handler, "old")
	body := `{"current_password":"old","new_password":"new"}`
	for _, test := range []struct {
		name, token, origin, contentType, code string
		cookies                                []*http.Cookie
		status                                 int
	}{
		{name: "unauthenticated", token: csrf, contentType: "application/json", code: "unauthorized", status: http.StatusUnauthorized},
		{name: "missing_csrf", cookies: []*http.Cookie{cookie}, contentType: "application/json", code: "csrf_invalid", status: http.StatusForbidden},
		{name: "bad_csrf", token: "wrong", cookies: []*http.Cookie{cookie}, contentType: "application/json", code: "csrf_invalid", status: http.StatusForbidden},
		{name: "cross_origin", token: csrf, cookies: []*http.Cookie{cookie}, origin: "https://other.example", contentType: "application/json", code: "origin_invalid", status: http.StatusForbidden},
		{name: "wrong_content_type", token: csrf, cookies: []*http.Cookie{cookie}, contentType: "text/plain", code: "content_type_invalid", status: http.StatusUnsupportedMediaType},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/password", strings.NewReader(body))
			request.Header.Set("Content-Type", test.contentType)
			request.Header.Set(csrfHeaderName, test.token)
			request.Header.Set("Origin", test.origin)
			for _, cookie := range test.cookies {
				request.AddCookie(cookie)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("change = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestPasswordChangeAcceptsAnyLengthAndRevokesEverySession(t *testing.T) {
	for _, test := range []struct{ name, old, next string }{
		{"short", "old", "a"},
		{"empty_new", "old", ""},
		{"empty_current", "", "new"},
		{"long", "old", strings.Repeat("x", 2048)},
		{"same", "same", "same"},
		{"spaces", " old ", " next \n密码 "},
	} {
		t.Run(test.name, func(t *testing.T) {
			var store *controlstore.Store
			handler, now := newTestAdminAPI(t, func(config *APIConfig) { store = config.Store })
			cookie, csrf := passwordSetup(t, handler, test.old)
			login := performJSON(t, handler, http.MethodPost, "/api/v1/admin/login", passwordJSON(t, map[string]string{"username": "owner", "password": test.old}), nil, "")
			if login.Code != http.StatusOK {
				t.Fatalf("second login = %d %s", login.Code, login.Body.String())
			}
			otherCookie := responseCookie(t, login, sessionCookieName)
			before, err := store.AdminByID(t.Context(), 1)
			if err != nil {
				t.Fatal(err)
			}
			response := performJSON(t, handler, http.MethodPost, "/api/v1/admin/password", passwordJSON(t, map[string]string{"current_password": test.old, "new_password": test.next}), []*http.Cookie{cookie}, csrf)
			if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
				t.Fatalf("change = %d %s", response.Code, response.Body.String())
			}
			if len(response.Result().Cookies()) != 2 {
				t.Fatal("expected both cookies cleared")
			}
			for _, name := range []string{sessionCookieName, csrfCookieName} {
				cleared := responseCookie(t, response, name)
				if cleared.Value != "" || cleared.MaxAge != -1 || !cleared.Secure || cleared.Path != "/" || !cleared.Expires.Before(now) || cleared.SameSite != http.SameSiteStrictMode || cleared.HttpOnly != (name == sessionCookieName) {
					t.Fatalf("cleared cookie = %#v", cleared)
				}
			}
			after, err := store.AdminByID(t.Context(), 1)
			if err != nil || after.PasswordHash == before.PasswordHash || !strings.HasPrefix(after.PasswordHash, "$argon2id$") {
				t.Fatalf("password was not freshly hashed: %v", err)
			}
			for _, oldCookie := range []*http.Cookie{cookie, otherCookie} {
				if response := performJSON(t, handler, http.MethodGet, "/api/v1/admin/session", "", []*http.Cookie{oldCookie}, ""); response.Code != http.StatusUnauthorized {
					t.Fatalf("old session remained valid: %d", response.Code)
				}
			}
			if test.old != test.next {
				response := performJSON(t, handler, http.MethodPost, "/api/v1/admin/login", passwordJSON(t, map[string]string{"username": "owner", "password": test.old}), nil, "")
				if response.Code != http.StatusUnauthorized {
					t.Fatalf("old password login = %d", response.Code)
				}
			}
			response = performJSON(t, handler, http.MethodPost, "/api/v1/admin/login", passwordJSON(t, map[string]string{"username": "owner", "password": test.next}), nil, "")
			if response.Code != http.StatusOK {
				t.Fatalf("new password login = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestPasswordChangeRateLimitIsIndependentAndSharedAcrossSessions(t *testing.T) {
	handler, _ := newTestAdminAPI(t, func(config *APIConfig) { config.LoginLimiter = security.NewLimiter(1, time.Minute, 8) })
	cookie, csrf := passwordSetup(t, handler, "old")
	login := performJSON(t, handler, http.MethodPost, "/api/v1/admin/login", `{"username":"owner","password":"old"}`, nil, "")
	if login.Code != http.StatusOK {
		t.Fatalf("login = %d %s", login.Code, login.Body.String())
	}
	body := `{"current_password":"wrong","new_password":"new"}`
	response := performJSON(t, handler, http.MethodPost, "/api/v1/admin/password", body, []*http.Cookie{cookie}, csrf)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("login bucket affected first password attempt: %d %s", response.Code, response.Body.String())
	}
	otherCSRF := decodeResponse[struct {
		CSRF string `json:"csrf_token"`
	}](t, login).CSRF
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/password", strings.NewReader(body))
	request.RemoteAddr = "198.51.100.9:5555"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeaderName, otherCSRF)
	request.AddCookie(responseCookie(t, login, sessionCookieName))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests || !strings.Contains(response.Body.String(), `"code":"rate_limited"`) || response.Header().Get("Retry-After") == "" {
		t.Fatalf("rate limit = %d %s Retry-After=%q", response.Code, response.Body.String(), response.Header().Get("Retry-After"))
	}
}

func TestPasswordChangePreservesMFAAndRecoveryCodes(t *testing.T) {
	var store *controlstore.Store
	handler, now := newTestAdminAPI(t, func(config *APIConfig) { store = config.Store })
	cookie, csrf := passwordSetup(t, handler, "old")
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/mfa/totp/setup", "", []*http.Cookie{cookie}, csrf)
	if setup.Code != http.StatusOK {
		t.Fatalf("TOTP setup = %d %s", setup.Code, setup.Body.String())
	}
	secret := decodeResponse[struct {
		Secret string `json:"secret"`
	}](t, setup).Secret
	confirm := performJSON(t, handler, http.MethodPost, "/api/v1/admin/mfa/totp/confirm", passwordJSON(t, map[string]string{"code": testTOTPCode(t, secret, now)}), []*http.Cookie{cookie}, csrf)
	if confirm.Code != http.StatusOK {
		t.Fatalf("TOTP confirm = %d %s", confirm.Code, confirm.Body.String())
	}
	codes := decodeResponse[struct {
		Codes []string `json:"recovery_codes"`
	}](t, confirm).Codes
	before, err := store.AdminByID(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	response := performJSON(t, handler, http.MethodPost, "/api/v1/admin/password", `{"current_password":"old","new_password":"new"}`, []*http.Cookie{cookie}, csrf)
	if response.Code != http.StatusNoContent {
		t.Fatalf("change = %d %s", response.Code, response.Body.String())
	}
	after, err := store.AdminByID(t.Context(), 1)
	if err != nil || !after.TOTPConfirmed || !bytes.Equal(before.TOTPSecretCipher, after.TOTPSecretCipher) {
		t.Fatalf("MFA changed: %v", err)
	}
	response = performJSON(t, handler, http.MethodPost, "/api/v1/admin/login", `{"username":"owner","password":"new"}`, nil, "")
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":"mfa_required"`) {
		t.Fatalf("new password bypassed MFA: %d %s", response.Code, response.Body.String())
	}
	for _, code := range []string{testTOTPCode(t, secret, now), codes[0]} {
		response = performJSON(t, handler, http.MethodPost, "/api/v1/admin/login", passwordJSON(t, map[string]string{"username": "owner", "password": "new", "mfa_code": code}), nil, "")
		if response.Code != http.StatusOK {
			t.Fatalf("preserved MFA credential login = %d %s", response.Code, response.Body.String())
		}
	}
	if err := store.ConsumeRecoveryCode(t.Context(), 1, security.DigestRecoveryCode(codes[0]), now); !errors.Is(err, controlstore.ErrRecoveryCode) {
		t.Fatalf("recovery code reused: %v", err)
	}
}

func TestPasswordChangeReportsStaleAuthenticationSnapshot(t *testing.T) {
	var config APIConfig
	handler, now := newTestAdminAPI(t, func(value *APIConfig) { config = *value })
	cookie, _ := passwordSetup(t, handler, "old")
	admin, err := config.Store.AdminByID(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	session, err := config.Store.SessionByTokenDigest(t.Context(), security.DigestToken(cookie.Value), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Store.ChangePassword(t.Context(), admin.ID, session.ID, admin.PasswordHash, "another-hash", false, now); err != nil {
		t.Fatal(err)
	}
	config.LoginLimiter = security.NewLimiter(10, time.Minute, 8)
	api := &adminAPI{config: config}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/password", strings.NewReader(`{"current_password":"old","new_password":"new"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.changePassword(response, request, authenticatedRequest{admin: admin, session: session})
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"auth_state_changed"`) || len(response.Result().Cookies()) != 0 {
		t.Fatalf("stale change = %d %s", response.Code, response.Body.String())
	}
	// A previously verified login must also report 409 and issue no cookies.
	config.SessionTTL = time.Hour
	api.config = config
	response = httptest.NewRecorder()
	api.issueSession(response, request, admin, http.StatusOK)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"auth_state_changed"`) || len(response.Result().Cookies()) != 0 {
		t.Fatalf("stale login = %d %s", response.Code, response.Body.String())
	}
	got, err := config.Store.AdminByID(t.Context(), admin.ID)
	if err != nil || got.PasswordHash != "another-hash" {
		t.Fatalf("stale change mutated hash: %v", err)
	}
}

func TestPasswordChangeInternalFailuresDoNotLeakSecretsOrRevokeSession(t *testing.T) {
	for _, scenario := range []string{"corrupt_hash", "invalid_hash_params"} {
		t.Run(scenario, func(t *testing.T) {
			var config APIConfig
			handler, now := newTestAdminAPI(t, func(value *APIConfig) { config = *value })
			cookie, _ := passwordSetup(t, handler, "old-secret")
			admin, err := config.Store.AdminByID(t.Context(), 1)
			if err != nil {
				t.Fatal(err)
			}
			session, err := config.Store.SessionByTokenDigest(t.Context(), security.DigestToken(cookie.Value), now)
			if err != nil {
				t.Fatal(err)
			}
			config.LoginLimiter = security.NewLimiter(10, time.Minute, 8)
			if scenario == "corrupt_hash" {
				admin.PasswordHash = "invalid-digest-secret"
			} else {
				config.PasswordParams = security.PasswordParams{}
			}
			api := &adminAPI{config: config}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/password", strings.NewReader(`{"current_password":"old-secret","new_password":"new-secret"}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			api.changePassword(response, request, authenticatedRequest{admin: admin, session: session})
			if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), `"code":"internal_error"`) || strings.Contains(response.Body.String(), "secret") || len(response.Result().Cookies()) != 0 {
				t.Fatalf("failure = %d %s", response.Code, response.Body.String())
			}
			if _, err := config.Store.SessionByTokenDigest(t.Context(), session.TokenDigest, now); err != nil {
				t.Fatalf("failed change revoked session: %v", err)
			}
		})
	}
}
