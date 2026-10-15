package control

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/protocol"
	"github.com/senhao-xu/vps-status/internal/security"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

func TestPublicStatusUsesExplicitAllowlistAndLeaksNoPrivateFields(t *testing.T) {
	directory := t.TempDir()
	store, err := controlstore.Open(t.Context(), filepath.Join(directory, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	key, err := security.LoadOrCreateMasterKey(filepath.Join(directory, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	live := NewLiveStore()
	handler, err := NewHandler(APIConfig{Store: store, MasterKey: key, PasswordParams: apiTestPasswordParams,
		Now: func() time.Time { return now }, Web: http.NotFoundHandler(), Live: live, WorkerContext: t.Context()})
	if err != nil {
		t.Fatal(err)
	}
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"very-long-test-password"}`, nil, "")
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup = %d %s", setup.Code, setup.Body.String())
	}
	var auth struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(setup.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}
	cookie := responseCookie(t, setup, sessionCookieName)

	token := controlstore.EnrollmentToken{ID: "enroll-public", Digest: security.DigestEnrollmentToken("enroll-public"),
		NodeName: "Singapore edge", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := store.CreateEnrollmentToken(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	credential := "credential-that-must-never-be-public"
	if _, err := store.ConsumeEnrollment(t.Context(), token.Digest, "private-node-id", "credential-id",
		security.DigestNodeCredential(credential), now); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateNode(t.Context(), "private-node-id", "Singapore edge", "SG", false, now); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateNodeFacts(t.Context(), "private-node-id", controlstore.NodeFacts{
		Hostname: "real-hostname-secret", OS: "PrivateOS", Kernel: "private-kernel", Architecture: "arm64", Virtualization: "203.0.113.99",
	}, now); err != nil {
		t.Fatal(err)
	}
	live.setConnected("private-node-id", "session", now)
	report := protocol.Report{CollectedAtUnixMilli: now.UnixMilli(), CPU: protocol.CPUStats{UsagePercent: 25},
		Memory:  protocol.MemoryStats{TotalBytes: 1000, UsedBytes: 400, AvailableBytes: 600},
		Disk:    protocol.DiskStats{TotalBytes: 2000, UsedBytes: 500, FreeBytes: 1500},
		Network: protocol.NetworkStats{ReceiveBytesPerSecond: 10, TransmitBytesPerSecond: 20}, UptimeSeconds: 3600}
	if !live.acceptReport("private-node-id", "session", report, now) {
		t.Fatal("live report was not accepted")
	}

	enrollHomepageTestNode(t, store, "unlisted-node", now)
	private := performJSON(t, handler, http.MethodGet, "/api/v1/public/status", "", nil, "")
	if private.Code != http.StatusOK || !strings.Contains(private.Body.String(), "Singapore edge") {
		t.Fatalf("default homepage status = %d %s", private.Code, private.Body.String())
	}
	update := performJSON(t, handler, http.MethodPut, "/api/v1/admin/public-page",
		`{"enabled":true,"title":"Fleet status","description":"Public health","node_ids":["private-node-id"]}`,
		[]*http.Cookie{cookie}, auth.CSRF)
	if update.Code != http.StatusOK {
		t.Fatalf("public config update = %d %s", update.Code, update.Body.String())
	}

	public := performJSON(t, handler, http.MethodGet, "/api/v1/public/status", "", nil, "")
	if public.Code != http.StatusOK {
		t.Fatalf("public status = %d %s", public.Code, public.Body.String())
	}
	assertSafeHomepageResponse(t, public)

	// Requiring a login changes access, never the page DTO or selected nodes.
	restricted := performJSON(t, handler, http.MethodPut, "/api/v1/admin/public-page",
		`{"enabled":false,"title":"Fleet status","description":"Public health"}`, []*http.Cookie{cookie}, auth.CSRF)
	if restricted.Code != http.StatusOK {
		t.Fatalf("restrict homepage = %d %s", restricted.Code, restricted.Body.String())
	}
	for _, cookies := range [][]*http.Cookie{nil, {{Name: sessionCookieName, Value: "invalid-session"}}} {
		denied := performJSON(t, handler, http.MethodGet, "/api/v1/public/status", "", cookies, "")
		if denied.Code != http.StatusUnauthorized || strings.Contains(denied.Body.String(), "Singapore edge") || strings.Contains(denied.Body.String(), "Fleet status") {
			t.Fatalf("restricted anonymous status = %d %s", denied.Code, denied.Body.String())
		}
	}
	authenticated := performJSON(t, handler, http.MethodGet, "/api/v1/public/status", "", []*http.Cookie{cookie}, "")
	if authenticated.Code != http.StatusOK || authenticated.Body.String() != public.Body.String() {
		t.Fatalf("authenticated homepage changed projection = %d %s", authenticated.Code, authenticated.Body.String())
	}
	assertSafeHomepageResponse(t, authenticated)
	logout := performJSON(t, handler, http.MethodPost, "/api/v1/admin/logout", "", []*http.Cookie{cookie}, auth.CSRF)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout = %d %s", logout.Code, logout.Body.String())
	}
	if revoked := performJSON(t, handler, http.MethodGet, "/api/v1/public/status", "", []*http.Cookie{cookie}, ""); revoked.Code != http.StatusUnauthorized {
		t.Fatalf("revoked homepage session = %d %s", revoked.Code, revoked.Body.String())
	}
}

func assertSafeHomepageResponse(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	body := response.Body.String()
	for _, required := range []string{
		"Fleet status", "Singapore edge", `"group":"SG"`, `"online":true`, `"cpu_usage_percent":25`,
		`"month_receive_bytes":null`, `"month_transmit_bytes":null`,
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("public response missing %q: %s", required, body)
		}
	}
	for _, forbidden := range []string{
		"unlisted-node", "private-node-id", "real-hostname-secret", "PrivateOS", "private-kernel", "arm64", "203.0.113.99", "credential-that-must-never-be-public",
		`"hostname"`, `"ip"`, `"os"`, `"kernel"`, `"architecture"`, `"virtualization"`,
		`"homepage_visible"`, `"hidden"`, `"credential"`, `"history"`, `"points"`, `"bucket_start"`, `"receive_bytes_total"`,
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("public response leaked %q: %s", forbidden, body)
		}
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 2 || decoded["page"] == nil || decoded["nodes"] == nil {
		t.Fatalf("unexpected public top-level allowlist: %v", decoded)
	}
	var nodes []map[string]json.RawMessage
	if err := json.Unmarshal(decoded["nodes"], &nodes); err != nil || len(nodes) != 1 {
		t.Fatalf("decode public nodes: %#v, %v", nodes, err)
	}
	allowed := map[string]bool{
		"name": true, "group": true, "online": true, "uptime_seconds": true, "cpu_usage_percent": true,
		"memory_used_bytes": true, "memory_total_bytes": true, "disk_used_bytes": true, "disk_total_bytes": true,
		"receive_bytes_per_second": true, "transmit_bytes_per_second": true,
		"month_receive_bytes": true, "month_transmit_bytes": true,
	}
	if len(nodes[0]) != len(allowed) {
		t.Fatalf("public node fields = %v", nodes[0])
	}
	for field := range nodes[0] {
		if !allowed[field] {
			t.Fatalf("public node contained non-allowlisted field %q", field)
		}
	}
}

func TestPublicPageMutationRequiresAuthenticationAndCSRF(t *testing.T) {
	handler, _ := newTestAdminAPI(t, nil)
	unauthenticated := performJSON(t, handler, http.MethodPut, "/api/v1/admin/public-page",
		`{"enabled":false,"title":"Status","description":"","node_ids":[]}`, nil, "")
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated update = %d", unauthenticated.Code)
	}
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"very-long-test-password"}`, nil, "")
	cookie := responseCookie(t, setup, sessionCookieName)
	defaultPage := performJSON(t, handler, http.MethodGet, "/api/v1/admin/public-page", "", []*http.Cookie{cookie}, "")
	if defaultPage.Code != http.StatusOK || !strings.Contains(defaultPage.Body.String(), `"node_ids":[]`) {
		t.Fatalf("default public allowlist must be an array = %d %s", defaultPage.Code, defaultPage.Body.String())
	}
	missingCSRF := performJSON(t, handler, http.MethodPut, "/api/v1/admin/public-page",
		`{"enabled":false,"title":"Status","description":"","node_ids":[]}`, []*http.Cookie{cookie}, "")
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("update without CSRF = %d", missingCSRF.Code)
	}
}

func TestCurrentUTCMonthDoesNotUseControlLocalTimezone(t *testing.T) {
	local := time.Date(2026, time.October, 1, 0, 30, 0, 0, time.FixedZone("UTC+2", 2*60*60))
	if got := currentUTCMonth(local); got != "2026-09" {
		t.Fatalf("current UTC month = %q, want 2026-09", got)
	}
}

func TestProjectPublicReportKeepsUnavailableMetricsNull(t *testing.T) {
	var dto publicNodeDTO
	projectPublicReport(&dto, protocol.Report{Unavailable: []protocol.MetricArea{
		protocol.AreaCPU, protocol.AreaMemory, protocol.AreaDisk, protocol.AreaNetwork, protocol.AreaUptime,
	}})
	if dto.CPUUsagePercent != nil || dto.MemoryUsedBytes != nil || dto.MemoryTotalBytes != nil ||
		dto.DiskUsedBytes != nil || dto.DiskTotalBytes != nil || dto.ReceiveBytesPerSecond != nil ||
		dto.TransmitBytesPerSecond != nil || dto.UptimeSeconds != nil {
		t.Fatalf("unavailable public metrics were projected as measured values: %#v", dto)
	}
}
