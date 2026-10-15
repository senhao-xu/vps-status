package control

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func TestProbeAutoJoinAPIContractAndQuota(t *testing.T) {
	var store *controlstore.Store
	handler, now := newTestAdminAPI(t, func(config *APIConfig) { store = config.Store })
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"test-password"}`, nil, "")
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup=%d", setup.Code)
	}
	var auth struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(setup.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}
	cookies := []*http.Cookie{responseCookie(t, setup, sessionCookieName)}
	created := performJSON(t, handler, http.MethodPost, "/api/v1/admin/probes", `{"name":"manual","host":"localhost","port":443,"interval_seconds":60,"node_ids":[]}`, cookies, auth.CSRFToken)
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	var task probeTaskDTO
	if err := json.Unmarshal(created.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if enabled, ok := wire["auto_join"].(bool); !ok || enabled {
		t.Fatalf("legacy default=%s", created.Body.String())
	}
	for _, tc := range []struct {
		body    string
		enabled bool
	}{{`{"auto_join":true}`, true}, {`{"name":"renamed"}`, true}, {`{"auto_join":false}`, false}} {
		response := performJSON(t, handler, http.MethodPatch, "/api/v1/admin/probes/"+task.ID, tc.body, cookies, auth.CSRFToken)
		if response.Code != http.StatusOK {
			t.Fatalf("patch=%d %s", response.Code, response.Body.String())
		}
		var result probeTaskDTO
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.AutoJoin != tc.enabled {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
	invalid := performJSON(t, handler, http.MethodPatch, "/api/v1/admin/probes/"+task.ID, `{"auto_join":"true"}`, cookies, auth.CSRFToken)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("malformed bool=%d", invalid.Code)
	}
	for i := 0; i < 64; i++ {
		if _, err := store.CreateProbeTask(t.Context(), controlstore.ProbeTask{ID: fmt.Sprintf("auto-%d", i), Name: "auto", Host: "localhost", Port: 443, IntervalSeconds: 60, AutoJoin: true}, nil, now); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ method, path, body string }{{http.MethodPost, "/api/v1/admin/probes", `{"name":"overflow","host":"localhost","port":443,"interval_seconds":60,"auto_join":true,"node_ids":[]}`}, {http.MethodPatch, "/api/v1/admin/probes/" + task.ID, `{"auto_join":true}`}} {
		response := performJSON(t, handler, tc.method, tc.path, tc.body, cookies, auth.CSRFToken)
		var result struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusConflict || result.Error.Code != "probe_auto_join_limit" {
			t.Fatalf("quota=%d %s", response.Code, response.Body.String())
		}
	}
}

// gatedProbeBody lets enrollment commit after the handler's initial task read.
type gatedProbeBody struct {
	io.Reader
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *gatedProbeBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.entered); <-b.release })
	return b.Reader.Read(p)
}
func (*gatedProbeBody) Close() error { return nil }

func TestProbePatchPreservesEnrollmentDuringDecode(t *testing.T) {
	var store *controlstore.Store
	handler, now := newTestAdminAPI(t, func(config *APIConfig) { store = config.Store })
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"test-password"}`, nil, "")
	var auth struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(setup.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}
	cookie := responseCookie(t, setup, sessionCookieName)
	task := controlstore.ProbeTask{ID: "race-task", Name: "race", Host: "localhost", Port: 443, IntervalSeconds: 60, AutoJoin: true}
	if _, err := store.CreateProbeTask(t.Context(), task, nil, now); err != nil {
		t.Fatal(err)
	}
	body := &gatedProbeBody{Reader: strings.NewReader(`{"auto_join":false}`), entered: make(chan struct{}), release: make(chan struct{})}
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/probes/"+task.ID, body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", auth.CSRFToken)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { handler.ServeHTTP(response, request); close(done) }()
	select {
	case <-body.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not reach decoder")
	}
	token := controlstore.EnrollmentToken{ID: "race-token", Digest: security.DigestEnrollmentToken("race-token"), NodeName: "new-node", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := store.CreateEnrollmentToken(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeEnrollment(t.Context(), token.Digest, "new-node", "new-credential", security.DigestNodeCredential("new-credential"), now); err != nil {
		t.Fatal(err)
	}
	close(body.release)
	<-done
	if response.Code != http.StatusOK {
		t.Fatalf("patch=%d %s", response.Code, response.Body.String())
	}
	saved, err := store.ProbeTaskByID(t.Context(), task.ID)
	if err != nil || saved.AutoJoin || len(saved.NodeIDs) != 1 || saved.NodeIDs[0] != "new-node" {
		t.Fatalf("lost enrollment assignment=%+v %v", saved, err)
	}
	clear := performJSON(t, handler, http.MethodPatch, "/api/v1/admin/probes/"+task.ID, `{"node_ids":[]}`, []*http.Cookie{cookie}, auth.CSRFToken)
	if clear.Code != http.StatusOK {
		t.Fatalf("clear=%d %s", clear.Code, clear.Body.String())
	}
	saved, err = store.ProbeTaskByID(t.Context(), task.ID)
	if err != nil || len(saved.NodeIDs) != 0 {
		t.Fatalf("explicit clear=%+v %v", saved, err)
	}
}

func TestProbeAssignmentEndpointsPreserveOtherAutoJoinedNodes(t *testing.T) {
	var store *controlstore.Store
	handler, now := newTestAdminAPI(t, func(config *APIConfig) { store = config.Store })
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"test-password"}`, nil, "")
	var auth struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(setup.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}
	cookies := []*http.Cookie{responseCookie(t, setup, sessionCookieName)}
	task := controlstore.ProbeTask{ID: "assignment-task", Name: "assignment", Host: "localhost", Port: 443, IntervalSeconds: 60, AutoJoin: true}
	if _, err := store.CreateProbeTask(t.Context(), task, nil, now); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"first", "second", "third"} {
		token := controlstore.EnrollmentToken{ID: id, Digest: security.DigestEnrollmentToken(id), NodeName: id, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
		if err := store.CreateEnrollmentToken(t.Context(), token); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ConsumeEnrollment(t.Context(), token.Digest, id, "credential-"+id, security.DigestNodeCredential(id), now); err != nil {
			t.Fatal(err)
		}
	}
	path := "/api/v1/admin/probes/" + task.ID + "/assignments/first"
	for _, method := range []string{http.MethodDelete, http.MethodPut} {
		response := performJSON(t, handler, method, path, "", cookies, auth.CSRFToken)
		if response.Code != http.StatusNoContent {
			t.Fatalf("%s=%d %s", method, response.Code, response.Body.String())
		}
		saved, err := store.ProbeTaskByID(t.Context(), task.ID)
		expected := 2
		if method == http.MethodPut {
			expected = 3
		}
		if err != nil || !saved.AutoJoin || len(saved.NodeIDs) != expected {
			t.Fatalf("saved=%+v %v", saved, err)
		}
		for _, node := range []string{"second", "third"} {
			found := false
			for _, id := range saved.NodeIDs {
				found = found || id == node
			}
			if !found {
				t.Fatalf("lost other assignment %s", node)
			}
		}
	}
}
