package control

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/security"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

func enrollHomepageTestNode(t *testing.T, store *controlstore.Store, id string, now time.Time) {
	t.Helper()
	token := controlstore.EnrollmentToken{ID: "token-" + id, Digest: security.DigestEnrollmentToken("token-" + id), NodeName: id, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := store.CreateEnrollmentToken(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeEnrollment(t.Context(), token.Digest, id, "credential-"+id, security.DigestNodeCredential(id), now); err != nil {
		t.Fatal(err)
	}
}

func TestHomepageNodeSettingsAndSitePartialWrite(t *testing.T) {
	var store *controlstore.Store
	handler, now := newTestAdminAPI(t, func(config *APIConfig) { store = config.Store })
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"test"}`, nil, "")
	auth := decodeResponse[struct {
		CSRF string `json:"csrf_token"`
	}](t, setup)
	cookie := responseCookie(t, setup, sessionCookieName)
	cookies := []*http.Cookie{cookie}
	enrollHomepageTestNode(t, store, "shown", now)
	enrollHomepageTestNode(t, store, "unlisted", now)
	for _, endpoint := range []string{"/api/v1/admin/nodes", "/api/v1/admin/nodes/shown"} {
		response := performJSON(t, handler, http.MethodGet, endpoint, "", cookies, "")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"homepage_visible":true`) {
			t.Fatalf("node contract=%d %s", response.Code, response.Body.String())
		}
	}
	unauthenticated := performJSON(t, handler, http.MethodPatch, "/api/v1/admin/nodes/shown", `{"homepage_visible":false}`, nil, "")
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated visibility=%d", unauthenticated.Code)
	}
	csrf := performJSON(t, handler, http.MethodPatch, "/api/v1/admin/nodes/shown", `{"homepage_visible":false}`, cookies, "")
	if csrf.Code != http.StatusForbidden {
		t.Fatalf("visibility without csrf=%d", csrf.Code)
	}
	for _, body := range []string{`{"homepage_visible":"false"}`, `{"name":"","homepage_visible":false}`, `{"name":"` + strings.Repeat("x", 129) + `"}`} {
		invalid := performJSON(t, handler, http.MethodPatch, "/api/v1/admin/nodes/shown", body, cookies, auth.CSRF)
		if invalid.Code != http.StatusBadRequest {
			t.Fatalf("invalid patch=%d %s", invalid.Code, invalid.Body.String())
		}
	}
	change := performJSON(t, handler, http.MethodPatch, "/api/v1/admin/nodes/unlisted", `{"name":"Excluded node","hidden":true,"homepage_visible":false}`, cookies, auth.CSRF)
	if change.Code != http.StatusOK || !strings.Contains(change.Body.String(), `"homepage_visible":false`) {
		t.Fatalf("visibility patch=%d %s", change.Code, change.Body.String())
	}
	// Site forms send no stale node IDs. Preserve the excluded choice and a new enrollment.
	enrollHomepageTestNode(t, store, "new", now)
	save := performJSON(t, handler, http.MethodPut, "/api/v1/admin/public-page", `{"enabled":true,"title":"My fleet","description":"Status"}`, cookies, auth.CSRF)
	if save.Code != http.StatusOK {
		t.Fatalf("site save=%d %s", save.Code, save.Body.String())
	}
	var config publicPageConfigDTO
	if err := json.Unmarshal(save.Body.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	if len(config.NodeIDs) != 2 {
		t.Fatalf("site save replaced membership=%v", config.NodeIDs)
	}
	page := performJSON(t, handler, http.MethodGet, "/api/v1/public/status", "", nil, "")
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `"name":"shown"`) || !strings.Contains(page.Body.String(), `"name":"new"`) || strings.Contains(page.Body.String(), "Excluded node") {
		t.Fatalf("homepage membership=%d %s", page.Code, page.Body.String())
	}
	// Admin hiding is independent of homepage visibility.
	change = performJSON(t, handler, http.MethodPatch, "/api/v1/admin/nodes/shown", `{"hidden":true}`, cookies, auth.CSRF)
	if change.Code != http.StatusOK || !strings.Contains(change.Body.String(), `"homepage_visible":true`) {
		t.Fatalf("hidden changed display=%d %s", change.Code, change.Body.String())
	}
	clear := performJSON(t, handler, http.MethodPut, "/api/v1/admin/public-page", `{"enabled":true,"title":"My fleet","description":"","node_ids":[]}`, cookies, auth.CSRF)
	if clear.Code != http.StatusOK || !strings.Contains(clear.Body.String(), `"node_ids":[]`) {
		t.Fatalf("legacy explicit clear=%d %s", clear.Code, clear.Body.String())
	}
}

