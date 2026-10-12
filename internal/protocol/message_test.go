package protocol

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEnvelopeContractAndAllowedTypes(t *testing.T) {
	allowed := []MessageType{TypeHello, TypeReport, TypeProbeSync, TypeProbeResult, TypePing, TypePong, TypeError}
	for _, messageType := range allowed {
		var payload any = map[string]bool{"ok": true}
		switch messageType {
		case TypeReport:
			payload = Report{CollectedAtUnixMilli: time.Now().UnixMilli()}
		case TypeProbeSync:
			payload = ProbeSync{Version: 1}
		case TypeProbeResult:
			payload = ProbeResult{TaskID: "task", ScheduledAtUnixMilli: time.Now().UnixMilli(), Error: ProbeErrorTimeout}
		}
		envelope, err := NewEnvelope(messageType, "session", 1, payload)
		if err != nil {
			t.Fatalf("NewEnvelope(%q): %v", messageType, err)
		}
		data, _ := json.Marshal(envelope)
		if _, err := DecodeEnvelope(data); err != nil {
			t.Fatalf("DecodeEnvelope(%q): %v", messageType, err)
		}
	}
	for _, data := range []string{
		`{"version":"v2","type":"hello","session_id":"s","seq":1,"payload":{}}`,
		`{"version":"v1","type":"command","session_id":"s","seq":1,"payload":{}}`,
		`{"version":"v1","type":"hello","session_id":"s","seq":0,"payload":{}}`,
		`{"version":"v1","type":"hello","session_id":"s","seq":1,"payload":{},"extra":1}`,
		`{"version":"v1","type":"hello","session_id":"s","seq":1,"payload":{}} trailing`,
	} {
		if _, err := DecodeEnvelope([]byte(data)); err == nil {
			t.Fatalf("DecodeEnvelope(%s) accepted invalid frame", data)
		}
	}
}

func TestDecodePayloadRejectsTrailingMalformedData(t *testing.T) {
	var value map[string]any
	if err := DecodePayload(json.RawMessage(`{} trailing`), &value); err == nil {
		t.Fatal("trailing malformed payload accepted")
	}
}

func TestClientHelloBootIDContract(t *testing.T) {
	valid := "550e8400-e29b-41d4-a716-446655440000"
	if !ValidBootID(valid) {
		t.Fatal("canonical boot ID rejected")
	}
	for _, value := range []string{"", "550E8400-E29B-41D4-A716-446655440000", valid + "0", "550e8400-e29b-41d4-a716-44665544000z", "主" + valid} {
		if ValidBootID(value) {
			t.Fatalf("invalid boot ID accepted: %q", value)
		}
	}
	var hello ClientHello
	if err := DecodePayload(json.RawMessage(`{"agent_version":"test","boot_id":"`+valid+`","unexpected":true}`), &hello); err == nil {
		t.Fatal("ClientHello accepted an unknown field")
	}
}

func TestFrameAndErrorBounds(t *testing.T) {
	if _, err := DecodeEnvelope([]byte(strings.Repeat("x", int(MaxFrameBytes)+1))); err == nil {
		t.Fatal("oversized frame accepted")
	}
	if _, err := NewEnvelope(TypePing, "session", 1, map[string]string{"value": strings.Repeat("x", int(MaxFrameBytes))}); err == nil {
		t.Fatal("oversized outbound frame accepted")
	}
	if _, err := NewEnvelope(TypePing, strings.Repeat("s", 129), 1, Ping{}); err == nil {
		t.Fatal("oversized outbound session ID accepted")
	}
	payload := BoundedErrorPayload(&ProtocolError{Code: "invalid", Message: strings.Repeat("m", 1000)})
	if len(payload.Message) != MaxErrorMessageBytes {
		t.Fatalf("bounded message length=%d", len(payload.Message))
	}
}
