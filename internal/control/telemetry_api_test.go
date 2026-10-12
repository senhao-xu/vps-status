package control

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/storage/telemetry"
)

func TestTelemetryHostAPISerializesUnavailableMetricsAsNull(t *testing.T) {
	store, err := telemetry.Open(t.Context(), filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := telemetry.NewService(store, telemetry.Config{FileBudgetBytes: 1 << 30, MinimumFreeBytes: 1, GuardInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })

	handler, _ := newTestAdminAPI(t, func(config *APIConfig) { config.Telemetry = service })
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"very-long-test-password"}`, nil, "")
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup = %d %s", setup.Code, setup.Body.String())
	}
	cookie := responseCookie(t, setup, sessionCookieName)
	start := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	if err := service.SubmitHost(telemetry.HostBucket{NodeID: "node", BucketStart: start, SampleCount: 1}); err != nil {
		t.Fatal(err)
	}
	if err := service.Checkpoint(t.Context()); err != nil {
		t.Fatal(err)
	}
	query := url.Values{
		"node_id":    {"node"},
		"resolution": {"1m"},
		"from":       {start.Format(time.RFC3339)},
		"to":         {start.Add(time.Minute).Format(time.RFC3339)},
	}
	response := performJSON(t, handler, http.MethodGet, "/api/v1/admin/telemetry/host?"+query.Encode(), "", []*http.Cookie{cookie}, "")
	if response.Code != http.StatusOK {
		t.Fatalf("host history = %d %s", response.Code, response.Body.String())
	}
	body := decodeResponse[struct {
		Points []map[string]any `json:"points"`
	}](t, response)
	if len(body.Points) != 1 {
		t.Fatalf("points = %#v", body.Points)
	}
	point := body.Points[0]
	for _, field := range []string{
		"cpu_average", "cpu_min", "cpu_max",
		"load_1_average", "load_5_average", "load_15_average",
		"memory_used_average", "memory_total_last", "swap_used_average", "swap_total_last",
		"disk_used_average", "disk_total_last",
		"receive_rate_average", "receive_rate_max", "transmit_rate_average", "transmit_rate_max",
		"uptime_last", "process_average", "tcp_average", "udp_average",
	} {
		value, exists := point[field]
		if !exists || value != nil {
			t.Fatalf("%s = %#v, exists=%v; want explicit null", field, value, exists)
		}
	}
	for _, field := range []string{"cpu_count", "load_count", "memory_count", "disk_count", "network_count", "uptime_count", "process_count", "connection_count"} {
		if value, ok := point[field].(float64); !ok || value != 0 {
			t.Fatalf("%s = %#v; want 0", field, point[field])
		}
	}
}

func TestTelemetryProbeAPIExposesAggregatesAndValidatesResolution(t *testing.T) {
	store, err := telemetry.Open(t.Context(), filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := telemetry.NewService(store, telemetry.Config{FileBudgetBytes: 1 << 30, MinimumFreeBytes: 1, GuardInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	handler, _ := newTestAdminAPI(t, func(config *APIConfig) { config.Telemetry = service })
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"very-long-test-password"}`, nil, "")
	cookie := responseCookie(t, setup, sessionCookieName)
	start := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	if err := service.SubmitProbe(telemetry.ProbeBucket{NodeID: "node", TaskID: "task", BucketStart: start, Attempts: 2, Successes: 1, Failures: 1, LatencyCount: 1, LatencySum: 12, LatencyMin: 12, LatencyMax: 12}); err != nil {
		t.Fatal(err)
	}
	if err := service.Checkpoint(t.Context()); err != nil {
		t.Fatal(err)
	}
	query := url.Values{"node_id": {"node"}, "task_id": {"task"}, "resolution": {"1m"}, "from": {start.Format(time.RFC3339)}, "to": {start.Add(time.Minute).Format(time.RFC3339)}}
	response := performJSON(t, handler, http.MethodGet, "/api/v1/admin/telemetry/probe?"+query.Encode(), "", []*http.Cookie{cookie}, "")
	if response.Code != http.StatusOK {
		t.Fatalf("probe history=%d %s", response.Code, response.Body.String())
	}
	body := decodeResponse[struct {
		Points []telemetry.ProbePoint `json:"points"`
	}](t, response)
	if len(body.Points) != 1 || body.Points[0].SuccessRate != .5 || body.Points[0].LatencyAverage == nil || *body.Points[0].LatencyAverage != 12 {
		t.Fatalf("probe points=%#v", body.Points)
	}
	query.Set("resolution", "raw")
	response = performJSON(t, handler, http.MethodGet, "/api/v1/admin/telemetry/probe?"+query.Encode(), "", []*http.Cookie{cookie}, "")
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_resolution"`) {
		t.Fatalf("invalid resolution=%d %s", response.Code, response.Body.String())
	}
}

