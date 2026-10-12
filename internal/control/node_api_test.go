package control

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/protocol"
	"github.com/senhao-xu/vps-status/internal/security"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

func TestEnrollmentAPIIsOneTimeAndCredentialsRotate(t *testing.T) {
	handler, _ := newTestAdminAPI(t, nil)
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"very-long-test-password"}`, nil, "")
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup=%d %s", setup.Code, setup.Body.String())
	}
	var auth struct {
		CSRFToken string `json:"csrf_token"`
	}
	auth = decodeResponse[struct {
		CSRFToken string `json:"csrf_token"`
	}](t, setup)
	cookie := responseCookie(t, setup, sessionCookieName)
	created := performJSON(t, handler, http.MethodPost, "/api/v1/admin/enrollments", `{"node_name":"edge-1","control_url":"https://control.example","expires_in_seconds":600}`, []*http.Cookie{cookie}, auth.CSRFToken)
	if created.Code != http.StatusCreated {
		t.Fatalf("create enrollment=%d %s", created.Code, created.Body.String())
	}
	var enrollment struct {
		Token   string `json:"enrollment_token"`
		Command string `json:"install_command"`
	}
	enrollment = decodeResponse[struct {
		Token   string `json:"enrollment_token"`
		Command string `json:"install_command"`
	}](t, created)
	if enrollment.Token == "" || !strings.Contains(enrollment.Command, enrollment.Token) || strings.Contains(enrollment.Command, "?token=") {
		t.Fatalf("enrollment response=%#v", enrollment)
	}

	enroll := func(token string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/agent/v1/enroll", nil)
		request.RemoteAddr = "192.0.2.8:1234"
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	first := enroll(enrollment.Token)
	if first.Code != http.StatusCreated {
		t.Fatalf("enroll=%d %s", first.Code, first.Body.String())
	}
	var node struct {
		NodeID     string `json:"node_id"`
		Credential string `json:"credential"`
	}
	node = decodeResponse[struct {
		NodeID     string `json:"node_id"`
		Credential string `json:"credential"`
	}](t, first)
	if node.NodeID == "" || node.Credential == "" || strings.Contains(first.Body.String(), enrollment.Token) {
		t.Fatalf("node response=%#v", node)
	}
	if reused := enroll(enrollment.Token); reused.Code != http.StatusUnauthorized {
		t.Fatalf("reused enrollment=%d", reused.Code)
	}

	rotated := performJSON(t, handler, http.MethodPost, "/api/v1/admin/nodes/"+node.NodeID+"/credentials/rotate", "", []*http.Cookie{cookie}, auth.CSRFToken)
	if rotated.Code != http.StatusOK || strings.Contains(rotated.Body.String(), node.Credential) {
		t.Fatalf("rotate=%d %s", rotated.Code, rotated.Body.String())
	}
	revoked := performJSON(t, handler, http.MethodDelete, "/api/v1/admin/nodes/"+node.NodeID+"/credentials", "", []*http.Cookie{cookie}, auth.CSRFToken)
	if revoked.Code != http.StatusNoContent {
		t.Fatalf("revoke=%d %s", revoked.Code, revoked.Body.String())
	}
}

func TestAgentAuthenticationDistinguishesStorageFailure(t *testing.T) {
	directory := t.TempDir()
	store, err := controlstore.Open(t.Context(), filepath.Join(directory, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := security.LoadOrCreateMasterKey(filepath.Join(directory, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(APIConfig{Store: store, MasterKey: key, Web: http.NotFoundHandler()})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/agent/v1/ws", nil)
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), `"code":"internal_error"`) {
		t.Fatalf("storage failure=%d %s", response.Code, response.Body.String())
	}
}

func TestEnrollmentRejectsInsecureRemoteControlURL(t *testing.T) {
	handler, _ := newTestAdminAPI(t, nil)
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"very-long-test-password"}`, nil, "")
	var auth struct {
		CSRFToken string `json:"csrf_token"`
	}
	auth = decodeResponse[struct {
		CSRFToken string `json:"csrf_token"`
	}](t, setup)
	response := performJSON(t, handler, http.MethodPost, "/api/v1/admin/enrollments", `{"node_name":"edge","control_url":"http://control.example"}`, []*http.Cookie{responseCookie(t, setup, sessionCookieName)}, auth.CSRFToken)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("insecure URL=%d %s", response.Code, response.Body.String())
	}
}

