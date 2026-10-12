package control

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
	"github.com/senhao-xu/vps-status/internal/protocol"
	"github.com/senhao-xu/vps-status/internal/security"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

var errQueueFull = errors.New("agent session queue is full")

// AgentSessionConfig bounds the WSS connection lifecycle.
type AgentSessionConfig struct {
	HeartbeatInterval time.Duration
	HeartbeatTimeout  time.Duration
	HelloTimeout      time.Duration
	WriteTimeout      time.Duration
	ReadQueue         int
	WriteQueue        int
	ReportInterval    time.Duration
}

func (c AgentSessionConfig) withDefaults() AgentSessionConfig {
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = 20 * time.Second
	}
	if c.HeartbeatTimeout <= 0 {
		c.HeartbeatTimeout = 60 * time.Second
	}
	if c.HelloTimeout <= 0 {
		c.HelloTimeout = 15 * time.Second
	}
	if c.WriteTimeout <= 0 {
		c.WriteTimeout = 10 * time.Second
	}
	if c.ReadQueue <= 0 {
		c.ReadQueue = 16
	}
	if c.WriteQueue <= 0 {
		c.WriteQueue = 16
	}
	if c.ReportInterval == 0 {
		c.ReportInterval = protocol.DefaultReportInterval
	}
	return c
}

type agentServer struct {
	store    *controlstore.Store
	registry *Registry
	live     *LiveStore
	history  *HostHistory
	probes   *ProbeHistory
	alerts   *AlertManager
	config   AgentSessionConfig
	limiter  *security.Limiter
	now      func() time.Time
	upgrader websocket.Upgrader
}

