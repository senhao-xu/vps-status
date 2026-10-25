package agent

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/senhao-xu/vps-status/internal/protocol"
)

func TestRetryPolicyCapsJitterAndCancellation(t *testing.T) {
	policy := RetryPolicy{Initial: time.Second, Maximum: 5 * time.Second, Multiplier: 2, Jitter: .2, Rand: func() float64 { return 1 }}
	want := []time.Duration{1200 * time.Millisecond, 2400 * time.Millisecond, 4800 * time.Millisecond, 5 * time.Second, 5 * time.Second}
	for attempt, expected := range want {
		if got := policy.Delay(attempt); got != expected {
			t.Fatalf("Delay(%d)=%v want %v", attempt, got, expected)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	connects := 0
	policy.Sleep = func(ctx context.Context, _ time.Duration) error { cancel(); <-ctx.Done(); return ctx.Err() }
	err := RunReconnect(ctx, policy, func(context.Context) error { connects++; return errors.New("offline") })
	if !errors.Is(err, context.Canceled) || connects != 1 {
		t.Fatalf("RunReconnect=%v connects=%d", err, connects)
	}
	if got := (RetryPolicy{Initial: time.Second, Maximum: time.Minute, Rand: func() float64 { return 1 }}).Delay(0); got != 1200*time.Millisecond {
		t.Fatalf("default jitter delay=%v", got)
	}
}

func TestEnrollUsesAuthorizationHeaderAndConfigPermissions(t *testing.T) {
	var authorization, query string
	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		authorization, query = r.Header.Get("Authorization"), r.URL.RawQuery
		return &http.Response{StatusCode: http.StatusCreated, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"node_id":"node-1","credential":"node-secret"}`))}, nil
	})}
	config, err := Enroll(t.Context(), client, "http://127.0.0.1:8080", "enrollment-secret")
	if err != nil {
		t.Fatal(err)
	}
	if authorization != "Bearer enrollment-secret" || query != "" {
		t.Fatalf("authorization=%q query=%q", authorization, query)
	}
	path := filepath.Join(t.TempDir(), "agent.json")
	if err := SaveConfig(path, config); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode=%o", info.Mode().Perm())
	}
	loaded, err := LoadConfig(path)
	if err != nil || loaded != config {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "node-secret") {
		t.Fatal("credential was not persisted")
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("group-readable credential config accepted")
	}
	unknownPath := filepath.Join(t.TempDir(), "unknown.json")
	if err := os.WriteFile(unknownPath, []byte(`{"control_url":"https://control.example","node_id":"node","credential":"secret","unexpected":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(unknownPath); err == nil {
		t.Fatal("Agent config with an unknown field was accepted")
	}
	insecurePath := filepath.Join(t.TempDir(), "insecure.json")
	if err := os.WriteFile(insecurePath, []byte(`{"control_url":"http://control.example","node_id":"node","credential":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(insecurePath); err == nil {
		t.Fatal("Agent config with a remote plaintext Control URL was accepted")
	}
}

func TestEnrollAcceptsDocumentedControlResponse(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusCreated, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"node_id":"node-1","node_name":"Local test node","credential":"node-secret","websocket_path":"/agent/v1/ws"}`))}, nil
	})}
	config, err := Enroll(t.Context(), client, "http://127.0.0.1:8080", "enrollment-secret")
	if err != nil {
		t.Fatalf("enroll with documented Control response: %v", err)
	}
	if config.ControlURL != "http://127.0.0.1:8080" || config.NodeID != "node-1" || config.Credential != "node-secret" {
		t.Fatal("documented Control response did not produce the expected Agent config")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestEnrollBoundsAndStrictlyDecodesResponseWithoutRedirecting(t *testing.T) {
	t.Run("oversize", func(t *testing.T) {
		client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusCreated, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(strings.Repeat("x", maxEnrollmentResponseBytes+1)))}, nil
		})}
		if _, err := Enroll(t.Context(), client, "http://127.0.0.1", "secret"); err == nil || !strings.Contains(err.Error(), "size limit") {
			t.Fatalf("oversized response error=%v", err)
		}
	})
	for _, test := range []struct {
		name string
		body string
	}{
		{"unknown field", `{"node_id":"node","credential":"secret","extra":true}`},
		{"numeric node name", `{"node_id":"node","credential":"secret","node_name":123}`},
		{"object node name", `{"node_id":"node","credential":"secret","node_name":{}}`},
		{"boolean websocket path", `{"node_id":"node","credential":"secret","websocket_path":true}`},
		{"array websocket path", `{"node_id":"node","credential":"secret","websocket_path":[]}`},
		{"trailing JSON", `{"node_id":"node","node_name":"Local test node","credential":"secret","websocket_path":"/agent/v1/ws"} {}`},
		{"trailing garbage", `{"node_id":"node","credential":"secret"} invalid`},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusCreated, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(test.body))}, nil
			})}
			if config, err := Enroll(t.Context(), client, "http://127.0.0.1", "secret"); err == nil || config != (Config{}) {
				t.Fatalf("invalid response error=%v; empty config=%v", err, config == (Config{}))
			}
		})
	}
	t.Run("redirect", func(t *testing.T) {
		calls := 0
		client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			if calls > 1 || request.URL.Path != "/agent/v1/enroll" {
				t.Fatalf("unexpected redirected request: calls=%d url=%s", calls, request.URL)
			}
			header := make(http.Header)
			header.Set("Location", "http://127.0.0.1/credential-capture")
			return &http.Response{StatusCode: http.StatusFound, Header: header, Body: http.NoBody, Request: request}, nil
		})}
		if _, err := Enroll(t.Context(), client, "http://127.0.0.1", "secret"); err == nil || calls != 1 {
			t.Fatalf("redirect error=%v calls=%d", err, calls)
		}
	})
	t.Run("cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		})}
		if _, err := Enroll(ctx, client, "http://127.0.0.1", "secret"); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error=%v", err)
		}
	})
}