func TestEnrollmentCreationMapsActiveLimitAndRejectsExpiryOverflow(t *testing.T) {
	var store *controlstore.Store
	handler, now := newTestAdminAPI(t, func(config *APIConfig) { store = config.Store })
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"very-long-test-password"}`, nil, "")
	auth := decodeResponse[struct {
		CSRFToken string `json:"csrf_token"`
	}](t, setup)
	cookie := responseCookie(t, setup, sessionCookieName)

	overflow := performJSON(t, handler, http.MethodPost, "/api/v1/admin/enrollments",
		fmt.Sprintf(`{"node_name":"edge","control_url":"https://control.example","expires_in_seconds":%d}`, int64(math.MaxInt64)),
		[]*http.Cookie{cookie}, auth.CSRFToken)
	if overflow.Code != http.StatusBadRequest || !strings.Contains(overflow.Body.String(), `"code":"invalid_expiry"`) {
		t.Fatalf("overflow expiry=%d %s", overflow.Code, overflow.Body.String())
	}

	for index := 0; index < controlstore.MaxActiveEnrollmentTokens; index++ {
		plaintext := fmt.Sprintf("token-%d", index)
		err := store.CreateEnrollmentToken(t.Context(), controlstore.EnrollmentToken{
			ID: plaintext, Digest: security.DigestEnrollmentToken(plaintext), NodeName: "node",
			CreatedAt: now, ExpiresAt: now.Add(time.Hour),
		})
		if err != nil {
			t.Fatalf("seed active enrollment %d: %v", index, err)
		}
	}
	limited := performJSON(t, handler, http.MethodPost, "/api/v1/admin/enrollments",
		`{"node_name":"edge","control_url":"https://control.example"}`,
		[]*http.Cookie{cookie}, auth.CSRFToken)
	if limited.Code != http.StatusConflict || !strings.Contains(limited.Body.String(), `"code":"enrollment_token_limit"`) {
		t.Fatalf("active enrollment limit=%d %s", limited.Code, limited.Body.String())
	}
}

func TestNodeManagementProjectsLiveStateAndDeletesAtomically(t *testing.T) {
	live := NewLiveStore()
	registry := NewRegistry()
	var store *controlstore.Store
	handler, now := newTestAdminAPI(t, func(config *APIConfig) {
		store, config.Live, config.Registry = config.Store, live, registry
	})
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"very-long-test-password"}`, nil, "")
	auth := decodeResponse[struct {
		CSRFToken string `json:"csrf_token"`
	}](t, setup)
	cookie := responseCookie(t, setup, sessionCookieName)

	token := controlstore.EnrollmentToken{
		ID: "manage-token", Digest: security.DigestEnrollmentToken("manage-token"), NodeName: "edge",
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if err := store.CreateEnrollmentToken(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	credential := security.DigestNodeCredential("manage-credential")
	if _, err := store.ConsumeEnrollment(t.Context(), token.Digest, "managed-node", "managed-credential-id", credential, now); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateNodeFacts(t.Context(), "managed-node", controlstore.NodeFacts{
		Hostname: "private-host", OS: "Linux", Kernel: "6.8", Architecture: "amd64", Virtualization: "kvm",
	}, now); err != nil {
		t.Fatal(err)
	}
	live.setConnected("managed-node", "session", now)
	if !live.acceptReport("managed-node", "session", protocol.Report{CollectedAtUnixMilli: now.UnixMilli(), ProcessCount: 9}, now) {
		t.Fatal("live report rejected")
	}

	unauthorized := performJSON(t, handler, http.MethodGet, "/api/v1/admin/nodes", "", nil, "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized list=%d", unauthorized.Code)
	}
	list := performJSON(t, handler, http.MethodGet, "/api/v1/admin/nodes", "", []*http.Cookie{cookie}, "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"hostname":"private-host"`) ||
		!strings.Contains(list.Body.String(), `"online":true`) || strings.Contains(list.Body.String(), "manage-credential") {
		t.Fatalf("node list=%d %s", list.Code, list.Body.String())
	}
	updated := performJSON(t, handler, http.MethodPatch, "/api/v1/admin/nodes/managed-node", `{"name":"Edge Display","group":"hk","hidden":true}`, []*http.Cookie{cookie}, auth.CSRFToken)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"name":"Edge Display"`) || !strings.Contains(updated.Body.String(), `"group":"hk"`) {
		t.Fatalf("update node=%d %s", updated.Code, updated.Body.String())
	}
	invalid := performJSON(t, handler, http.MethodPatch, "/api/v1/admin/nodes/managed-node", `{"unknown":true}`, []*http.Cookie{cookie}, auth.CSRFToken)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("unknown node field=%d %s", invalid.Code, invalid.Body.String())
	}

	closed := make(chan string, 1)
	registry.register("managed-node", "session", func(reason string) { closed <- reason })
	deleted := performJSON(t, handler, http.MethodDelete, "/api/v1/admin/nodes/managed-node", "", []*http.Cookie{cookie}, auth.CSRFToken)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete node=%d %s", deleted.Code, deleted.Body.String())
	}
	if reason := <-closed; reason != "node_deleted" {
		t.Fatalf("disconnect reason=%q", reason)
	}
	if detail := performJSON(t, handler, http.MethodGet, "/api/v1/admin/nodes/managed-node", "", []*http.Cookie{cookie}, ""); detail.Code != http.StatusNotFound {
		t.Fatalf("deleted detail=%d %s", detail.Code, detail.Body.String())
	}
	if _, _, err := store.AuthenticateNodeCredential(t.Context(), credential); !errors.Is(err, controlstore.ErrCredentialRevoked) {
		t.Fatalf("deleted credential error=%v", err)
	}
	if snapshot := live.Snapshot("managed-node"); snapshot != (LiveSnapshot{}) {
		t.Fatalf("deleted live snapshot=%#v", snapshot)
	}
}

type lockedSSERecorder struct {
	mu     sync.Mutex
	header http.Header
	body   bytes.Buffer
	status int
}

type blockingSSERecorder struct {
	mu        sync.Mutex
	header    http.Header
	writes    int
	deadlines int
	ready     chan struct{}
	blocked   chan struct{}
	release   chan struct{}
}

func newBlockingSSERecorder() *blockingSSERecorder {
	return &blockingSSERecorder{
		header: make(http.Header), ready: make(chan struct{}), blocked: make(chan struct{}), release: make(chan struct{}),
	}
}

func (recorder *blockingSSERecorder) Header() http.Header { return recorder.header }
func (*blockingSSERecorder) WriteHeader(int)              {}
func (*blockingSSERecorder) Flush()                       {}
func (recorder *blockingSSERecorder) SetWriteDeadline(time.Time) error {
	recorder.mu.Lock()
	recorder.deadlines++
	recorder.mu.Unlock()
	return nil
}
func (recorder *blockingSSERecorder) Write(data []byte) (int, error) {
	recorder.mu.Lock()
	recorder.writes++
	write := recorder.writes
	recorder.mu.Unlock()
	if write == 1 {
		close(recorder.ready)
		return len(data), nil
	}
	if write == 2 {
		close(recorder.blocked)
	}
	<-recorder.release
	return 0, errors.New("slow SSE writer released")
}

func newLockedSSERecorder() *lockedSSERecorder          { return &lockedSSERecorder{header: make(http.Header)} }
func (recorder *lockedSSERecorder) Header() http.Header { return recorder.header }
func (recorder *lockedSSERecorder) WriteHeader(status int) {
	recorder.mu.Lock()
	if recorder.status == 0 {
		recorder.status = status
	}
	recorder.mu.Unlock()
}
func (recorder *lockedSSERecorder) Write(data []byte) (int, error) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.status == 0 {
		recorder.status = http.StatusOK
	}
	return recorder.body.Write(data)
}
func (*lockedSSERecorder) Flush() {}
func (recorder *lockedSSERecorder) String() string {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return recorder.body.String()
}

func TestAdminLiveSSEIsAuthenticatedAndStreamsBoundedDTO(t *testing.T) {
	live := NewLiveStore()
	handler, now := newTestAdminAPI(t, func(config *APIConfig) { config.Live = live })
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"very-long-test-password"}`, nil, "")
	cookie := responseCookie(t, setup, sessionCookieName)
	ctx, cancel := context.WithCancel(t.Context())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/live", nil).WithContext(ctx)
	request.AddCookie(cookie)
	recorder := newLockedSSERecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(recorder, request)
		close(done)
	}()
	eventually(t, func() bool { return strings.Contains(recorder.String(), ": ready") })
	live.setConnected("node", "session", now)
	if !live.acceptReport("node", "session", protocol.Report{CollectedAtUnixMilli: now.UnixMilli(), ProcessCount: 4}, now) {
		t.Fatal("live report rejected")
	}
	eventually(t, func() bool {
		body := recorder.String()
		return strings.Contains(body, "event: report") && strings.Contains(body, `"process_count":4`) && !strings.Contains(body, "credential")
	})
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SSE handler did not stop after cancellation")
	}
}

