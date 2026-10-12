package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"time"
)

const (
	// MaxFrameBytes bounds every Agent protocol JSON message.
	MaxFrameBytes int64 = 64 << 10
	// MaxErrorMessageBytes bounds peer-visible error detail.
	MaxErrorMessageBytes = 256
	// DefaultReportInterval is the single shared Agent reporting cadence.
	DefaultReportInterval = 3 * time.Second
	// MinReportInterval is the fastest cadence an Agent will accept from a
	// Control. It is also the lower bound used by the Control rate guard.
	MinReportInterval = time.Second
	// MaxReportInterval keeps a compromised Control from disabling useful live
	// monitoring for an unbounded period.
	MaxReportInterval = time.Minute
	// MissedReportLimit is the number of expected reports after which a
	// connected node becomes stale/offline.
	MissedReportLimit = 4
	// MaxReportClockSkew bounds timestamps supplied by an untrusted Agent.
	MaxReportClockSkew = 5 * time.Minute
)

// OfflineAfter derives the live-state timeout from the negotiated report
// interval. Callers must pass a validated interval.
func OfflineAfter(interval time.Duration) time.Duration { return MissedReportLimit * interval }

// MessageType is one of the deliberately narrow Agent protocol operations.
type MessageType string

const (
	TypeHello       MessageType = "hello"
	TypeReport      MessageType = "report"
	TypeProbeSync   MessageType = "probe.sync"
	TypeProbeResult MessageType = "probe.result"
	TypePing        MessageType = "ping"
	TypePong        MessageType = "pong"
	TypeError       MessageType = "error"
)

// Envelope is the shared versioned JSON frame. Seq is monotonic per sender.
type Envelope struct {
	Version   string          `json:"version"`
	Type      MessageType     `json:"type"`
	SessionID string          `json:"session_id"`
	Seq       uint64          `json:"seq"`
	Payload   json.RawMessage `json:"payload"`
}

// ServerHello establishes the connection-bound nonce and heartbeat contract.
type ServerHello struct {
	NodeID                   string `json:"node_id"`
	ServerNonce              string `json:"server_nonce"`
	HeartbeatIntervalSeconds int64  `json:"heartbeat_interval_seconds"`
	HeartbeatTimeoutSeconds  int64  `json:"heartbeat_timeout_seconds"`
	ReportIntervalMillis     int64  `json:"report_interval_millis"`
}

// ClientHello proves that the Agent received the server's connection-bound
// nonce and declares only static, read-only host identity fields.
type ClientHello struct {
	ServerNonce    string `json:"server_nonce"`
	AgentVersion   string `json:"agent_version"`
	Hostname       string `json:"hostname,omitempty"`
	OS             string `json:"os,omitempty"`
	Kernel         string `json:"kernel,omitempty"`
	Architecture   string `json:"architecture,omitempty"`
	Virtualization string `json:"virtualization,omitempty"`
	BootID         string `json:"boot_id,omitempty"`
}

// ValidBootID accepts only the canonical lowercase Linux boot UUID form.
func ValidBootID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index := 0; index < len(value); index++ {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if value[index] != '-' {
				return false
			}
			continue
		}
		character := value[index]
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return false
		}
	}
	return true
}

// Ping carries a peer timestamp for bounded liveness diagnostics.
type Ping struct {
	SentAtUnixMilli int64 `json:"sent_at_unix_milli"`
}

// ErrorPayload is the stable, bounded peer error contract.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ProtocolError is safe to map to an ErrorPayload; it never embeds raw peer
// input or internal errors.
type ProtocolError struct{ Code, Message string }

func (e *ProtocolError) Error() string { return e.Code + ": " + e.Message }