func newAgentServer(store *controlstore.Store, registry *Registry, live *LiveStore, config AgentSessionConfig, limiter *security.Limiter, now func() time.Time) *agentServer {
	return &agentServer{store: store, registry: registry, live: live, config: config.withDefaults(), limiter: limiter, now: now,
		upgrader: websocket.Upgrader{HandshakeTimeout: 10 * time.Second, CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" }}}
}

func bearerCredential(request *http.Request) (string, bool) {
	value := request.Header.Get("Authorization")
	prefix := "Bearer "
	if !strings.HasPrefix(value, prefix) || len(value) <= len(prefix) || strings.ContainsAny(value[len(prefix):], " \t\r\n") {
		return "", false
	}
	return value[len(prefix):], true
}

func (server *agentServer) serveWS(response http.ResponseWriter, request *http.Request) {
	if allowed, retry := server.limiter.Allow(clientIP(request.RemoteAddr)); !allowed {
		response.Header().Set("Retry-After", strconv.Itoa(max(1, int(retry.Seconds()))))
		writeError(response, http.StatusTooManyRequests, "rate_limited", "Too many Agent connection attempts.")
		return
	}
	credential, ok := bearerCredential(request)
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized", "A node credential is required.")
		return
	}
	digest := security.DigestNodeCredential(credential)
	node, credentialRecord, err := server.store.AuthenticateNodeCredential(request.Context(), digest)
	if err != nil {
		if errors.Is(err, controlstore.ErrCredentialRevoked) {
			writeError(response, http.StatusUnauthorized, "unauthorized", "The node credential is invalid.")
		} else {
			writeInternal(response)
		}
		return
	}
	connection, err := server.upgrader.Upgrade(response, request, nil)
	if err != nil {
		return
	}
	sessionID, err := security.RandomToken()
	if err != nil {
		_ = connection.Close()
		return
	}
	nonce, err := security.RandomToken()
	if err != nil {
		_ = connection.Close()
		return
	}
	session := newAgentSession(request.Context(), connection, server.store, server.live, node.ID, credentialRecord.ID, digest, sessionID, nonce, server.config, server.now)
	session.history = server.history
	session.probes = server.probes
	session.alerts = server.alerts
	server.registry.register(node.ID, sessionID, session.close, session.requestProbeSync)
	defer server.registry.remove(node.ID, sessionID)
	// Close the auth/register race: a rotation before registration is observed
	// here; a rotation after registration disconnects through Registry.
	if _, active, authErr := server.store.AuthenticateNodeCredential(request.Context(), digest); authErr != nil || active.ID != credentialRecord.ID {
		session.close("credential_revoked")
		return
	}
	session.run()
}

type incomingFrame struct {
	data []byte
	err  error
}
type outgoingFrame struct {
	envelope   protocol.Envelope
	closeAfter bool
	ack        chan struct{}
}

type agentSession struct {
	ctx                  context.Context
	cancel               context.CancelFunc
	conn                 *websocket.Conn
	store                *controlstore.Store
	live                 *LiveStore
	history              *HostHistory
	probes               *ProbeHistory
	alerts               *AlertManager
	nodeID, credentialID string
	credentialDigest     [32]byte
	sessionID, nonce     string
	config               AgentSessionConfig
	now                  func() time.Time
	incoming             chan incomingFrame
	outgoing             chan outgoingFrame
	closeOnce            sync.Once
	seqMu                sync.Mutex
	serverSeq            uint64
	probeMu              sync.Mutex
	probeSyncMu          sync.Mutex
	probeHello           bool
	probeVersion         uint64
	probeTasks           map[string]protocol.ProbeTask
	probeLastScheduled   map[string]int64
	probeLastReceived    map[string]time.Time
	probeWindowStart     time.Time
	probeWindowCount     int
}

func newAgentSession(parent context.Context, conn *websocket.Conn, store *controlstore.Store, live *LiveStore, nodeID, credentialID string, digest [32]byte, sessionID, nonce string, config AgentSessionConfig, now func() time.Time) *agentSession {
	ctx, cancel := context.WithCancel(parent)
	config = config.withDefaults()
	return &agentSession{ctx: ctx, cancel: cancel, conn: conn, store: store, live: live, nodeID: nodeID, credentialID: credentialID,
		credentialDigest: digest, sessionID: sessionID, nonce: nonce, config: config, now: now,
		incoming: make(chan incomingFrame, config.ReadQueue), outgoing: make(chan outgoingFrame, config.WriteQueue),
		probeTasks: make(map[string]protocol.ProbeTask), probeLastScheduled: make(map[string]int64), probeLastReceived: make(map[string]time.Time)}
}

func (s *agentSession) requestProbeSync() {
	s.probeSyncMu.Lock()
	defer s.probeSyncMu.Unlock()
	s.probeMu.Lock()
	ready := s.probeHello
	s.probeMu.Unlock()
	if !ready || s.store == nil {
		return
	}
	snapshot, err := s.store.ProbeSnapshot(s.ctx, s.nodeID)
	if err != nil {
		s.close("probe_sync_failed")
		return
	}
	tasks := make([]protocol.ProbeTask, 0, len(snapshot.Tasks))
	for _, task := range snapshot.Tasks {
		tasks = append(tasks, protocol.ProbeTask{ID: task.ID, Host: task.Host, Port: task.Port, IntervalSeconds: task.IntervalSeconds})
	}
	payload := protocol.ProbeSync{Version: snapshot.Version, Tasks: tasks}
	// Publish the admission snapshot before making the frame visible to the
	// writer. A fast Agent can acknowledge immediately after the socket write;
	// updating this state after enqueue would race that acknowledgement.
	s.probeMu.Lock()
	s.probeVersion = payload.Version
	next := make(map[string]protocol.ProbeTask, len(tasks))
	for _, task := range tasks {
		next[task.ID] = task
	}
	for taskID := range s.probeLastScheduled {
		if _, exists := next[taskID]; !exists {
			delete(s.probeLastScheduled, taskID)
			delete(s.probeLastReceived, taskID)
		}
	}
	s.probeTasks = next
	s.probeMu.Unlock()
	if _, err := s.enqueue(protocol.TypeProbeSync, payload, false); err != nil {
		s.close("probe_sync_failed")
	}
}

func (s *agentSession) close(reason string) {
	s.closeOnce.Do(func() {
		s.cancel()
		message := reason
		if len(message) > protocol.MaxErrorMessageBytes {
			message = message[:protocol.MaxErrorMessageBytes]
		}
		_ = s.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, message), time.Now().Add(time.Second))
		_ = s.conn.Close()
	})
}