func TestWebsocketURLNeverIncludesCredential(t *testing.T) {
	got, err := websocketURL("https://control.example/base")
	if err != nil || got != "wss://control.example/base/agent/v1/ws" {
		t.Fatalf("websocketURL=%q %v", got, err)
	}
	if _, err := websocketURL("http://control.example"); err == nil {
		t.Fatal("remote plaintext websocket URL accepted")
	}
}

func TestClientHandshakeSequenceAuthorizationAndPong(t *testing.T) {
	result := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer node-secret" {
			result <- errors.New("missing Authorization header")
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		connection, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(response, request, nil)
		if err != nil {
			result <- err
			return
		}
		defer connection.Close()
		hello, _ := protocol.NewEnvelope(protocol.TypeHello, "session", 1, protocol.ServerHello{NodeID: "node-1", ServerNonce: "nonce", HeartbeatTimeoutSeconds: 1, HeartbeatIntervalSeconds: 1, ReportIntervalMillis: protocol.DefaultReportInterval.Milliseconds()})
		if err := connection.WriteJSON(hello); err != nil {
			result <- err
			return
		}
		var clientHello protocol.Envelope
		if err := connection.ReadJSON(&clientHello); err != nil || clientHello.Seq != 1 || clientHello.Type != protocol.TypeHello {
			result <- errors.New("invalid client hello")
			return
		}
		ping, _ := protocol.NewEnvelope(protocol.TypePing, "session", 2, protocol.Ping{SentAtUnixMilli: 123})
		if err := connection.WriteJSON(ping); err != nil {
			result <- err
			return
		}
		var pong protocol.Envelope
		if err := connection.ReadJSON(&pong); err != nil || pong.Seq != 2 || pong.Type != protocol.TypePong {
			result <- errors.New("invalid client pong")
			return
		}
		result <- nil
	}))
	defer server.Close()
	client := Client{Config: Config{ControlURL: server.URL, NodeID: "node-1", Credential: "node-secret"}}
	_ = client.ConnectOnce(t.Context())
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestClientRejectsBadServerSequenceAndSilentHeartbeat(t *testing.T) {
	t.Run("sequence", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			connection, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(response, request, nil)
			if err != nil {
				return
			}
			defer connection.Close()
			hello, _ := protocol.NewEnvelope(protocol.TypeHello, "session", 2, protocol.ServerHello{NodeID: "node-1", ServerNonce: "nonce", HeartbeatIntervalSeconds: 1, HeartbeatTimeoutSeconds: 1, ReportIntervalMillis: protocol.DefaultReportInterval.Milliseconds()})
			_ = connection.WriteJSON(hello)
		}))
		defer server.Close()
		err := (&Client{Config: Config{ControlURL: server.URL, NodeID: "node-1", Credential: "secret"}}).ConnectOnce(t.Context())
		if err == nil || !strings.Contains(err.Error(), "sequence") {
			t.Fatalf("bad sequence error=%v", err)
		}
	})
	t.Run("heartbeat", func(t *testing.T) {
		receivedHello := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			connection, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(response, request, nil)
			if err != nil {
				return
			}
			defer connection.Close()
			hello, _ := protocol.NewEnvelope(protocol.TypeHello, "session", 1, protocol.ServerHello{NodeID: "node-1", ServerNonce: "nonce", HeartbeatIntervalSeconds: 1, HeartbeatTimeoutSeconds: 1, ReportIntervalMillis: protocol.DefaultReportInterval.Milliseconds()})
			_ = connection.WriteJSON(hello)
			var reply protocol.Envelope
			_ = connection.ReadJSON(&reply)
			close(receivedHello)
			time.Sleep(1500 * time.Millisecond)
		}))
		defer server.Close()
		started := time.Now()
		err := (&Client{Config: Config{ControlURL: server.URL, NodeID: "node-1", Credential: "secret"}}).ConnectOnce(t.Context())
		<-receivedHello
		if err == nil || time.Since(started) > 1400*time.Millisecond {
			t.Fatalf("silent heartbeat error=%v elapsed=%v", err, time.Since(started))
		}
	})
	t.Run("report interval overflow", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			connection, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(response, request, nil)
			if err != nil {
				return
			}
			defer connection.Close()
			hello, _ := protocol.NewEnvelope(protocol.TypeHello, "session", 1, protocol.ServerHello{
				NodeID: "node-1", ServerNonce: "nonce", HeartbeatIntervalSeconds: 1,
				HeartbeatTimeoutSeconds: 1, ReportIntervalMillis: math.MaxInt64,
			})
			_ = connection.WriteJSON(hello)
		}))
		defer server.Close()
		err := (&Client{Config: Config{ControlURL: server.URL, NodeID: "node-1", Credential: "secret"}}).ConnectOnce(t.Context())
		if err == nil || !strings.Contains(err.Error(), "report interval") {
			t.Fatalf("overflow report interval error=%v", err)
		}
	})
}