// DecodeEnvelope strictly decodes and validates the common envelope.
func DecodeEnvelope(data []byte) (Envelope, error) {
	if int64(len(data)) > MaxFrameBytes {
		return Envelope{}, &ProtocolError{Code: "message_too_large", Message: "The protocol frame exceeds the size limit."}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var envelope Envelope
	if err := decoder.Decode(&envelope); err != nil {
		return Envelope{}, &ProtocolError{Code: "invalid_envelope", Message: "The protocol frame is not valid JSON."}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Envelope{}, &ProtocolError{Code: "invalid_envelope", Message: "The protocol frame must contain one JSON object."}
	}
	if envelope.Version != Version {
		return Envelope{}, &ProtocolError{Code: "incompatible_version", Message: "The Agent protocol version is not supported."}
	}
	if !ValidMessageType(envelope.Type) {
		return Envelope{}, &ProtocolError{Code: "unsupported_message", Message: "The protocol message type is not supported."}
	}
	if envelope.SessionID == "" || len(envelope.SessionID) > 128 || envelope.Seq == 0 || len(envelope.Payload) == 0 {
		return Envelope{}, &ProtocolError{Code: "invalid_envelope", Message: "The protocol envelope is incomplete."}
	}
	return envelope, nil
}

// ValidMessageType reports whether a type is in the complete v1 surface.
func ValidMessageType(value MessageType) bool {
	switch value {
	case TypeHello, TypeReport, TypeProbeSync, TypeProbeResult, TypePing, TypePong, TypeError:
		return true
	default:
		return false
	}
}

// NewEnvelope marshals a typed payload into a validated envelope.
func NewEnvelope(messageType MessageType, sessionID string, seq uint64, payload any) (Envelope, error) {
	if !ValidMessageType(messageType) || sessionID == "" || len(sessionID) > 128 || seq == 0 {
		return Envelope{}, errors.New("invalid protocol envelope fields")
	}
	if messageType == TypeReport {
		var report Report
		switch value := payload.(type) {
		case Report:
			report = value
		case *Report:
			if value == nil {
				return Envelope{}, errors.New("invalid report payload")
			}
			report = *value
		default:
			return Envelope{}, errors.New("report payload must use protocol.Report")
		}
		if err := ValidateReport(report); err != nil {
			return Envelope{}, fmt.Errorf("invalid report payload: %w", err)
		}
	}
	if messageType == TypeProbeSync {
		switch value := payload.(type) {
		case ProbeSync:
			if err := ValidateProbeSync(value); err != nil {
				return Envelope{}, fmt.Errorf("invalid probe sync payload: %w", err)
			}
		case *ProbeSync:
			if value == nil {
				return Envelope{}, errors.New("invalid probe sync payload")
			}
			if err := ValidateProbeSync(*value); err != nil {
				return Envelope{}, fmt.Errorf("invalid probe sync payload: %w", err)
			}
		case ProbeSyncStatus:
			if err := ValidateProbeSyncStatus(value); err != nil {
				return Envelope{}, fmt.Errorf("invalid probe sync status: %w", err)
			}
		case *ProbeSyncStatus:
			if value == nil {
				return Envelope{}, errors.New("invalid probe sync status")
			}
			if err := ValidateProbeSyncStatus(*value); err != nil {
				return Envelope{}, fmt.Errorf("invalid probe sync status: %w", err)
			}
		default:
			return Envelope{}, errors.New("probe sync payload must use a protocol probe type")
		}
	}
	if messageType == TypeProbeResult {
		var result ProbeResult
		switch value := payload.(type) {
		case ProbeResult:
			result = value
		case *ProbeResult:
			if value == nil {
				return Envelope{}, errors.New("invalid probe result payload")
			}
			result = *value
		default:
			return Envelope{}, errors.New("probe result payload must use protocol.ProbeResult")
		}
		// Writers may use a deterministic test clock, so shape validation here
		// deliberately omits wall-clock skew. Receivers apply Validate...At.
		if !ValidProbeID(result.TaskID) || result.ScheduledAtUnixMilli <= 0 || math.IsNaN(result.LatencyMillis) || math.IsInf(result.LatencyMillis, 0) || result.LatencyMillis < 0 || result.LatencyMillis > MaxProbeLatencyMillis ||
			(result.Success && result.Error != "") || (!result.Success && (result.LatencyMillis != 0 || !ValidProbeError(result.Error))) {
			return Envelope{}, errors.New("invalid probe result payload")
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("marshal protocol payload: %w", err)
	}
	envelope := Envelope{Version: Version, Type: messageType, SessionID: sessionID, Seq: seq, Payload: raw}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return Envelope{}, fmt.Errorf("marshal protocol envelope: %w", err)
	}
	// Current writers use json.Encoder through websocket.WriteJSON, which adds
	// one trailing newline to the frame.
	if int64(len(encoded)+1) > MaxFrameBytes {
		return Envelope{}, errors.New("protocol frame exceeds the size limit")
	}
	return envelope, nil
}

// DecodePayload strictly decodes one typed payload.
func DecodePayload(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple payload values")
	}
	return nil
}

// BoundedErrorPayload converts an error to the stable peer contract.
func BoundedErrorPayload(err error) ErrorPayload {
	var protocolErr *ProtocolError
	if !errors.As(err, &protocolErr) {
		return ErrorPayload{Code: "internal_error", Message: "The connection could not continue."}
	}
	message := protocolErr.Message
	if len(message) > MaxErrorMessageBytes {
		message = message[:MaxErrorMessageBytes]
	}
	return ErrorPayload{Code: protocolErr.Code, Message: message}
}