func TestAdminLiveSSEBoundsSlowWritesWithoutBlockingFanout(t *testing.T) {
	live := NewLiveStore()
	api := adminAPI{config: APIConfig{Live: live, Now: time.Now}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/live", nil).WithContext(ctx)
	recorder := newBlockingSSERecorder()
	done := make(chan struct{})
	go func() {
		api.liveEvents(recorder, request, authenticatedRequest{session: controlstore.Session{ExpiresAt: time.Now().Add(time.Hour)}})
		close(done)
	}()
	<-recorder.ready
	live.setConnected("node", "session", time.Now())
	<-recorder.blocked
	started := time.Now()
	for index := 0; index <= liveSubscriberQueue; index++ {
		live.setConnected("node", "session", time.Now())
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("slow SSE writer blocked fanout for %v", elapsed)
	}
	live.mu.RLock()
	remaining := len(live.subscribers)
	live.mu.RUnlock()
	if remaining != 0 {
		t.Fatalf("slow SSE subscriber was not evicted: remaining=%d", remaining)
	}
	recorder.mu.Lock()
	deadlines := recorder.deadlines
	recorder.mu.Unlock()
	if deadlines < 2 {
		t.Fatalf("SSE writes did not set per-write deadlines: count=%d", deadlines)
	}
	close(recorder.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SSE handler did not stop after its blocked write was released")
	}
}
