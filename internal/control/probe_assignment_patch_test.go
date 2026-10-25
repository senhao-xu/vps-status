package control

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/protocol"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

func TestProbeEditorAssignmentChangesPreserveEnrollmentDuringSave(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want []string
	}{
		{"rename only", `{"name":"Renamed"}`, []string{"automatic", "keep", "remove"}},
		{"explicit changes", `{"name":"Renamed","node_ids_add":["manual"],"node_ids_remove":["remove"]}`, []string{"automatic", "keep", "manual"}},
		{"disable automatic joining", `{"auto_join":false}`, []string{"automatic", "keep", "remove"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var store *controlstore.Store
			var history *ProbeHistory
			handler, now := newTestAdminAPI(t, func(config *APIConfig) {
				config.ProbeHistory = NewProbeHistory(nil)
				store, history = config.Store, config.ProbeHistory
			})
			setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"test-password"}`, nil, "")
			auth := decodeResponse[struct {
				CSRF string `json:"csrf_token"`
			}](t, setup)
			cookie := responseCookie(t, setup, sessionCookieName)
			for _, id := range []string{"keep", "remove", "manual"} {
				enrollHomepageTestNode(t, store, id, now)
			}
			task := controlstore.ProbeTask{ID: "task", Name: "Original", Host: "localhost", Port: 443, IntervalSeconds: 60, AutoJoin: true}
			if _, err := store.CreateProbeTask(t.Context(), task, []string{"keep", "remove"}, now); err != nil {
				t.Fatal(err)
			}
			history.Observe("remove", protocol.ProbeResult{TaskID: task.ID, Success: true, LatencyMillis: 1}, now)
			body := &gatedProbeBody{Reader: strings.NewReader(tc.body), entered: make(chan struct{}), release: make(chan struct{})}
			defer func() {
				select {
				case <-body.release:
				default:
					close(body.release)
				}
			}()
			request := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/probes/"+task.ID, body)
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
			// Enrollment commits after the editor/handler snapshot was read.
			enrollHomepageTestNode(t, store, "automatic", now)
			history.Observe("automatic", protocol.ProbeResult{TaskID: task.ID, Success: true, LatencyMillis: 2}, now)
			close(body.release)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("save did not finish")
			}
			if response.Code != http.StatusOK {
				t.Fatalf("save=%d %s", response.Code, response.Body.String())
			}
			saved, err := store.ProbeTaskByID(t.Context(), task.ID)
			if err != nil || !reflect.DeepEqual(saved.NodeIDs, tc.want) {
				t.Fatalf("saved=%+v error=%v want=%v", saved, err, tc.want)
			}
			if tc.name == "disable automatic joining" {
				if saved.AutoJoin {
					t.Fatal("auto_join stayed enabled")
				}
				enrollHomepageTestNode(t, store, "later", now)
				later, err := store.ProbeSnapshot(t.Context(), "later")
				if err != nil || len(later.Tasks) != 0 {
					t.Fatalf("disabled policy assigned a later node=%+v error=%v", later, err)
				}
			}
			if tc.name == "explicit changes" && len(history.Latest("remove", task.ID)) != 0 {
				t.Fatal("removed assignment retained its current result")
			}
			if len(history.Latest("automatic", task.ID)) != 1 {
				t.Fatal("untouched automatic assignment lost its current result")
			}
		})
	}
}

func TestProbeAssignmentPatchValidatesChangesAndKeepsLegacyReplacement(t *testing.T) {
	var store *controlstore.Store
	handler, now := newTestAdminAPI(t, func(config *APIConfig) { store = config.Store })
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"test-password"}`, nil, "")
	auth := decodeResponse[struct {
		CSRF string `json:"csrf_token"`
	}](t, setup)
	cookies := []*http.Cookie{responseCookie(t, setup, sessionCookieName)}
	enrollHomepageTestNode(t, store, "node", now)
	task := controlstore.ProbeTask{ID: "task", Name: "Original", Host: "localhost", Port: 443, IntervalSeconds: 60}
	if _, err := store.CreateProbeTask(t.Context(), task, []string{"node"}, now); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/admin/probes/task"
	for _, tc := range []struct {
		cookies []*http.Cookie
		csrf    string
		want    int
	}{{nil, "", http.StatusUnauthorized}, {cookies, "", http.StatusForbidden}} {
		response := performJSON(t, handler, http.MethodPatch, path, `{"node_ids_remove":["node"]}`, tc.cookies, tc.csrf)
		if response.Code != tc.want {
			t.Fatalf("authorization=%d want=%d", response.Code, tc.want)
		}
	}
	for _, body := range []string{
		`{"node_ids":[],"node_ids_add":[]}`,
		`{"node_ids":["node"],"node_ids_remove":[]}`,
		`{"node_ids_add":["node","node"]}`,
		`{"node_ids_remove":["node","node"]}`,
		`{"node_ids_add":["node"],"node_ids_remove":["node"]}`,
		`{"node_ids_add":[""]}`,
		`{"node_ids_remove":["node\n"]}`,
		`{"node_ids_add":["` + strings.Repeat("x", 129) + `"]}`,
		`{"node_ids_add":"node"}`,
		`{"node_ids_remove":[1]}`,
	} {
		response := performJSON(t, handler, http.MethodPatch, path, body, cookies, auth.CSRF)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid patch=%s status=%d %s", body, response.Code, response.Body.String())
		}
		got, err := store.ProbeTaskByID(t.Context(), task.ID)
		if err != nil || got.Name != task.Name || !reflect.DeepEqual(got.NodeIDs, []string{"node"}) {
			t.Fatalf("invalid patch changed task=%+v error=%v", got, err)
		}
	}
	for _, body := range []string{`{}`, `{"node_ids_add":[],"node_ids_remove":[]}`, `{"node_ids_add":["node"],"node_ids_remove":["already-removed"]}`} {
		response := performJSON(t, handler, http.MethodPatch, path, body, cookies, auth.CSRF)
		if response.Code != http.StatusOK {
			t.Fatalf("no-op patch=%s status=%d %s", body, response.Code, response.Body.String())
		}
		got := decodeResponse[probeTaskDTO](t, response)
		if !reflect.DeepEqual(got.NodeIDs, []string{"node"}) {
			t.Fatalf("no-op changed assignments=%v", got.NodeIDs)
		}
	}
	missing := performJSON(t, handler, http.MethodPatch, path, `{"name":"must roll back","node_ids_add":["missing"],"node_ids_remove":["node"]}`, cookies, auth.CSRF)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing addition=%d %s", missing.Code, missing.Body.String())
	}
	for _, tc := range []struct {
		body string
		want []string
	}{{`{"node_ids":[]}`, []string{}}, {`{"node_ids":["node"]}`, []string{"node"}}} {
		response := performJSON(t, handler, http.MethodPatch, path, tc.body, cookies, auth.CSRF)
		if response.Code != http.StatusOK {
			t.Fatalf("legacy replacement=%d %s", response.Code, response.Body.String())
		}
		got := decodeResponse[probeTaskDTO](t, response)
		if !reflect.DeepEqual(got.NodeIDs, tc.want) || got.Name != task.Name {
			t.Fatalf("legacy replacement=%+v want=%v", got, tc.want)
		}
	}
}

