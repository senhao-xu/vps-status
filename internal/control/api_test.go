package control

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/security"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

var apiTestPasswordParams = security.PasswordParams{
	MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
}

func newTestAdminAPI(t *testing.T, mutate func(*APIConfig)) (http.Handler, time.Time) {
	t.Helper()
	directory := t.TempDir()
	store, err := controlstore.Open(t.Context(), filepath.Join(directory, "control.db"))
	if err != nil {
		t.Fatalf("open control store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close control store: %v", err)
		}
	})
	key, err := security.LoadOrCreateMasterKey(filepath.Join(directory, "master.key"))
	if err != nil {
		t.Fatalf("create master key: %v", err)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	config := APIConfig{
		Store: store, MasterKey: key, PasswordParams: apiTestPasswordParams,
		Now: func() time.Time { return now }, Web: http.NotFoundHandler(), WorkerContext: t.Context(),
	}
	if mutate != nil {
		mutate(&config)
	}
	handler, err := NewHandler(config)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	return handler, now
}

func performJSON(t *testing.T, handler http.Handler, method, path, body string, cookies []*http.Cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.RemoteAddr = "192.0.2.10:12345"
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		request.Header.Set(csrfHeaderName, csrf)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func responseCookie(t *testing.T, response *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("response cookie %q not found", name)
	return nil
}

func decodeResponse[T any](t *testing.T, response *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
	return value
}

func TestAdminAuthenticationMFAAndSessionLifecycle(t *testing.T) {
	handler, now := newTestAdminAPI(t, nil)

	status := performJSON(t, handler, http.MethodGet, "/api/v1/admin/setup", "", nil, "")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"configured":false`) {
		t.Fatalf("initial setup status = %d %s", status.Code, status.Body.String())
	}

	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"very-long-test-password"}`, nil, "")
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup status = %d, body = %s", setup.Code, setup.Body.String())
	}
	if strings.Contains(setup.Body.String(), "very-long-test-password") {
		t.Fatal("setup response leaked password")
	}
	var setupBody struct {
		CSRFToken string `json:"csrf_token"`
	}
	setupBody = decodeResponse[struct {
		CSRFToken string `json:"csrf_token"`
	}](t, setup)
	sessionCookie := responseCookie(t, setup, sessionCookieName)
	csrfCookie := responseCookie(t, setup, csrfCookieName)
	if !sessionCookie.Secure || !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie flags = Secure:%v HttpOnly:%v SameSite:%v", sessionCookie.Secure, sessionCookie.HttpOnly, sessionCookie.SameSite)
	}
	if !csrfCookie.Secure || csrfCookie.HttpOnly || csrfCookie.Value != setupBody.CSRFToken {
		t.Fatalf("CSRF cookie flags/value are invalid: %#v", csrfCookie)
	}

	secondSetup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"other","password":"another-long-password"}`, nil, "")
	if secondSetup.Code != http.StatusConflict {
		t.Fatalf("second setup status = %d; want 409", secondSetup.Code)
	}
	preMFALogin := performJSON(t, handler, http.MethodPost, "/api/v1/admin/login", `{"username":"owner","password":"very-long-test-password"}`, nil, "")
	if preMFALogin.Code != http.StatusOK {
		t.Fatalf("pre-MFA login status = %d, body = %s", preMFALogin.Code, preMFALogin.Body.String())
	}
	preMFASessionCookie := responseCookie(t, preMFALogin, sessionCookieName)

	missingCSRF := performJSON(t, handler, http.MethodPost, "/api/v1/admin/mfa/totp/setup", "", []*http.Cookie{sessionCookie}, "")
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("MFA setup without CSRF status = %d; want 403", missingCSRF.Code)
	}
	mfaSetup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/mfa/totp/setup", "", []*http.Cookie{sessionCookie}, setupBody.CSRFToken)
	if mfaSetup.Code != http.StatusOK {
		t.Fatalf("MFA setup status = %d, body = %s", mfaSetup.Code, mfaSetup.Body.String())
	}
	var mfaSetupBody struct {
		Secret string `json:"secret"`
		URI    string `json:"uri"`
	}
	mfaSetupBody = decodeResponse[struct {
		Secret string `json:"secret"`
		URI    string `json:"uri"`
	}](t, mfaSetup)
	if mfaSetupBody.Secret == "" || !strings.HasPrefix(mfaSetupBody.URI, "otpauth://totp/") {
		t.Fatalf("MFA setup response = %#v", mfaSetupBody)
	}
	code := testTOTPCode(t, mfaSetupBody.Secret, now)
	confirm := performJSON(t, handler, http.MethodPost, "/api/v1/admin/mfa/totp/confirm", fmt.Sprintf(`{"code":%q}`, code), []*http.Cookie{sessionCookie}, setupBody.CSRFToken)
	if confirm.Code != http.StatusOK {
		t.Fatalf("MFA confirm status = %d, body = %s", confirm.Code, confirm.Body.String())
	}
	var confirmBody struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	confirmBody = decodeResponse[struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}](t, confirm)
	if len(confirmBody.RecoveryCodes) != recoveryCodeCount {
		t.Fatalf("recovery code count = %d; want %d", len(confirmBody.RecoveryCodes), recoveryCodeCount)
	}
	staleAfterMFA := performJSON(t, handler, http.MethodGet, "/api/v1/admin/session", "", []*http.Cookie{preMFASessionCookie}, "")
	if staleAfterMFA.Code != http.StatusUnauthorized {
		t.Fatalf("pre-existing session after MFA confirmation status = %d; want 401", staleAfterMFA.Code)
	}

	logout := performJSON(t, handler, http.MethodPost, "/api/v1/admin/logout", "", []*http.Cookie{sessionCookie}, setupBody.CSRFToken)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d; want 204", logout.Code)
	}
	afterLogout := performJSON(t, handler, http.MethodGet, "/api/v1/admin/session", "", []*http.Cookie{sessionCookie}, "")
	if afterLogout.Code != http.StatusUnauthorized {
		t.Fatalf("session after logout status = %d; want 401", afterLogout.Code)
	}

	recoveryCode := confirmBody.RecoveryCodes[0]
	recoveryLogin := performJSON(t, handler, http.MethodPost, "/api/v1/admin/login", fmt.Sprintf(`{"username":"owner","password":"very-long-test-password","mfa_code":%q}`, recoveryCode), nil, "")
	if recoveryLogin.Code != http.StatusOK {
		t.Fatalf("recovery login status = %d, body = %s", recoveryLogin.Code, recoveryLogin.Body.String())
	}
	var recoveryLoginBody struct {
		CSRFToken string `json:"csrf_token"`
	}
	recoveryLoginBody = decodeResponse[struct {
		CSRFToken string `json:"csrf_token"`
	}](t, recoveryLogin)
	recoverySessionCookie := responseCookie(t, recoveryLogin, sessionCookieName)
	reusedRecovery := performJSON(t, handler, http.MethodPost, "/api/v1/admin/login", fmt.Sprintf(`{"username":"owner","password":"very-long-test-password","mfa_code":%q}`, recoveryCode), nil, "")
	if reusedRecovery.Code != http.StatusUnauthorized {
		t.Fatalf("reused recovery code status = %d; want 401", reusedRecovery.Code)
	}

	totpLogin := performJSON(t, handler, http.MethodPost, "/api/v1/admin/login", fmt.Sprintf(`{"username":"owner","password":"very-long-test-password","mfa_code":%q}`, code), nil, "")
	if totpLogin.Code != http.StatusOK {
		t.Fatalf("TOTP login status = %d, body = %s", totpLogin.Code, totpLogin.Body.String())
	}
	var totpLoginBody struct {
		CSRFToken string `json:"csrf_token"`
	}
	totpLoginBody = decodeResponse[struct {
		CSRFToken string `json:"csrf_token"`
	}](t, totpLogin)
	totpSessionCookie := responseCookie(t, totpLogin, sessionCookieName)

	sessions := performJSON(t, handler, http.MethodGet, "/api/v1/admin/sessions", "", []*http.Cookie{totpSessionCookie}, "")
	if sessions.Code != http.StatusOK {
		t.Fatalf("list sessions status = %d, body = %s", sessions.Code, sessions.Body.String())
	}
	var sessionsBody struct {
		Sessions []struct {
			ID        string     `json:"id"`
			RevokedAt *time.Time `json:"revoked_at"`
			Current   bool       `json:"current"`
		} `json:"sessions"`
	}
	sessionsBody = decodeResponse[struct {
		Sessions []struct {
			ID        string     `json:"id"`
			RevokedAt *time.Time `json:"revoked_at"`
			Current   bool       `json:"current"`
		} `json:"sessions"`
	}](t, sessions)
	var revokeID string
	for _, record := range sessionsBody.Sessions {
		if !record.Current && record.RevokedAt == nil {
			revokeID = record.ID
			break
		}
	}
	if revokeID == "" {
		t.Fatal("no non-current active session available for revocation")
	}
	revoke := performJSON(t, handler, http.MethodDelete, "/api/v1/admin/sessions/"+revokeID, "", []*http.Cookie{totpSessionCookie}, totpLoginBody.CSRFToken)
	if revoke.Code != http.StatusNoContent {
		t.Fatalf("revoke session status = %d, body = %s", revoke.Code, revoke.Body.String())
	}
	revokedSession := performJSON(t, handler, http.MethodGet, "/api/v1/admin/session", "", []*http.Cookie{recoverySessionCookie}, recoveryLoginBody.CSRFToken)
	if revokedSession.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session status = %d; want 401", revokedSession.Code)
	}

	disable := performJSON(t, handler, http.MethodPost, "/api/v1/admin/mfa/totp/disable", fmt.Sprintf(`{"password":"very-long-test-password","code":%q}`, code), []*http.Cookie{totpSessionCookie}, totpLoginBody.CSRFToken)
	if disable.Code != http.StatusNoContent {
		t.Fatalf("disable MFA status = %d, body = %s", disable.Code, disable.Body.String())
	}
	passwordOnlyLogin := performJSON(t, handler, http.MethodPost, "/api/v1/admin/login", `{"username":"owner","password":"very-long-test-password"}`, nil, "")
	if passwordOnlyLogin.Code != http.StatusOK {
		t.Fatalf("password-only login after disabling MFA = %d, body = %s", passwordOnlyLogin.Code, passwordOnlyLogin.Body.String())
	}
}

func TestOperationalEndpointsOriginGuardAndAgentConnectionLimit(t *testing.T) {
	limiter := security.NewLimiter(1, time.Minute, 8)
	handler, _ := newTestAdminAPI(t, func(config *APIConfig) {
		config.AgentConnectLimiter = limiter
	})
	for _, path := range []string{"/healthz", "/readyz"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Body.String() != "ok\n" {
			t.Fatalf("%s = %d %q", path, response.Code, response.Body.String())
		}
		if response.Header().Get("Content-Security-Policy") == "" || response.Header().Get("X-Frame-Options") != "DENY" {
			t.Fatalf("%s missing security headers", path)
		}
	}
	crossOrigin := httptest.NewRequest(http.MethodPost, "/api/v1/admin/setup", strings.NewReader(`{"username":"owner","password":"very-long-test-password"}`))
	crossOrigin.Host = "control.example"
	crossOrigin.Header.Set("Content-Type", "application/json")
	crossOrigin.Header.Set("Origin", "https://attacker.example")
	crossOriginResponse := httptest.NewRecorder()
	handler.ServeHTTP(crossOriginResponse, crossOrigin)
	if crossOriginResponse.Code != http.StatusForbidden || !strings.Contains(crossOriginResponse.Body.String(), "origin_invalid") {
		t.Fatalf("cross-origin response = %d %s", crossOriginResponse.Code, crossOriginResponse.Body.String())
	}
	proxiedSameOrigin := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	proxiedSameOrigin.Host = "control.example"
	proxiedSameOrigin.Header.Set("Origin", "https://control.example")
	proxiedSameOrigin.Header.Set("X-Forwarded-Proto", "https")
	proxiedSameOriginResponse := httptest.NewRecorder()
	handler.ServeHTTP(proxiedSameOriginResponse, proxiedSameOrigin)
	if proxiedSameOriginResponse.Code != http.StatusOK {
		t.Fatalf("proxied same-origin response = %d %s", proxiedSameOriginResponse.Code, proxiedSameOriginResponse.Body.String())
	}
	schemeMismatch := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	schemeMismatch.Host = "control.example"
	schemeMismatch.Header.Set("Origin", "http://control.example")
	schemeMismatch.Header.Set("X-Forwarded-Proto", "https")
	schemeMismatchResponse := httptest.NewRecorder()
	handler.ServeHTTP(schemeMismatchResponse, schemeMismatch)
	if schemeMismatchResponse.Code != http.StatusForbidden {
		t.Fatalf("scheme-mismatched origin response = %d; want 403", schemeMismatchResponse.Code)
	}

	first := httptest.NewRequest(http.MethodGet, "/agent/v1/ws", nil)
	first.RemoteAddr = "192.0.2.20:1000"
	firstResponse := httptest.NewRecorder()
	handler.ServeHTTP(firstResponse, first)
	if firstResponse.Code != http.StatusUnauthorized {
		t.Fatalf("first Agent attempt = %d", firstResponse.Code)
	}
	second := httptest.NewRequest(http.MethodGet, "/agent/v1/ws", nil)
	second.RemoteAddr = "192.0.2.20:1001"
	secondResponse := httptest.NewRecorder()
	handler.ServeHTTP(secondResponse, second)
	if secondResponse.Code != http.StatusTooManyRequests || secondResponse.Header().Get("Retry-After") == "" {
		t.Fatalf("second Agent attempt = %d headers=%v", secondResponse.Code, secondResponse.Header())
	}
}

func TestLoginRateLimitAndJSONBoundary(t *testing.T) {
	handler, _ := newTestAdminAPI(t, func(config *APIConfig) {
		config.LoginLimiter = security.NewLimiter(1, time.Minute, 8)
	})
	first := performJSON(t, handler, http.MethodPost, "/api/v1/admin/login", `{"username":"nobody","password":"wrong"}`, nil, "")
	if first.Code != http.StatusUnauthorized {
		t.Fatalf("first login status = %d; want 401", first.Code)
	}
	second := performJSON(t, handler, http.MethodPost, "/api/v1/admin/login", `{"username":"nobody","password":"wrong"}`, nil, "")
	if second.Code != http.StatusTooManyRequests || second.Header().Get("Retry-After") == "" {
		t.Fatalf("rate-limited login = %d Retry-After:%q", second.Code, second.Header().Get("Retry-After"))
	}
	if second.Header().Get("Cache-Control") != "no-store" || second.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("API security headers missing: %#v", second.Header())
	}

	wrongType := httptest.NewRequest(http.MethodPost, "/api/v1/admin/login", strings.NewReader(`{}`))
	wrongType.RemoteAddr = "192.0.2.11:12345"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, wrongType)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("login without JSON content type = %d; want 415", response.Code)
	}

	jsonp := httptest.NewRequest(http.MethodPost, "/api/v1/admin/login", strings.NewReader(`{}`))
	jsonp.RemoteAddr = "192.0.2.12:12345"
	jsonp.Header.Set("Content-Type", "application/jsonp")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, jsonp)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("login with JSONP content type = %d; want 415", response.Code)
	}
}

func TestAuthenticationReportsStorageFailure(t *testing.T) {
	directory := t.TempDir()
	store, err := controlstore.Open(t.Context(), filepath.Join(directory, "control.db"))
	if err != nil {
		t.Fatalf("open control store: %v", err)
	}
	key, err := security.LoadOrCreateMasterKey(filepath.Join(directory, "master.key"))
	if err != nil {
		t.Fatalf("create master key: %v", err)
	}
	handler, err := NewHandler(APIConfig{
		Store: store, MasterKey: key, PasswordParams: apiTestPasswordParams,
		Web: http.NotFoundHandler(),
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close control store: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/session", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "unavailable-storage"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), `"code":"internal_error"`) {
		t.Fatalf("storage failure response = %d %s; want stable 500", response.Code, response.Body.String())
	}
}

func TestSessionIssuanceReportsAuthenticationStateChange(t *testing.T) {
	directory := t.TempDir()
	store, err := controlstore.Open(t.Context(), filepath.Join(directory, "control.db"))
	if err != nil {
		t.Fatalf("open control store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Unix(1_700_000_000, 0).UTC()
	staleAdmin, err := store.CreateAdmin(t.Context(), "owner", "argon-hash", now)
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	var keepToken, keepCSRF, recovery [32]byte
	keepToken[0], keepCSRF[0], recovery[0] = 1, 2, 3
	if err := store.CreateSession(t.Context(), controlstore.Session{
		ID: "keep", AdminID: staleAdmin.ID, TokenDigest: keepToken, CSRFDigest: keepCSRF,
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}, false); err != nil {
		t.Fatalf("create keep session: %v", err)
	}
	if err := store.SetPendingTOTP(t.Context(), staleAdmin.ID, []byte("encrypted-secret"), now); err != nil {
		t.Fatalf("set pending TOTP: %v", err)
	}
	if err := store.ConfirmTOTP(t.Context(), staleAdmin.ID, "keep", [][32]byte{recovery}, now); err != nil {
		t.Fatalf("confirm TOTP: %v", err)
	}

	api := &adminAPI{config: APIConfig{
		Store: store, SessionTTL: time.Hour, Now: func() time.Time { return now },
	}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/login", nil)
	response := httptest.NewRecorder()
	api.issueSession(response, request, staleAdmin, http.StatusOK)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"auth_state_changed"`) {
		t.Fatalf("stale auth-state response = %d %s; want stable 409", response.Code, response.Body.String())
	}
}

func testTOTPCode(t *testing.T, secret string, now time.Time) string {
	t.Helper()
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatalf("decode TOTP secret: %v", err)
	}
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], uint64(now.Unix()/30))
	digest := hmac.New(sha1.New, decoded)
	_, _ = digest.Write(message[:])
	sum := digest.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	return fmt.Sprintf("%06d", value%1_000_000)
}
