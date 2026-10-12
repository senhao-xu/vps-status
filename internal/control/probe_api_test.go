package control

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/security"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

func TestProbeCRUDRequiresSessionCSRFAndValidatesSafetyBounds(t *testing.T) {
	var store *controlstore.Store
	handler, now := newTestAdminAPI(t, func(config *APIConfig) { store = config.Store })
	for _, nodeID := range []string{"node-a", "node-b"} {
		token := controlstore.EnrollmentToken{ID: "token-" + nodeID, Digest: security.DigestEnrollmentToken("token-" + nodeID), NodeName: nodeID, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
		if err := store.CreateEnrollmentToken(t.Context(), token); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ConsumeEnrollment(t.Context(), token.Digest, nodeID, "credential-"+nodeID, security.DigestNodeCredential("secret-"+nodeID), now); err != nil {
			t.Fatal(err)
		}
	}
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"very-long-test-password"}`, nil, "")
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup=%d %s", setup.Code, setup.Body.String())
	}
	var auth struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(setup.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}
	cookie := responseCookie(t, setup, sessionCookieName)
	missingCSRF := performJSON(t, handler, http.MethodPost, "/api/v1/admin/probes", `{"name":"web","host":"127.0.0.1","port":443,"interval_seconds":5,"node_ids":["node-a"]}`, []*http.Cookie{cookie}, "")
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF=%d", missingCSRF.Code)
	}
	for _, body := range []string{
		`{"name":"bad","host":"0.0.0.0","port":443,"interval_seconds":5,"node_ids":["node-a"]}`,
		`{"name":"bad","host":"example.com","port":0,"interval_seconds":5,"node_ids":["node-a"]}`,
		`{"name":"bad","host":"example.com","port":443,"interval_seconds":4,"node_ids":["node-a"]}`,
		`{"name":"bad","host":"example.com","port":443,"interval_seconds":3601,"node_ids":["node-a"]}`,
	} {
		response := performJSON(t, handler, http.MethodPost, "/api/v1/admin/probes", body, []*http.Cookie{cookie}, auth.CSRFToken)
		if response.Code != http.StatusBadRequest || !json.Valid(response.Body.Bytes()) {
			t.Fatalf("invalid probe=%d %s", response.Code, response.Body.String())
		}
	}
	created := performJSON(t, handler, http.MethodPost, "/api/v1/admin/probes", `{"name":"web","host":"localhost","port":443,"interval_seconds":5,"node_ids":["node-a","node-b"]}`, []*http.Cookie{cookie}, auth.CSRFToken)
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	var task probeTaskDTO
	if err := json.Unmarshal(created.Body.Bytes(), &task); err != nil || len(task.NodeIDs) != 2 {
		t.Fatalf("task=%#v err=%v", task, err)
	}
	patched := performJSON(t, handler, http.MethodPatch, "/api/v1/admin/probes/"+task.ID, `{"interval_seconds":3600,"node_ids":["node-a"]}`, []*http.Cookie{cookie}, auth.CSRFToken)
	if patched.Code != http.StatusOK {
		t.Fatalf("patch=%d %s", patched.Code, patched.Body.String())
	}
	listed := performJSON(t, handler, http.MethodGet, "/api/v1/admin/probes", "", []*http.Cookie{cookie}, "")
	if listed.Code != http.StatusOK || !json.Valid(listed.Body.Bytes()) {
		t.Fatalf("list=%d %s", listed.Code, listed.Body.String())
	}
	deleted := performJSON(t, handler, http.MethodDelete, "/api/v1/admin/probes/"+task.ID, "", []*http.Cookie{cookie}, auth.CSRFToken)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete=%d %s", deleted.Code, deleted.Body.String())
	}
	jobs, err := store.ListProbeCleanupJobs(t.Context(), 10)
	if err != nil || len(jobs) != 1 || jobs[0].TaskID != task.ID {
		t.Fatalf("cleanup jobs=%#v err=%v", jobs, err)
	}
}
