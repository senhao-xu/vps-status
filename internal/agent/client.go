package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/senhao-xu/vps-status/internal/protocol"
)

// Client is an outbound-only Agent protocol client.
type Client struct {
	Config       Config
	AgentVersion string
	Dialer       *websocket.Dialer
	WriteTimeout time.Duration
	Collector    Collector
	SourcePaths  SourcePaths
	Now          func() time.Time
	ProbeConfig  ProbeSchedulerConfig
}

// Run connects until ctx is cancelled using the supplied bounded retry policy.
func (c *Client) Run(ctx context.Context, policy RetryPolicy) error {
	return RunReconnect(ctx, policy, c.ConnectOnce)
}

// ConnectOnce runs one authenticated Agent WSS session.
func (c *Client) ConnectOnce(ctx context.Context) error {
	endpoint, err := websocketURL(c.Config.ControlURL)
	if err != nil {
		return err
	}
	dialer := c.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.Config.Credential)
	connection, response, err := dialer.DialContext(ctx, endpoint, header)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("connect to Control: %w", err)
	}
	defer connection.Close()
	connection.SetReadLimit(protocol.MaxFrameBytes)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			_ = connection.Close()
		case <-stop:
		}
	}()
	if err := connection.SetReadDeadline(time.Now().Add(20 * time.Second)); err != nil {
		return err
	}
	messageType, data, err := connection.ReadMessage()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("Control hello was not received")
	}
	if messageType != websocket.TextMessage {
		return errors.New("Control hello was not received")
	}
	envelope, err := protocol.DecodeEnvelope(data)
	if err != nil || envelope.Type != protocol.TypeHello {
		return errors.New("Control hello is invalid")
	}
	if envelope.Seq != 1 {
		return errors.New("Control hello sequence is invalid")
	}
	var hello protocol.ServerHello
	if protocol.DecodePayload(envelope.Payload, &hello) != nil || hello.ServerNonce == "" || hello.NodeID != c.Config.NodeID {
		return errors.New("Control hello is invalid")
	}
	facts := CollectHostFactsWithPaths(c.SourcePaths)
	if facts.Hostname == "" {
		facts.Hostname, _ = os.Hostname()
	}
	agentVersion := c.AgentVersion
	if agentVersion == "" {
		agentVersion = "dev"
	}
	if hello.HeartbeatTimeoutSeconds < 1 || hello.HeartbeatTimeoutSeconds > 3600 {
		return errors.New("Control heartbeat timeout is invalid")
	}
	if hello.HeartbeatIntervalSeconds < 1 || hello.HeartbeatIntervalSeconds > hello.HeartbeatTimeoutSeconds {
		return errors.New("Control heartbeat interval is invalid")
	}
	if hello.ReportIntervalMillis < protocol.MinReportInterval.Milliseconds() ||
		hello.ReportIntervalMillis > protocol.MaxReportInterval.Milliseconds() {
		return errors.New("Control report interval is invalid")
	}
	reportInterval := time.Duration(hello.ReportIntervalMillis) * time.Millisecond
	writer := &sessionWriter{client: c, connection: connection, sessionID: envelope.SessionID}
	if err := writer.send(protocol.TypeHello, protocol.ClientHello{
		ServerNonce: hello.ServerNonce, AgentVersion: agentVersion, Hostname: facts.Hostname, OS: facts.OS,
		Kernel: facts.Kernel, Architecture: facts.Architecture, Virtualization: facts.Virtualization, BootID: facts.BootID,
	}); err != nil {
		return err
	}
	heartbeatTimeout := time.Duration(hello.HeartbeatTimeoutSeconds) * time.Second
	if err := connection.SetReadDeadline(time.Now().Add(heartbeatTimeout)); err != nil {
		return fmt.Errorf("set Control heartbeat deadline: %w", err)
	}
	serverSequence := envelope.Seq
	collector := c.Collector
	if collector == nil {
		collector = NewCollectorWithPaths(c.SourcePaths)
	}
	reportErrors := make(chan error, 1)
	reportContext, stopReports := context.WithCancel(ctx)
	defer stopReports()
	go c.runReports(reportContext, connection, writer, collector, reportInterval, reportErrors)
	probes := NewProbeScheduler(reportContext, c.ProbeConfig, func(result protocol.ProbeResult) error {
		err := writer.send(protocol.TypeProbeResult, result)
		if err != nil {
			select {
			case reportErrors <- fmt.Errorf("send TCP probe result: %w", err):
			default:
			}
			_ = connection.Close()
		}
		return err
	})
	defer probes.Close()
	for {
		messageType, data, err = connection.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			select {
			case reportErr := <-reportErrors:
				return reportErr
			default:
			}
			return fmt.Errorf("read Control frame: %w", err)
		}
		if messageType != websocket.TextMessage {
			return errors.New("Control sent a non-text protocol frame")
		}
		envelope, err = protocol.DecodeEnvelope(data)
		if err != nil || envelope.SessionID != writer.sessionID {
			return errors.New("Control protocol frame is invalid")
		}
		if envelope.Seq != serverSequence+1 {
			return errors.New("Control protocol sequence is invalid")
		}
		serverSequence = envelope.Seq
		if err := connection.SetReadDeadline(time.Now().Add(heartbeatTimeout)); err != nil {
			return fmt.Errorf("refresh Control heartbeat deadline: %w", err)
		}
		switch envelope.Type {
		case protocol.TypeProbeSync:
			sync, decodeErr := protocol.DecodeProbeSync(envelope.Payload)
			if decodeErr != nil {
				return errors.New("Control probe snapshot is invalid")
			}
			status := probes.Apply(sync)
			if err := writer.send(protocol.TypeProbeSync, status); err != nil {
				return err
			}
		case protocol.TypePing:
			var ping protocol.Ping
			if protocol.DecodePayload(envelope.Payload, &ping) != nil {
				return errors.New("Control ping is invalid")
			}
			if err := writer.send(protocol.TypePong, ping); err != nil {
				return err
			}
		case protocol.TypePong:
			var pong protocol.Ping
			if protocol.DecodePayload(envelope.Payload, &pong) != nil {
				return errors.New("Control pong is invalid")
			}
			continue
		case protocol.TypeError:
			var payload protocol.ErrorPayload
			if protocol.DecodePayload(envelope.Payload, &payload) != nil {
				return errors.New("Control protocol error is invalid")
			}
			return fmt.Errorf("Control closed Agent protocol: %s", payload.Code)
		default:
			return errors.New("Control sent a message unavailable in this Agent milestone")
		}
	}
}

