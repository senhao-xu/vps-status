package control

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/senhao-xu/vps-status/internal/protocol"
	"github.com/senhao-xu/vps-status/internal/security"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

type wsFixture struct {
	server             *httptest.Server
	store              *controlstore.Store
	registry           *Registry
	live               *LiveStore
	probes             *ProbeHistory
	nodeID, credential string
}

func newWSFixture(t *testing.T, sessions AgentSessionConfig) *wsFixture {
	t.Helper()
	directory := t.TempDir()
	store, err := controlstore.Open(t.Context(), filepath.Join(directory, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := security.LoadOrCreateMasterKey(filepath.Join(directory, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	token := security.DigestEnrollmentToken("enrollment")
	if err := store.CreateEnrollmentToken(t.Context(), controlstore.EnrollmentToken{ID: "enrollment", Digest: token, NodeName: "node", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	credential := "node-credential"
	if _, err := store.ConsumeEnrollment(t.Context(), token, "node-1", "credential-1", security.DigestNodeCredential(credential), now); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	live := NewLiveStore()
	probes := NewProbeHistory(nil)
	handler, err := NewHandler(APIConfig{Store: store, MasterKey: key, Web: http.NotFoundHandler(), Registry: registry, Live: live, ProbeHistory: probes, AgentSessions: sessions})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(func() { server.Close(); _ = store.Close() })
	return &wsFixture{server: server, store: store, registry: registry, live: live, probes: probes, nodeID: "node-1", credential: credential}
}

func (fixture *wsFixture) dial(t *testing.T, credential string) (*websocket.Conn, protocol.Envelope, *http.Response) {
	t.Helper()
	header := http.Header{}
	header.Set("Authorization", "Bearer "+credential)
	endpoint := "ws" + strings.TrimPrefix(fixture.server.URL, "http") + "/agent/v1/ws"
	connection, response, err := websocket.DefaultDialer.Dial(endpoint, header)
	if err != nil {
		return nil, protocol.Envelope{}, response
	}
	_, data, err := connection.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	hello, err := protocol.DecodeEnvelope(data)
	if err != nil {
		t.Fatal(err)
	}
	return connection, hello, response
}

func sendClientHello(t *testing.T, connection *websocket.Conn, serverHello protocol.Envelope, seq uint64) {
	sendClientHelloWithFacts(t, connection, serverHello, seq, protocol.ClientHello{AgentVersion: "test"})
}

func sendClientHelloWithFacts(t *testing.T, connection *websocket.Conn, serverHello protocol.Envelope, seq uint64, helloPayload protocol.ClientHello) {
	t.Helper()
	var payload protocol.ServerHello
	if err := protocol.DecodePayload(serverHello.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	helloPayload.ServerNonce = payload.ServerNonce
	hello, _ := protocol.NewEnvelope(protocol.TypeHello, serverHello.SessionID, seq, helloPayload)
	if err := connection.WriteJSON(hello); err != nil {
		t.Fatal(err)
	}
}

func readProbeSync(t *testing.T, connection *websocket.Conn) (protocol.Envelope, protocol.ProbeSync) {
	t.Helper()
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	_, data, err := connection.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.SetReadDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	envelope, err := protocol.DecodeEnvelope(data)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Type != protocol.TypeProbeSync {
		t.Fatalf("initial post-hello frame=%s; want probe.sync", envelope.Type)
	}
	sync, err := protocol.DecodeProbeSync(envelope.Payload)
	if err != nil {
		t.Fatal(err)
	}
	return envelope, sync
}

func eventually(t *testing.T, condition func() bool) {
	eventuallyWithin(t, time.Second, condition)
}

func eventuallyWithin(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition did not become true")
}

func TestAgentWSAuthenticationSequenceAndRegistryCleanup(t *testing.T) {
	fixture := newWSFixture(t, AgentSessionConfig{HeartbeatInterval: time.Second, HeartbeatTimeout: 3 * time.Second})
	if connection, _, response := fixture.dial(t, "wrong"); connection != nil || response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("invalid credential response=%v", response)
	}
	connection, hello, _ := fixture.dial(t, fixture.credential)
	defer connection.Close()
	if hello.Seq != 1 || hello.Type != protocol.TypeHello {
		t.Fatalf("server hello=%#v", hello)
	}
	var helloPayload protocol.ServerHello
	if err := protocol.DecodePayload(hello.Payload, &helloPayload); err != nil || helloPayload.ReportIntervalMillis != protocol.DefaultReportInterval.Milliseconds() {
		t.Fatalf("server report interval=%d, %v", helloPayload.ReportIntervalMillis, err)
	}
	sendClientHello(t, connection, hello, 1)
	readProbeSync(t, connection)
	eventually(t, func() bool { return fixture.registry.Connected(fixture.nodeID) })
	duplicate, _ := protocol.NewEnvelope(protocol.TypePing, hello.SessionID, 1, protocol.Ping{})
	if err := connection.WriteJSON(duplicate); err != nil {
		t.Fatal(err)
	}
	_, data, err := connection.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	errorEnvelope, err := protocol.DecodeEnvelope(data)
	if err != nil {
		t.Fatal(err)
	}
	var payload protocol.ErrorPayload
	_ = protocol.DecodePayload(errorEnvelope.Payload, &payload)
	if errorEnvelope.Type != protocol.TypeError || payload.Code != "invalid_sequence" {
		t.Fatalf("error=%#v payload=%#v", errorEnvelope, payload)
	}
	eventually(t, func() bool { return !fixture.registry.Connected(fixture.nodeID) })

	outOfOrder, secondHello, _ := fixture.dial(t, fixture.credential)
	defer outOfOrder.Close()
	sendClientHello(t, outOfOrder, secondHello, 2)
	_, data, err = outOfOrder.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	errorEnvelope, err = protocol.DecodeEnvelope(data)
	if err != nil {
		t.Fatal(err)
	}
	_ = protocol.DecodePayload(errorEnvelope.Payload, &payload)
	if payload.Code != "invalid_sequence" {
		t.Fatalf("out-of-order code=%q", payload.Code)
	}
}

func TestAgentWSRejectsVersionOversizeAndHeartbeatTimeout(t *testing.T) {
	t.Run("hello timeout", func(t *testing.T) {
		fixture := newWSFixture(t, AgentSessionConfig{HelloTimeout: 20 * time.Millisecond, HeartbeatInterval: time.Second, HeartbeatTimeout: 3 * time.Second})
		connection, _, _ := fixture.dial(t, fixture.credential)
		defer connection.Close()
		_ = connection.SetReadDeadline(time.Now().Add(time.Second))
		_, data, err := connection.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		envelope, err := protocol.DecodeEnvelope(data)
		if err != nil {
			t.Fatal(err)
		}
		var payload protocol.ErrorPayload
		if err := protocol.DecodePayload(envelope.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if envelope.Type != protocol.TypeError || payload.Code != "hello_required" {
			t.Fatalf("hello timeout frame=%#v payload=%#v", envelope, payload)
		}
		eventually(t, func() bool { return !fixture.registry.Connected(fixture.nodeID) })
	})
	t.Run("version", func(t *testing.T) {
		fixture := newWSFixture(t, AgentSessionConfig{HeartbeatInterval: time.Second, HeartbeatTimeout: 3 * time.Second})
		connection, hello, _ := fixture.dial(t, fixture.credential)
		defer connection.Close()
		bad := map[string]any{"version": "v2", "type": "hello", "session_id": hello.SessionID, "seq": 1, "payload": map[string]any{}}
		if err := connection.WriteJSON(bad); err != nil {
			t.Fatal(err)
		}
		_, data, err := connection.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		envelope, err := protocol.DecodeEnvelope(data)
		if err != nil {
			t.Fatal(err)
		}
		var payload protocol.ErrorPayload
		_ = json.Unmarshal(envelope.Payload, &payload)
		if payload.Code != "incompatible_version" {
			t.Fatalf("code=%q", payload.Code)
		}
	})
	t.Run("error payload", func(t *testing.T) {
		fixture := newWSFixture(t, AgentSessionConfig{})
		connection, hello, _ := fixture.dial(t, fixture.credential)
		defer connection.Close()
		sendClientHello(t, connection, hello, 1)
		readProbeSync(t, connection)
		invalid := map[string]any{
			"version": protocol.Version, "type": protocol.TypeError, "session_id": hello.SessionID, "seq": 2,
			"payload": map[string]any{"code": "peer_error", "message": "bad", "unknown": true},
		}
		if err := connection.WriteJSON(invalid); err != nil {
			t.Fatal(err)
		}
		_, data, err := connection.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		envelope, err := protocol.DecodeEnvelope(data)
		if err != nil {
			t.Fatal(err)
		}
		var payload protocol.ErrorPayload
		if err := protocol.DecodePayload(envelope.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if envelope.Type != protocol.TypeError || payload.Code != "invalid_envelope" {
			t.Fatalf("invalid error-payload response=%#v payload=%#v", envelope, payload)
		}
	})
	t.Run("oversize", func(t *testing.T) {
		fixture := newWSFixture(t, AgentSessionConfig{})
		connection, _, _ := fixture.dial(t, fixture.credential)
		defer connection.Close()
		if err := connection.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("x", int(protocol.MaxFrameBytes)+1))); err != nil {
			t.Fatal(err)
		}
		_ = connection.SetReadDeadline(time.Now().Add(time.Second))
		if _, _, err := connection.ReadMessage(); err == nil {
			t.Fatal("oversized frame did not close connection")
		}
		eventually(t, func() bool { return !fixture.registry.Connected(fixture.nodeID) })
	})
	t.Run("heartbeat", func(t *testing.T) {
		fixture := newWSFixture(t, AgentSessionConfig{HeartbeatInterval: 20 * time.Millisecond, HeartbeatTimeout: 45 * time.Millisecond})
		connection, hello, _ := fixture.dial(t, fixture.credential)
		defer connection.Close()
		sendClientHello(t, connection, hello, 1)
		// Read pings but deliberately send no pong.
		_ = connection.SetReadDeadline(time.Now().Add(time.Second))
		for {
			if _, _, err := connection.ReadMessage(); err != nil {
				break
			}
		}
		eventually(t, func() bool { return !fixture.registry.Connected(fixture.nodeID) })
	})
}

func TestCredentialRotationDisconnectsAndInvalidatesOldCredential(t *testing.T) {
	fixture := newWSFixture(t, AgentSessionConfig{})
	connection, hello, _ := fixture.dial(t, fixture.credential)
	defer connection.Close()
	sendClientHello(t, connection, hello, 1)
	readProbeSync(t, connection)
	eventually(t, func() bool { return fixture.registry.Connected(fixture.nodeID) })
	newCredential := "rotated-credential"
	if err := fixture.store.RotateNodeCredential(t.Context(), fixture.nodeID, "credential-2", security.DigestNodeCredential(newCredential), time.Now()); err != nil {
		t.Fatal(err)
	}
	if !fixture.registry.DisconnectNode(fixture.nodeID, "credential_rotated") {
		t.Fatal("active connection not found")
	}
	_ = connection.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := connection.ReadMessage(); err == nil {
		t.Fatal("rotated connection stayed open")
	}
	if old, _, response := fixture.dial(t, fixture.credential); old != nil || response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old credential response=%v", response)
	}
	newConnection, _, _ := fixture.dial(t, newCredential)
	if newConnection == nil {
		t.Fatal("new credential rejected")
	}
	newConnection.Close()
}

func TestAgentWSAcceptsLiveReportsPersistsFactsAndMarksStale(t *testing.T) {
	fixture := newWSFixture(t, AgentSessionConfig{
		HeartbeatInterval: time.Second, HeartbeatTimeout: 10 * time.Second,
		ReportInterval: protocol.MinReportInterval,
	})
	connection, hello, _ := fixture.dial(t, fixture.credential)
	defer connection.Close()
	sendClientHelloWithFacts(t, connection, hello, 1, protocol.ClientHello{
		AgentVersion: "test", Hostname: "real-host", OS: "Linux", Kernel: "6.8.0",
		Architecture: "amd64", Virtualization: "kvm",
	})
	var node controlstore.Node
	var err error
	eventually(t, func() bool {
		node, err = fixture.store.NodeByID(t.Context(), fixture.nodeID)
		return err == nil && node.Facts.Hostname == "real-host"
	})
	if err != nil || node.Facts.Hostname != "real-host" || node.Facts.Virtualization != "kvm" {
		t.Fatalf("node facts=%#v, %v", node.Facts, err)
	}
	report := protocol.Report{CollectedAtUnixMilli: time.Now().UnixMilli(), CPU: protocol.CPUStats{UsagePercent: 12.5}, ProcessCount: 3}
	envelope, err := protocol.NewEnvelope(protocol.TypeReport, hello.SessionID, 2, report)
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.WriteJSON(envelope); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		snapshot := fixture.live.Snapshot(fixture.nodeID)
		return snapshot.Online && snapshot.Report != nil && snapshot.Report.CPU.UsagePercent == 12.5
	})
	eventuallyWithin(t, 6*time.Second, func() bool {
		snapshot := fixture.live.Snapshot(fixture.nodeID)
		return snapshot.Connected && !snapshot.Online && snapshot.Report != nil
	})
}

func TestNewHandlerRejectsReportIntervalOutsideProtocolBounds(t *testing.T) {
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
	for _, interval := range []time.Duration{-time.Second, protocol.MinReportInterval - time.Millisecond, protocol.MaxReportInterval + time.Millisecond} {
		if _, err := NewHandler(APIConfig{
			Store: store, MasterKey: key, Web: http.NotFoundHandler(),
			AgentSessions: AgentSessionConfig{ReportInterval: interval},
		}); err == nil {
			t.Fatalf("NewHandler accepted report interval %v", interval)
		}
	}
}

func TestAgentWSRejectsInvalidAndOverFrequencyReports(t *testing.T) {
	fixture := newWSFixture(t, AgentSessionConfig{ReportInterval: protocol.DefaultReportInterval})
	connection, hello, _ := fixture.dial(t, fixture.credential)
	defer connection.Close()
	sendClientHello(t, connection, hello, 1)
	readProbeSync(t, connection)
	report := protocol.Report{CollectedAtUnixMilli: time.Now().UnixMilli()}
	first, _ := protocol.NewEnvelope(protocol.TypeReport, hello.SessionID, 2, report)
	if err := connection.WriteJSON(first); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return fixture.live.Snapshot(fixture.nodeID).Online })
	report.CollectedAtUnixMilli++
	second, _ := protocol.NewEnvelope(protocol.TypeReport, hello.SessionID, 3, report)
	if err := connection.WriteJSON(second); err != nil {
		t.Fatal(err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(time.Second))
	_, data, err := connection.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	response, err := protocol.DecodeEnvelope(data)
	if err != nil {
		t.Fatal(err)
	}
	var payload protocol.ErrorPayload
	if err := protocol.DecodePayload(response.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if response.Type != protocol.TypeError || payload.Code != "report_rate" {
		t.Fatalf("report-rate response=%#v payload=%#v", response, payload)
	}
}

func TestAgentWSProbeSyncResultAndReplayAdmission(t *testing.T) {
	fixture := newWSFixture(t, AgentSessionConfig{HeartbeatInterval: time.Second, HeartbeatTimeout: 3 * time.Second})
	now := time.Now().UTC()
	task := controlstore.ProbeTask{ID: "task", Name: "TCP", Host: "127.0.0.1", Port: 443, IntervalSeconds: 5}
	if _, err := fixture.store.CreateProbeTask(t.Context(), task, []string{fixture.nodeID}, now); err != nil {
		t.Fatal(err)
	}
	connection, hello, _ := fixture.dial(t, fixture.credential)
	defer connection.Close()
	sendClientHello(t, connection, hello, 1)
	syncEnvelope, sync := readProbeSync(t, connection)
	if len(sync.Tasks) != 1 || sync.Tasks[0].ID != task.ID {
		t.Fatalf("probe snapshot=%#v", sync)
	}
	status, err := protocol.NewEnvelope(protocol.TypeProbeSync, hello.SessionID, 2, protocol.ProbeSyncStatus{Version: sync.Version, Accepted: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.WriteJSON(status); err != nil {
		t.Fatal(err)
	}
	result := protocol.ProbeResult{TaskID: task.ID, ScheduledAtUnixMilli: now.UnixMilli(), Success: true, LatencyMillis: 2}
	resultEnvelope, err := protocol.NewEnvelope(protocol.TypeProbeResult, hello.SessionID, 3, result)
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.WriteJSON(resultEnvelope); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return len(fixture.probes.Latest(fixture.nodeID, task.ID)) == 1 })
	replay, err := protocol.NewEnvelope(protocol.TypeProbeResult, hello.SessionID, 4, result)
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.WriteJSON(replay); err != nil {
		t.Fatal(err)
	}
	_, data, err := connection.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	errorEnvelope, err := protocol.DecodeEnvelope(data)
	if err != nil {
		t.Fatal(err)
	}
	var payload protocol.ErrorPayload
	if err := protocol.DecodePayload(errorEnvelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if syncEnvelope.Seq != 2 || errorEnvelope.Type != protocol.TypeError || payload.Code != "invalid_probe_result" {
		t.Fatalf("sync seq=%d error=%#v payload=%#v", syncEnvelope.Seq, errorEnvelope, payload)
	}
}

func TestSessionQueuesAndRegistryAreBounded(t *testing.T) {
	if validClientHello(protocol.ClientHello{AgentVersion: "test", Hostname: "bad\nhost"}) {
		t.Fatal("Client hello accepted a control character")
	}
	for _, bootID := range []string{"550E8400-E29B-41D4-A716-446655440000", "not-a-uuid", strings.Repeat("a", 129), "主机"} {
		if validClientHello(protocol.ClientHello{AgentVersion: "test", BootID: bootID}) {
			t.Fatalf("Client hello accepted invalid boot ID %q", bootID)
		}
	}
	session := newAgentSession(t.Context(), nil, nil, NewLiveStore(), "node", "credential", [32]byte{}, "session", "nonce", AgentSessionConfig{ReadQueue: 1, WriteQueue: 1}, time.Now)
	if _, err := session.enqueue(protocol.TypePing, protocol.Ping{}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := session.enqueue(protocol.TypePing, protocol.Ping{}, false); err != errQueueFull {
		t.Fatalf("full queue error=%v", err)
	}
	session.cancel()
	registry := NewRegistry()
	closed := make(chan string, 1)
	registry.register("node", "old", func(reason string) { closed <- reason })
	registry.register("node", "new", func(string) {})
	if reason := <-closed; reason != "replaced" {
		t.Fatalf("reason=%q", reason)
	}
	registry.remove("node", "old")
	if !registry.Connected("node") {
		t.Fatal("stale cleanup removed replacement")
	}
}

type oneConnListener struct {
	connections chan net.Conn
	done        chan struct{}
	closeOnce   sync.Once
}

func newOneConnListener(connection net.Conn) *oneConnListener {
	connections := make(chan net.Conn, 1)
	connections <- connection
	return &oneConnListener{connections: connections, done: make(chan struct{})}
}

func (listener *oneConnListener) Accept() (net.Conn, error) {
	select {
	case connection := <-listener.connections:
		return connection, nil
	case <-listener.done:
		return nil, net.ErrClosed
	}
}

func (listener *oneConnListener) Close() error {
	listener.closeOnce.Do(func() { close(listener.done) })
	return nil
}

func (*oneConnListener) Addr() net.Addr { return pipeAddress("agent-ws-test") }

type pipeAddress string

func (address pipeAddress) Network() string { return "pipe" }
func (address pipeAddress) String() string  { return string(address) }

func TestAgentWSSlowSocketWriteHonorsDeadline(t *testing.T) {
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
	now := time.Now().UTC()
	token := security.DigestEnrollmentToken("slow-writer-enrollment")
	if err := store.CreateEnrollmentToken(t.Context(), controlstore.EnrollmentToken{
		ID: "slow-writer-enrollment", Digest: token, NodeName: "node", CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	const nodeID = "slow-writer-node"
	const credential = "slow-writer-credential"
	if _, err := store.ConsumeEnrollment(t.Context(), token, nodeID, "slow-writer-credential-id", security.DigestNodeCredential(credential), now); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	handler, err := NewHandler(APIConfig{
		Store: store, MasterKey: key, Web: http.NotFoundHandler(), Registry: registry,
		AgentSessions: AgentSessionConfig{
			HeartbeatInterval: 100 * time.Millisecond,
			HeartbeatTimeout:  5 * time.Second,
			WriteTimeout:      25 * time.Millisecond,
			WriteQueue:        1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	serverConnection, clientConnection := net.Pipe()
	listener := newOneConnListener(serverConnection)
	httpServer := &http.Server{Handler: handler}
	serveDone := make(chan error, 1)
	go func() { serveDone <- httpServer.Serve(listener) }()
	t.Cleanup(func() {
		_ = clientConnection.Close()
		_ = listener.Close()
		_ = httpServer.Close()
		select {
		case serveErr := <-serveDone:
			if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) && !errors.Is(serveErr, net.ErrClosed) {
				t.Errorf("serve pipe HTTP: %v", serveErr)
			}
		case <-time.After(time.Second):
			t.Error("pipe HTTP server did not stop")
		}
	})

	dialer := websocket.Dialer{NetDialContext: func(context.Context, string, string) (net.Conn, error) {
		return clientConnection, nil
	}}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+credential)
	connection, response, err := dialer.DialContext(t.Context(), "ws://control.test/agent/v1/ws", header)
	if err != nil {
		t.Fatalf("dial pipe websocket: %v (response=%v)", err, response)
	}
	t.Cleanup(func() { _ = connection.Close() })
	_, helloData, err := connection.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	hello, err := protocol.DecodeEnvelope(helloData)
	if err != nil {
		t.Fatal(err)
	}
	sendClientHello(t, connection, hello, 1)
	eventually(t, func() bool { return registry.Connected(nodeID) })

	// net.Pipe has no write buffer. Once this client stops reading, the next
	// heartbeat write can only finish by honoring the configured deadline.
	started := time.Now()
	eventually(t, func() bool { return !registry.Connected(nodeID) })
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("slow socket disconnected after %v; write timeout was not effective", elapsed)
	}
}
