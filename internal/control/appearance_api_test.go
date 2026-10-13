package control

import (
	"net/http"
	"strings"
	"testing"

	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

func TestAppearanceAPIAllowlistAuthenticationAndCSRF(t *testing.T) {
	handler, _ := newTestAdminAPI(t, nil)
	public := performJSON(t, handler, http.MethodGet, "/api/v1/public/appearance", "", nil, "")
	if public.Code != http.StatusOK || strings.TrimSpace(public.Body.String()) != `{"default_theme":"emerald"}` || public.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("initial public appearance=%d %s", public.Code, public.Body.String())
	}
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		response := performJSON(t, handler, method, "/api/v1/admin/appearance", `{"default_theme":"classic"}`, nil, "")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s=%d", method, response.Code)
		}
	}
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"very-long-test-password"}`, nil, "")
	auth := decodeResponse[struct {
		CSRFToken string `json:"csrf_token"`
	}](t, setup)
	cookies := []*http.Cookie{responseCookie(t, setup, sessionCookieName)}
	for _, csrf := range []string{"", "wrong-csrf"} {
		response := performJSON(t, handler, http.MethodPut, "/api/v1/admin/appearance", `{"default_theme":"classic"}`, cookies, csrf)
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"csrf_invalid"`) {
			t.Fatalf("missing/wrong CSRF=%d %s", response.Code, response.Body.String())
		}
	}
	for _, body := range []string{`{"default_theme":"unknown"}`, `{"default_theme":"Emerald"}`, `{"default_theme":""}`, `{}`, `null`, `{"default_theme":"classic","custom_js":"payload"}`, `{"default_theme":"classic"} {}`} {
		response := performJSON(t, handler, http.MethodPut, "/api/v1/admin/appearance", body, cookies, auth.CSRFToken)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid body %s=%d %s", body, response.Code, response.Body.String())
		}
	}
	unchanged := performJSON(t, handler, http.MethodGet, "/api/v1/admin/appearance", "", cookies, "")
	if unchanged.Code != http.StatusOK || strings.TrimSpace(unchanged.Body.String()) != `{"default_theme":"emerald"}` {
		t.Fatalf("invalid write changed config: %d %s", unchanged.Code, unchanged.Body.String())
	}
	for _, theme := range []string{"classic", "emerald"} {
		body := `{"default_theme":"` + theme + `"}`
		response := performJSON(t, handler, http.MethodPut, "/api/v1/admin/appearance", body, cookies, auth.CSRFToken)
		public := performJSON(t, handler, http.MethodGet, "/api/v1/public/appearance", "", nil, "")
		if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != body || strings.TrimSpace(public.Body.String()) != body {
			t.Fatalf("save theme=%d %s public=%s", response.Code, response.Body.String(), public.Body.String())
		}
	}
}

func TestPublicAppearanceStorageFailureIsRedacted(t *testing.T) {
	var store *controlstore.Store
	handler, _ := newTestAdminAPI(t, func(config *APIConfig) { store = config.Store })
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	response := performJSON(t, handler, http.MethodGet, "/api/v1/public/appearance", "", nil, "")
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), `"code":"internal_error"`) || strings.Contains(response.Body.String(), store.Path()) {
		t.Fatalf("storage failure=%d %s", response.Code, response.Body.String())
	}
}