func TestProbeAssignmentDeltaQuotaReturnsConflictWithoutPartialSave(t *testing.T) {
	var store *controlstore.Store
	handler, now := newTestAdminAPI(t, func(config *APIConfig) { store = config.Store })
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"test-password"}`, nil, "")
	auth := decodeResponse[struct {
		CSRF string `json:"csrf_token"`
	}](t, setup)
	cookies := []*http.Cookie{responseCookie(t, setup, sessionCookieName)}
	for _, id := range []string{"existing", "full"} {
		enrollHomepageTestNode(t, store, id, now)
	}
	task := controlstore.ProbeTask{ID: "task", Name: "Original", Host: "localhost", Port: 443, IntervalSeconds: 60, AutoJoin: true}
	if _, err := store.CreateProbeTask(t.Context(), task, []string{"existing"}, now); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < protocol.MaxProbeTasks; i++ {
		filler := task
		filler.ID, filler.AutoJoin = fmt.Sprintf("filler-%d", i), false
		if _, err := store.CreateProbeTask(t.Context(), filler, []string{"full"}, now); err != nil {
			t.Fatal(err)
		}
	}
	before, err := store.ProbeSnapshot(t.Context(), "existing")
	if err != nil {
		t.Fatal(err)
	}
	response := performJSON(t, handler, http.MethodPatch, "/api/v1/admin/probes/task", `{"name":"must roll back","auto_join":false,"node_ids_add":["full"],"node_ids_remove":["existing"]}`, cookies, auth.CSRF)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"probe_task_limit"`) {
		t.Fatalf("quota=%d %s", response.Code, response.Body.String())
	}
	saved, err := store.ProbeTaskByID(t.Context(), task.ID)
	if err != nil || saved.Name != task.Name || !saved.AutoJoin || !reflect.DeepEqual(saved.NodeIDs, []string{"existing"}) {
		t.Fatalf("quota partially saved=%+v error=%v", saved, err)
	}
	after, err := store.ProbeSnapshot(t.Context(), "existing")
	if err != nil || after.Version != before.Version {
		t.Fatalf("quota advanced version=%+v error=%v", after, err)
	}
}