func TestClientHandshakeHonorsContextCancellation(t *testing.T) {
	upgraded := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		connection, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(response, request, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		close(upgraded)
		_, _, _ = connection.ReadMessage()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		result <- (&Client{Config: Config{ControlURL: server.URL, NodeID: "node-1", Credential: "secret"}}).ConnectOnce(ctx)
	}()
	<-upgraded
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("handshake cancellation error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Agent handshake did not stop after cancellation")
	}
}

type collectorFunc func(time.Time) (protocol.Report, error)

func (collect collectorFunc) Collect(now time.Time) (protocol.Report, error) { return collect(now) }

func TestClientSendsTypedReportsAtNegotiatedInterval(t *testing.T) {
	received := make(chan protocol.Report, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		connection, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(response, request, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		hello, _ := protocol.NewEnvelope(protocol.TypeHello, "session", 1, protocol.ServerHello{
			NodeID: "node-1", ServerNonce: "nonce", HeartbeatIntervalSeconds: 5,
			HeartbeatTimeoutSeconds: 5, ReportIntervalMillis: protocol.MinReportInterval.Milliseconds(),
		})
		if err := connection.WriteJSON(hello); err != nil {
			return
		}
		var clientHello protocol.Envelope
		if err := connection.ReadJSON(&clientHello); err != nil {
			return
		}
		var reportEnvelope protocol.Envelope
		if err := connection.ReadJSON(&reportEnvelope); err != nil || reportEnvelope.Type != protocol.TypeReport || reportEnvelope.Seq != 2 {
			return
		}
		var report protocol.Report
		if err := protocol.DecodePayload(reportEnvelope.Payload, &report); err != nil {
			return
		}
		received <- report
		terminal, _ := protocol.NewEnvelope(protocol.TypeError, "session", 2, protocol.ErrorPayload{Code: "test_complete", Message: "done"})
		_ = connection.WriteJSON(terminal)
	}))
	defer server.Close()
	now := time.Unix(1_700_000_000, 0).UTC()
	client := &Client{
		Config: Config{ControlURL: server.URL, NodeID: "node-1", Credential: "secret"},
		Now:    func() time.Time { return now },
		Collector: collectorFunc(func(collectedAt time.Time) (protocol.Report, error) {
			return protocol.Report{CollectedAtUnixMilli: collectedAt.UnixMilli(), ProcessCount: 7}, nil
		}),
	}
	if err := client.ConnectOnce(t.Context()); err == nil || !strings.Contains(err.Error(), "test_complete") {
		t.Fatalf("ConnectOnce() terminal error=%v", err)
	}
	select {
	case report := <-received:
		if report.CollectedAtUnixMilli != now.UnixMilli() || report.ProcessCount != 7 {
			t.Fatalf("report=%#v", report)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Agent did not send negotiated report")
	}
}