func (s *agentSession) nextEnvelope(messageType protocol.MessageType, payload any) (protocol.Envelope, error) {
	s.seqMu.Lock()
	defer s.seqMu.Unlock()
	s.serverSeq++
	return protocol.NewEnvelope(messageType, s.sessionID, s.serverSeq, payload)
}

func (s *agentSession) enqueue(messageType protocol.MessageType, payload any, closeAfter bool) (<-chan struct{}, error) {
	envelope, err := s.nextEnvelope(messageType, payload)
	if err != nil {
		return nil, err
	}
	ack := make(chan struct{})
	select {
	case s.outgoing <- outgoingFrame{envelope: envelope, closeAfter: closeAfter, ack: ack}:
		return ack, nil
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	default:
		return nil, errQueueFull
	}
}

func (s *agentSession) fail(err error) {
	ack, queueErr := s.enqueue(protocol.TypeError, protocol.BoundedErrorPayload(err), true)
	if queueErr != nil {
		s.close("protocol_error")
		return
	}
	select {
	case <-ack:
	case <-time.After(s.config.WriteTimeout):
	}
	s.close("protocol_error")
}

func (s *agentSession) run() {
	s.conn.SetReadLimit(protocol.MaxFrameBytes)
	go s.readLoop()
	go s.writeLoop()
	defer s.close("disconnected")

	_, err := s.enqueue(protocol.TypeHello, protocol.ServerHello{NodeID: s.nodeID, ServerNonce: s.nonce,
		HeartbeatIntervalSeconds: max(1, int64(s.config.HeartbeatInterval/time.Second)),
		HeartbeatTimeoutSeconds:  max(1, int64(s.config.HeartbeatTimeout/time.Second)),
		ReportIntervalMillis:     s.config.ReportInterval.Milliseconds()}, false)
	if err != nil {
		return
	}
	heartbeat := time.NewTicker(s.config.HeartbeatInterval)
	defer heartbeat.Stop()
	helloTimer := time.NewTimer(s.config.HelloTimeout)
	defer helloTimer.Stop()
	reportTimer := time.NewTimer(protocol.OfflineAfter(s.config.ReportInterval))
	if !reportTimer.Stop() {
		<-reportTimer.C
	}
	defer reportTimer.Stop()
	var reportDeadline <-chan time.Time
	lastPong := s.now()
	var lastReportAt time.Time
	var lastCollectedAt int64
	var clientSeq uint64
	helloReceived := false
	bootID := ""
	defer func() {
		if s.live != nil {
			now := s.now()
			s.live.setDisconnected(s.nodeID, s.sessionID, now)
			if s.alerts != nil {
				_ = s.alerts.Observe(s.nodeID, s.live.Snapshot(s.nodeID), now)
			}
		}
	}()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-helloTimer.C:
			if !helloReceived {
				s.fail(&protocol.ProtocolError{Code: "hello_required", Message: "The Agent hello was not received in time."})
				return
			}
		case <-reportDeadline:
			if s.live != nil {
				now := s.now()
				s.live.markStale(s.nodeID, s.sessionID, now)
				if s.alerts != nil {
					_ = s.alerts.Observe(s.nodeID, s.live.Snapshot(s.nodeID), now)
				}
			}
			reportDeadline = nil
		case <-heartbeat.C:
			if s.now().Sub(lastPong) > s.config.HeartbeatTimeout {
				s.fail(&protocol.ProtocolError{Code: "heartbeat_timeout", Message: "The Agent heartbeat timed out."})
				return
			}
			if _, err := s.enqueue(protocol.TypePing, protocol.Ping{SentAtUnixMilli: s.now().UnixMilli()}, false); err != nil {
				s.close("slow_client")
				return
			}
		case input := <-s.incoming:
			if input.err != nil {
				return
			}
			envelope, err := protocol.DecodeEnvelope(input.data)
			if err != nil {
				s.fail(err)
				return
			}
			if envelope.SessionID != s.sessionID {
				s.fail(&protocol.ProtocolError{Code: "session_mismatch", Message: "The protocol session does not match this connection."})
				return
			}
			if envelope.Seq != clientSeq+1 {
				s.fail(&protocol.ProtocolError{Code: "invalid_sequence", Message: "The client sequence must increase by exactly one."})
				return
			}
			clientSeq = envelope.Seq
			if !helloReceived && envelope.Type != protocol.TypeHello {
				s.fail(&protocol.ProtocolError{Code: "hello_required", Message: "The first Agent frame must be hello."})
				return
			}
			switch envelope.Type {
			case protocol.TypeHello:
				if helloReceived {
					s.fail(&protocol.ProtocolError{Code: "invalid_sequence", Message: "The Agent hello may only be sent once."})
					return
				}
				var hello protocol.ClientHello
				if protocol.DecodePayload(envelope.Payload, &hello) != nil || hello.ServerNonce != s.nonce || !validClientHello(hello) {
					s.fail(&protocol.ProtocolError{Code: "invalid_hello", Message: "The Agent hello is invalid."})
					return
				}
				if s.store != nil {
					err := s.store.UpdateNodeFacts(s.ctx, s.nodeID, controlstore.NodeFacts{Hostname: hello.Hostname, OS: hello.OS,
						Kernel: hello.Kernel, Architecture: hello.Architecture, Virtualization: hello.Virtualization}, s.now())
					if err != nil {
						s.fail(&protocol.ProtocolError{Code: "internal_error", Message: "The connection could not continue."})
						return
					}
				}
				helloReceived = true
				s.probeMu.Lock()
				s.probeHello = true
				s.probeMu.Unlock()
				bootID = hello.BootID
				lastPong = s.now()
				if s.live != nil {
					s.live.setConnected(s.nodeID, s.sessionID, s.now())
				}
				resetTimer(reportTimer, protocol.OfflineAfter(s.config.ReportInterval))
				reportDeadline = reportTimer.C
				s.requestProbeSync()
			case protocol.TypeReport:
				now := s.now()
				report, decodeErr := protocol.DecodeReport(envelope.Payload, now)
				if decodeErr != nil || report.CollectedAtUnixMilli <= lastCollectedAt {
					s.fail(&protocol.ProtocolError{Code: "invalid_report", Message: "The host report is invalid."})
					return
				}
				minimumSpacing := s.config.ReportInterval / 2
				if minimumSpacing < protocol.MinReportInterval/2 {
					minimumSpacing = protocol.MinReportInterval / 2
				}
				if !lastReportAt.IsZero() && now.Sub(lastReportAt) < minimumSpacing {
					s.fail(&protocol.ProtocolError{Code: "report_rate", Message: "Host reports are arriving too frequently."})
					return
				}
				if s.live == nil || !s.live.acceptReport(s.nodeID, s.sessionID, report, now) {
					s.fail(&protocol.ProtocolError{Code: "session_mismatch", Message: "The protocol session does not match this connection."})
					return
				}
				if s.history != nil {
					s.history.ObserveSessionAt(s.nodeID, report, now, bootID)
				}
				if s.alerts != nil {
					_ = s.alerts.Observe(s.nodeID, s.live.Snapshot(s.nodeID), now)
				}
				lastReportAt, lastCollectedAt = now, report.CollectedAtUnixMilli
				resetTimer(reportTimer, protocol.OfflineAfter(s.config.ReportInterval))
				reportDeadline = reportTimer.C
			case protocol.TypePing:
				var ping protocol.Ping
				if protocol.DecodePayload(envelope.Payload, &ping) != nil {
					s.fail(&protocol.ProtocolError{Code: "invalid_envelope", Message: "The ping payload is invalid."})
					return
				}
				if _, err := s.enqueue(protocol.TypePong, ping, false); err != nil {
					s.close("slow_client")
					return
				}
			case protocol.TypePong:
				var pong protocol.Ping
				if protocol.DecodePayload(envelope.Payload, &pong) != nil {
					s.fail(&protocol.ProtocolError{Code: "invalid_envelope", Message: "The pong payload is invalid."})
					return
				}
				lastPong = s.now()
			case protocol.TypeProbeSync:
				status, decodeErr := protocol.DecodeProbeSyncStatus(envelope.Payload)
				if decodeErr != nil {
					s.fail(&protocol.ProtocolError{Code: "invalid_probe_sync", Message: "The probe sync status is invalid."})
					return
				}
				s.probeMu.Lock()
				currentVersion := s.probeVersion
				s.probeMu.Unlock()
				if status.Version > currentVersion {
					s.fail(&protocol.ProtocolError{Code: "invalid_probe_sync", Message: "The probe sync status version is invalid."})
					return
				}
				if status.Version == currentVersion && s.probes != nil {
					s.probes.RecordSyncStatus(s.nodeID, status)
				}
			case protocol.TypeProbeResult:
				now := s.now()
				result, decodeErr := protocol.DecodeProbeResult(envelope.Payload, now)
				if decodeErr != nil || !s.acceptProbeResult(result, now) {
					s.fail(&protocol.ProtocolError{Code: "invalid_probe_result", Message: "The TCP probe result is invalid or too frequent."})
					return
				}
				if s.probes != nil {
					s.probes.Observe(s.nodeID, result, now)
				}
			case protocol.TypeError:
				var peerError protocol.ErrorPayload
				if protocol.DecodePayload(envelope.Payload, &peerError) != nil {
					s.fail(&protocol.ProtocolError{Code: "invalid_envelope", Message: "The error payload is invalid."})
					return
				}
				// Peer errors are terminal and deliberately not echoed.
				return
			default:
				s.fail(&protocol.ProtocolError{Code: "unsupported_message", Message: "This protocol message is not available in the current milestone."})
				return
			}
		}
	}
}

