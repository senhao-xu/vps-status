package control

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/protocol"
	"github.com/senhao-xu/vps-status/internal/security"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

func TestEnrollmentStatusLifecycleAndRevocation(t *testing.T) {
	live := NewLiveStore()
	var store *controlstore.Store
	now := time.Unix(1_700_000_000, 0).UTC()
	handler, _ := newTestAdminAPI(t, func(config *APIConfig) {
		store, config.Live = config.Store, live
		config.Now = func() time.Time { return now }
	})
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"very-long-test-password"}`, nil, "")
	auth := decodeResponse[struct {
		CSRFToken string `json:"csrf_token"`
	}](t, setup)
	cookies := []*http.Cookie{responseCookie(t, setup, sessionCookieName)}
	created := performJSON(t, handler, http.MethodPost, "/api/v1/admin/enrollments", `{"node_name":"edge","control_url":"https://control.example"}`, cookies, auth.CSRFToken)
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	enrollment := decodeResponse[struct {
		ID    string `json:"id"`
		Token string `json:"enrollment_token"`
	}](t, created)
	path := "/api/v1/admin/enrollments/" + enrollment.ID
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		response := performJSON(t, handler, method, path, "", nil, "")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s=%d", method, response.Code)
		}
	}
	for _, csrf := range []string{"", "wrong-csrf"} {
		response := performJSON(t, handler, http.MethodDelete, path, "", cookies, csrf)
		if response.Code != http.StatusForbidden {
			t.Fatalf("DELETE invalid CSRF=%d", response.Code)
		}
	}
	checkStatus := func(status, nodeID string) {
		t.Helper()
		response := performJSON(t, handler, http.MethodGet, path, "", cookies, "")
		if response.Code != http.StatusOK {
			t.Fatalf("status=%d %s", response.Code, response.Body.String())
		}
		dto := decodeResponse[enrollmentStatusDTO](t, response)
		keys := decodeResponse[map[string]any](t, response)
		wantKeys := 4
		if nodeID != "" {
			wantKeys++
		}
		if dto.ID != enrollment.ID || dto.NodeName != "edge" || dto.Status != status || dto.NodeID != nodeID || len(keys) != wantKeys || strings.Contains(response.Body.String(), enrollment.Token) || strings.Contains(response.Body.String(), "digest") || strings.Contains(response.Body.String(), "credential") {
			t.Fatalf("unexpected status DTO=%+v payload=%s", dto, response.Body.String())
		}
	}
	checkStatus("pending", "")
	if _, err := store.ConsumeEnrollment(t.Context(), security.DigestEnrollmentToken(enrollment.Token), "enrolled-node", "enrolled-credential", security.DigestNodeCredential("private-credential"), now); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateNode(t.Context(), "enrolled-node", "renamed", "", false, now); err != nil {
		t.Fatal(err)
	}
	checkStatus("registered", "enrolled-node")
	live.setConnected("enrolled-node", "session", now)
	checkStatus("registered", "enrolled-node")
	if !live.acceptReport("enrolled-node", "session", protocol.Report{CollectedAtUnixMilli: now.UnixMilli()}, now) {
		t.Fatal("report rejected")
	}
	checkStatus("online", "enrolled-node")
	live.markStale("enrolled-node", "session", now)
	checkStatus("registered", "enrolled-node")
	if !live.acceptReport("enrolled-node", "session", protocol.Report{CollectedAtUnixMilli: now.UnixMilli()}, now) {
		t.Fatal("report after stale rejected")
	}
	checkStatus("online", "enrolled-node")
	revoked := performJSON(t, handler, http.MethodDelete, path, "", cookies, auth.CSRFToken)
	if revoked.Code != http.StatusConflict || !strings.Contains(revoked.Body.String(), `"code":"enrollment_consumed"`) {
		t.Fatalf("consumed DELETE=%d %s", revoked.Code, revoked.Body.String())
	}
	// Expiry applies to unconsumed tokens only, never a successfully registered node.
	now = now.Add(time.Hour)
	checkStatus("online", "enrolled-node")
	live.setDisconnected("enrolled-node", "session", now)
	checkStatus("registered", "enrolled-node")
	// Fresh creation prunes expired records; a previously registered node remains.
	newEnrollment := performJSON(t, handler, http.MethodPost, "/api/v1/admin/enrollments", `{"node_name":"edge","control_url":"https://control.example"}`, cookies, auth.CSRFToken)
	if newEnrollment.Code != http.StatusCreated {
		t.Fatalf("second create=%d %s", newEnrollment.Code, newEnrollment.Body.String())
	}
	missing := performJSON(t, handler, http.MethodGet, path, "", cookies, "")
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), `"code":"not_found"`) {
		t.Fatalf("pruned GET=%d %s", missing.Code, missing.Body.String())
	}
	if _, err := store.NodeByID(t.Context(), "enrolled-node"); err != nil {
		t.Fatal(err)
	}
	newID := decodeResponse[struct {
		ID string `json:"id"`
	}](t, newEnrollment).ID
	path = "/api/v1/admin/enrollments/" + newID
	now = now.Add(time.Hour)
	expired := performJSON(t, handler, http.MethodGet, path, "", cookies, "")
	if dto := decodeResponse[enrollmentStatusDTO](t, expired); expired.Code != http.StatusOK || dto.Status != "expired" || dto.NodeID != "" {
		t.Fatalf("expired=%d %s", expired.Code, expired.Body.String())
	}
	cancel := performJSON(t, handler, http.MethodDelete, path, "", cookies, auth.CSRFToken)
	if cancel.Code != http.StatusNoContent || cancel.Body.Len() != 0 {
		t.Fatalf("cancel=%d %s", cancel.Code, cancel.Body.String())
	}
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		for _, id := range []string{newID, "missing", strings.Repeat("x", 129)} {
			response := performJSON(t, handler, method, fmt.Sprintf("/api/v1/admin/enrollments/%s", id), "", cookies, auth.CSRFToken)
			if response.Code != http.StatusNotFound {
				t.Fatalf("%s missing enrollment=%d %s", method, response.Code, response.Body.String())
			}
		}
	}
}