func TestTelemetryStorageAPIExposesDegradedStateWithoutBreakingLogin(t *testing.T) {
	handler, _ := newTestAdminAPI(t, func(config *APIConfig) { config.Telemetry = telemetry.NewUnavailable("open_failed") })
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"very-long-test-password"}`, nil, "")
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup = %d %s", setup.Code, setup.Body.String())
	}
	cookie := responseCookie(t, setup, sessionCookieName)
	status := performJSON(t, handler, http.MethodGet, "/api/v1/admin/telemetry/storage", "", []*http.Cookie{cookie}, "")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"available":false`) || !strings.Contains(status.Body.String(), `"reason":"open_failed"`) {
		t.Fatalf("storage state = %d %s", status.Code, status.Body.String())
	}
	login := performJSON(t, handler, http.MethodPost, "/api/v1/admin/login", `{"username":"owner","password":"very-long-test-password"}`, nil, "")
	if login.Code != http.StatusOK {
		t.Fatalf("login while degraded = %d %s", login.Code, login.Body.String())
	}
}

func TestTelemetryDestructiveAPIRequiresCSRFAndExactConfirmation(t *testing.T) {
	store, err := telemetry.Open(t.Context(), filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := telemetry.NewService(store, telemetry.Config{FileBudgetBytes: 1 << 30, MinimumFreeBytes: 1, GuardInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	handler, now := newTestAdminAPI(t, func(config *APIConfig) { config.Telemetry = service; config.History = NewHostHistory(service) })
	setup := performJSON(t, handler, http.MethodPost, "/api/v1/admin/setup", `{"username":"owner","password":"very-long-test-password"}`, nil, "")
	cookie := responseCookie(t, setup, sessionCookieName)
	var body struct {
		CSRF string `json:"csrf_token"`
	}
	body = decodeResponse[struct {
		CSRF string `json:"csrf_token"`
	}](t, setup)
	if err := service.SubmitTraffic(telemetry.TrafficDelta{NodeID: "n", Month: "2026-10", ReceiveBytes: 1, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := service.Checkpoint(t.Context()); err != nil {
		t.Fatal(err)
	}
	missingCSRF := performJSON(t, handler, http.MethodPost, "/api/v1/admin/telemetry/clear-history", `{"confirm":"clear_history"}`, []*http.Cookie{cookie}, "")
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF = %d", missingCSRF.Code)
	}
	wrong := performJSON(t, handler, http.MethodPost, "/api/v1/admin/telemetry/clear-history", `{"confirm":"reset_all_telemetry"}`, []*http.Cookie{cookie}, body.CSRF)
	if wrong.Code != http.StatusBadRequest {
		t.Fatalf("wrong confirmation = %d %s", wrong.Code, wrong.Body.String())
	}
	clear := performJSON(t, handler, http.MethodPost, "/api/v1/admin/telemetry/clear-history", `{"confirm":"clear_history"}`, []*http.Cookie{cookie}, body.CSRF)
	if clear.Code != http.StatusNoContent {
		t.Fatalf("clear = %d %s", clear.Code, clear.Body.String())
	}
	totals, _ := service.Traffic(t.Context(), "n")
	if len(totals) != 2 {
		t.Fatalf("clear removed traffic: %#v", totals)
	}
	reset := performJSON(t, handler, http.MethodPost, "/api/v1/admin/telemetry/reset-all", `{"confirm":"reset_all_telemetry"}`, []*http.Cookie{cookie}, body.CSRF)
	if reset.Code != http.StatusNoContent {
		t.Fatalf("reset = %d %s", reset.Code, reset.Body.String())
	}
	totals, _ = service.Traffic(t.Context(), "n")
	if len(totals) != 0 {
		t.Fatalf("reset retained traffic: %#v", totals)
	}
}