func TestClientReportScheduleSkipsMissedIntervals(t *testing.T) {
	spacing := make(chan time.Duration, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		connection, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(response, request, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		hello, _ := protocol.NewEnvelope(protocol.TypeHello, "session", 1, protocol.ServerHello{
			NodeID: "node-1", ServerNonce: "nonce", HeartbeatIntervalSeconds: 5,
			HeartbeatTimeoutSeconds: 5, ReportIntervalMillis: protocol.MinReportInterval.Milliseconds(),
		})
		if err := connection.WriteJSON(hello); err != nil {
			return
		}
		var envelope protocol.Envelope
		if err := connection.ReadJSON(&envelope); err != nil {
			return
		}
		arrivals := make([]time.Time, 0, 2)
		for len(arrivals) < 2 {
			if err := connection.ReadJSON(&envelope); err != nil {
				return
			}
			if envelope.Type == protocol.TypeReport {
				arrivals = append(arrivals, time.Now())
			}
		}
		spacing <- arrivals[1].Sub(arrivals[0])
		terminal, _ := protocol.NewEnvelope(protocol.TypeError, "session", 2, protocol.ErrorPayload{Code: "test_complete", Message: "done"})
		_ = connection.WriteJSON(terminal)
	}))
	defer server.Close()
	collections := 0
	client := &Client{
		Config: Config{ControlURL: server.URL, NodeID: "node-1", Credential: "secret"},
		Collector: collectorFunc(func(collectedAt time.Time) (protocol.Report, error) {
			collections++
			if collections == 1 {
				time.Sleep(800 * time.Millisecond)
			}
			return protocol.Report{CollectedAtUnixMilli: collectedAt.UnixMilli()}, nil
		}),
	}
	if err := client.ConnectOnce(t.Context()); err == nil || !strings.Contains(err.Error(), "test_complete") {
		t.Fatalf("ConnectOnce() terminal error=%v", err)
	}
	if got := <-spacing; got < 900*time.Millisecond {
		t.Fatalf("reports caught up after delayed collection: spacing=%v", got)
	}
}