type sessionWriter struct {
	client     *Client
	connection *websocket.Conn
	sessionID  string
	mu         sync.Mutex
	sequence   uint64
}

func (writer *sessionWriter) send(messageType protocol.MessageType, payload any) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.sequence++
	envelope, err := protocol.NewEnvelope(messageType, writer.sessionID, writer.sequence, payload)
	if err != nil {
		return err
	}
	return writer.client.write(writer.connection, envelope)
}

func (c *Client) runReports(ctx context.Context, connection *websocket.Conn, writer *sessionWriter, collector Collector, interval time.Duration, result chan<- error) {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			now := time.Now().UTC()
			if c.Now != nil {
				now = c.Now().UTC()
			}
			report, err := collector.Collect(now)
			if ctx.Err() != nil {
				return
			}
			if err == nil {
				err = writer.send(protocol.TypeReport, report)
			}
			if err != nil {
				select {
				case result <- fmt.Errorf("collect and send host report: %w", err):
				default:
				}
				_ = connection.Close()
				return
			}
			timer.Reset(interval)
		}
	}
}

func (c *Client) write(connection *websocket.Conn, envelope protocol.Envelope) error {
	timeout := c.WriteTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if err := connection.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return fmt.Errorf("set Agent write deadline: %w", err)
	}
	if err := connection.WriteJSON(envelope); err != nil {
		return fmt.Errorf("write Agent protocol frame: %w", err)
	}
	return nil
}

func websocketURL(controlURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(controlURL, "/"))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("Control URL is invalid")
	}
	if !secureOrLoopbackHTTP(parsed) {
		return "", errors.New("Control URL must use HTTPS except on loopback")
	}
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	default:
		return "", errors.New("Control URL must use HTTP or HTTPS")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/agent/v1/ws"
	parsed.RawQuery, parsed.Fragment = "", ""
	return parsed.String(), nil
}