func TestHomepageNodePatchMergesChangesDuringDecode(t *testing.T) {
	var store *controlstore.Store
	handler, now := newTestAdminAPI(t, func(config *APIConfig) { store = config.Store })
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"test"}`, nil, "")
	auth := decodeResponse[struct {
		CSRF string `json:"csrf_token"`
	}](t, setup)
	cookie := responseCookie(t, setup, sessionCookieName)
	enrollHomepageTestNode(t, store, "node", now)
	body := &gatedProbeBody{Reader: strings.NewReader(`{"hidden":true}`), entered: make(chan struct{}), release: make(chan struct{})}
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/nodes/node", body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", auth.CSRF)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { handler.ServeHTTP(response, request); close(done) }()
	select {
	case <-body.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not reach decoder")
	}
	// A second mutation commits after any pre-decode read in an old handler.
	other := performJSON(t, handler, http.MethodPatch, "/api/v1/admin/nodes/node", `{"name":"Renamed","group":"SG","homepage_visible":false}`, []*http.Cookie{cookie}, auth.CSRF)
	close(body.release)
	<-done
	if other.Code != http.StatusOK || response.Code != http.StatusOK {
		t.Fatalf("patches=%d %d", other.Code, response.Code)
	}
	node, err := store.NodeByID(t.Context(), "node")
	if err != nil || node.Name != "Renamed" || node.Group != "SG" || !node.Hidden || node.HomepageVisible {
		t.Fatalf("lost concurrent node changes=%+v %v", node, err)
	}
}

func TestHomepageSiteSavePreservesEnrollmentAndNodeChoiceDuringDecode(t *testing.T) {
	var store *controlstore.Store
	handler, now := newTestAdminAPI(t, func(config *APIConfig) { store = config.Store })
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"test"}`, nil, "")
	auth := decodeResponse[struct {
		CSRF string `json:"csrf_token"`
	}](t, setup)
	cookie := responseCookie(t, setup, sessionCookieName)
	enrollHomepageTestNode(t, store, "old", now)
	body := &gatedProbeBody{Reader: strings.NewReader(`{"enabled":true,"title":"Saved site","description":""}`), entered: make(chan struct{}), release: make(chan struct{})}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/public-page", body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", auth.CSRF)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { handler.ServeHTTP(response, request); close(done) }()
	select {
	case <-body.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not reach decoder")
	}
	enrollHomepageTestNode(t, store, "new", now)
	visible := false
	_, err := store.PatchNode(t.Context(), "old", controlstore.NodePatch{HomepageVisible: &visible}, now)
	close(body.release)
	<-done
	if err != nil || response.Code != http.StatusOK {
		t.Fatalf("save=%d %v %s", response.Code, err, response.Body.String())
	}
	page, err := store.PublicPageConfig(t.Context())
	if err != nil || len(page.NodeIDs) != 1 || page.NodeIDs[0] != "new" {
		t.Fatalf("lost concurrent membership=%+v %v", page, err)
	}
}

func TestHomepageNodeCapacityReturnsStableConflict(t *testing.T) {
	var store *controlstore.Store
	handler, now := newTestAdminAPI(t, func(config *APIConfig) { store = config.Store })
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"test"}`, nil, "")
	auth := decodeResponse[struct {
		CSRF string `json:"csrf_token"`
	}](t, setup)
	cookie := responseCookie(t, setup, sessionCookieName)
	// Seed the bounded configuration set without generating a thousand credentials.
	db, err := sql.Open("sqlite", store.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < controlstore.MaxPublicNodes; i++ {
		id := fmt.Sprintf("cap-%d", i)
		if _, err := tx.Exec(`INSERT INTO nodes(id,name,created_at,updated_at) VALUES(?,?,1,1)`, id, id); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO public_nodes VALUES(?,1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	enrollHomepageTestNode(t, store, "overflow", now)
	response := performJSON(t, handler, http.MethodPatch, "/api/v1/admin/nodes/overflow", `{"name":"must not save","homepage_visible":true}`, []*http.Cookie{cookie}, auth.CSRF)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"homepage_node_limit"`) {
		t.Fatalf("capacity response=%d %s", response.Code, response.Body.String())
	}
	node, err := store.NodeByID(t.Context(), "overflow")
	if err != nil || node.Name != "overflow" || node.HomepageVisible {
		t.Fatalf("capacity partially saved=%+v %v", node, err)
	}
}

func TestRestrictedHomepageRejectsExpiredSessionAndHidesStorageFailures(t *testing.T) {
	var store *controlstore.Store
	handler, now := newTestAdminAPI(t, func(config *APIConfig) { store = config.Store })
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"test"}`, nil, "")
	auth := decodeResponse[struct {
		CSRF string `json:"csrf_token"`
	}](t, setup)
	cookie := responseCookie(t, setup, sessionCookieName)
	save := performJSON(t, handler, http.MethodPut, "/api/v1/admin/public-page", `{"enabled":false,"title":"Private title","description":""}`, []*http.Cookie{cookie}, auth.CSRF)
	if save.Code != http.StatusOK {
		t.Fatalf("restrict=%d %s", save.Code, save.Body.String())
	}
	db, err := sql.Open("sqlite", store.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE sessions SET created_at=?,expires_at=?`, now.Add(-2*time.Second).Unix(), now.Add(-time.Second).Unix()); err != nil {
		t.Fatal(err)
	}
	expired := performJSON(t, handler, http.MethodGet, "/api/v1/public/status", "", []*http.Cookie{cookie}, "")
	if expired.Code != http.StatusUnauthorized || strings.Contains(expired.Body.String(), "Private title") {
		t.Fatalf("expired homepage=%d %s", expired.Code, expired.Body.String())
	}
	if _, err := db.Exec(`DROP TABLE sessions`); err != nil {
		t.Fatal(err)
	}
	failed := performJSON(t, handler, http.MethodGet, "/api/v1/public/status", "", []*http.Cookie{cookie}, "")
	if failed.Code != http.StatusInternalServerError || !strings.Contains(failed.Body.String(), `"code":"internal_error"`) {
		t.Fatalf("failed auth storage=%d %s", failed.Code, failed.Body.String())
	}
	for _, secret := range []string{"Private title", "SELECT", "sessions", store.Path(), cookie.Value} {
		if strings.Contains(failed.Body.String(), secret) {
			t.Fatalf("storage failure leaked %q: %s", secret, failed.Body.String())
		}
	}
}