func (s *agentSession) acceptProbeResult(result protocol.ProbeResult, now time.Time) bool {
	s.probeMu.Lock()
	defer s.probeMu.Unlock()
	task, assigned := s.probeTasks[result.TaskID]
	if !assigned {
		return false
	}
	spacing := int64(task.IntervalSeconds) * 1000 / 2
	if previous, exists := s.probeLastScheduled[result.TaskID]; exists && result.ScheduledAtUnixMilli-previous < spacing {
		return false
	}
	minimumReceiveSpacing := time.Duration(task.IntervalSeconds) * time.Second / 2
	if previous, exists := s.probeLastReceived[result.TaskID]; exists && now.Sub(previous) < minimumReceiveSpacing {
		return false
	}
	if s.probeWindowStart.IsZero() || now.Sub(s.probeWindowStart) >= 5*time.Second {
		s.probeWindowStart, s.probeWindowCount = now, 0
	}
	limit := len(s.probeTasks) * 2
	if limit < 2 {
		limit = 2
	}
	if s.probeWindowCount >= limit {
		return false
	}
	s.probeLastScheduled[result.TaskID] = result.ScheduledAtUnixMilli
	s.probeLastReceived[result.TaskID] = now
	s.probeWindowCount++
	return true
}

func validClientHello(hello protocol.ClientHello) bool {
	return hello.AgentVersion != "" && validHelloText(hello.AgentVersion, 64) && validHelloText(hello.Hostname, 255) &&
		validHelloText(hello.OS, 255) && validHelloText(hello.Kernel, 255) &&
		validHelloText(hello.Architecture, 32) && validHelloText(hello.Virtualization, 64) &&
		(hello.BootID == "" || protocol.ValidBootID(hello.BootID))
}

func validHelloText(value string, maximum int) bool {
	return len(value) <= maximum && utf8.ValidString(value) && !strings.ContainsFunc(value, func(character rune) bool {
		return character < 0x20 || character == 0x7f
	})
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}

func (s *agentSession) readLoop() {
	for {
		messageType, data, err := s.conn.ReadMessage()
		if err == nil && messageType != websocket.TextMessage {
			err = errors.New("agent protocol requires text frames")
		}
		input := incomingFrame{data: data, err: err}
		select {
		case s.incoming <- input:
		case <-s.ctx.Done():
			return
		default:
			select {
			case s.incoming <- incomingFrame{err: errQueueFull}:
			default:
			}
			s.close("slow_reader")
			return
		}
		if err != nil {
			return
		}
	}
}

func (s *agentSession) writeLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case output := <-s.outgoing:
			err := s.conn.SetWriteDeadline(time.Now().Add(s.config.WriteTimeout))
			if err == nil {
				err = s.conn.WriteJSON(output.envelope)
			}
			close(output.ack)
			if err != nil {
				s.close("write_failed")
				return
			}
			if output.closeAfter {
				return
			}
		}
	}
}
