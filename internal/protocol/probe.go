package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/netip"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MaxProbeTasks is the hard per-node task limit enforced by both peers.
	MaxProbeTasks = 64
	// MinProbeIntervalSeconds and MaxProbeIntervalSeconds bound task cadence.
	MinProbeIntervalSeconds uint32 = 5
	MaxProbeIntervalSeconds uint32 = 3600
	// MaxProbeHostBytes keeps task snapshots and DNS input bounded.
	MaxProbeHostBytes = 253
	// MaxProbeIDBytes bounds stable task identifiers.
	MaxProbeIDBytes = 128
	// MaxProbeClockSkew bounds scheduled timestamps supplied by an Agent.
	MaxProbeClockSkew = 5 * time.Minute
	// MaxProbeLatencyMillis is the complete resolve/connect deadline.
	MaxProbeLatencyMillis = 3000
)

// ProbeErrorCode is the deliberately small connect-only failure taxonomy.
type ProbeErrorCode string

const (
	ProbeErrorDNS                ProbeErrorCode = "dns"
	ProbeErrorTimeout            ProbeErrorCode = "timeout"
	ProbeErrorConnectionRefused  ProbeErrorCode = "connection_refused"
	ProbeErrorNetworkUnreachable ProbeErrorCode = "network_unreachable"
	ProbeErrorHostUnreachable    ProbeErrorCode = "host_unreachable"
	ProbeErrorAddressInvalid     ProbeErrorCode = "address_invalid"
	ProbeErrorConnect            ProbeErrorCode = "connect"
)

// ProbeTask is one TCP connect-and-close assignment in a complete snapshot.
type ProbeTask struct {
	ID              string `json:"id"`
	Host            string `json:"host"`
	Port            uint16 `json:"port"`
	IntervalSeconds uint32 `json:"interval_seconds"`
}

// ProbeSync is a versioned, complete task snapshot from Control.
type ProbeSync struct {
	Version uint64      `json:"version"`
	Tasks   []ProbeTask `json:"tasks"`
}

// ProbeRejection reports one bounded reason why an untrusted snapshot was not
// scheduled. TaskID may be empty for a snapshot-wide rejection.
type ProbeRejection struct {
	TaskID string `json:"task_id,omitempty"`
	Code   string `json:"code"`
}

// ProbeSyncStatus acknowledges one snapshot without echoing its targets.
type ProbeSyncStatus struct {
	Version  uint64           `json:"version"`
	Accepted uint32           `json:"accepted"`
	Rejected []ProbeRejection `json:"rejected,omitempty"`
}

// ProbeResult is one connect-only outcome. Failures never carry raw network
// errors and successful results never carry a failure code.
type ProbeResult struct {
	TaskID               string         `json:"task_id"`
	ScheduledAtUnixMilli int64          `json:"scheduled_at_unix_milli"`
	Success              bool           `json:"success"`
	LatencyMillis        float64        `json:"latency_millis"`
	Error                ProbeErrorCode `json:"error,omitempty"`
}

// ValidateProbeTask validates a task independently at either untrusted peer.
func ValidateProbeTask(task ProbeTask) error {
	if !ValidProbeID(task.ID) || task.Port == 0 || task.IntervalSeconds < MinProbeIntervalSeconds || task.IntervalSeconds > MaxProbeIntervalSeconds {
		return errors.New("invalid probe task")
	}
	return ValidateProbeHost(task.Host)
}

// ValidateProbeHost permits DNS names and public/private/loopback IPs while
// rejecting destinations which can never identify a concrete unicast peer.
func ValidateProbeHost(host string) error {
	if host == "" || len(host) > MaxProbeHostBytes || !utf8.ValidString(host) || strings.TrimSpace(host) != host || strings.ContainsAny(host, "\x00\r\n\t /\\[]%") {
		return errors.New("invalid probe host")
	}
	if address, err := netip.ParseAddr(host); err == nil {
		address = address.Unmap()
		if address.IsUnspecified() || address.IsMulticast() {
			return errors.New("invalid probe address class")
		}
		return nil
	}
	if strings.HasSuffix(host, ".") {
		host = strings.TrimSuffix(host, ".")
	}
	if host == "" || len(host) > MaxProbeHostBytes {
		return errors.New("invalid probe hostname")
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("invalid probe hostname")
		}
		for _, character := range label {
			if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
				(character >= '0' && character <= '9') || character == '-' {
				continue
			}
			return errors.New("invalid probe hostname")
		}
	}
	return nil
}

// ValidProbeAddress applies the post-DNS address-class policy.
func ValidProbeAddress(address netip.Addr) bool {
	return address.IsValid() && !address.Unmap().IsUnspecified() && !address.Unmap().IsMulticast()
}

// ValidateProbeSync validates an honest complete snapshot. Agent callers that
// need bounded per-task rejection should inspect tasks individually instead.
func ValidateProbeSync(sync ProbeSync) error {
	if sync.Version == 0 || len(sync.Tasks) > MaxProbeTasks {
		return errors.New("invalid probe snapshot")
	}
	seen := make(map[string]struct{}, len(sync.Tasks))
	for _, task := range sync.Tasks {
		if err := ValidateProbeTask(task); err != nil {
			return err
		}
		if _, exists := seen[task.ID]; exists {
			return errors.New("duplicate probe task")
		}
		seen[task.ID] = struct{}{}
	}
	return nil
}

// ValidateProbeSyncStatus bounds acknowledgement memory and stable codes.
func ValidateProbeSyncStatus(status ProbeSyncStatus) error {
	if status.Version == 0 || status.Accepted > MaxProbeTasks || len(status.Rejected) > MaxProbeTasks || int(status.Accepted)+len(status.Rejected) > MaxProbeTasks {
		return errors.New("invalid probe sync status")
	}
	for _, rejection := range status.Rejected {
		if rejection.TaskID != "" && !ValidProbeID(rejection.TaskID) {
			return errors.New("invalid probe rejection task")
		}
		switch rejection.Code {
		case "task_limit", "interval", "target", "duplicate", "stale_version":
		default:
			return errors.New("invalid probe rejection code")
		}
	}
	return nil
}

// ValidateProbeResultAt validates result shape and its bounded timestamp.
func ValidateProbeResultAt(result ProbeResult, now time.Time) error {
	if !ValidProbeID(result.TaskID) || result.ScheduledAtUnixMilli <= 0 || math.IsNaN(result.LatencyMillis) || math.IsInf(result.LatencyMillis, 0) || result.LatencyMillis < 0 || result.LatencyMillis > MaxProbeLatencyMillis {
		return errors.New("invalid probe result")
	}
	scheduled := time.UnixMilli(result.ScheduledAtUnixMilli)
	if scheduled.Before(now.Add(-MaxProbeClockSkew)) || scheduled.After(now.Add(MaxProbeClockSkew)) {
		return errors.New("invalid probe result timestamp")
	}
	if result.Success {
		if result.Error != "" {
			return errors.New("successful probe has an error")
		}
		return nil
	}
	if result.LatencyMillis != 0 || !ValidProbeError(result.Error) {
		return errors.New("invalid failed probe result")
	}
	return nil
}

// ValidProbeError reports whether a failure uses the bounded taxonomy.
func ValidProbeError(code ProbeErrorCode) bool {
	switch code {
	case ProbeErrorDNS, ProbeErrorTimeout, ProbeErrorConnectionRefused, ProbeErrorNetworkUnreachable,
		ProbeErrorHostUnreachable, ProbeErrorAddressInvalid, ProbeErrorConnect:
		return true
	default:
		return false
	}
}

// DecodeProbeSync and its peers strictly decode one probe payload.
func DecodeProbeSync(raw json.RawMessage) (ProbeSync, error) {
	var value ProbeSync
	if err := strictProbeDecode(raw, &value); err != nil {
		return ProbeSync{}, err
	}
	return value, nil
}

func DecodeProbeSyncStatus(raw json.RawMessage) (ProbeSyncStatus, error) {
	var value ProbeSyncStatus
	if err := strictProbeDecode(raw, &value); err != nil {
		return ProbeSyncStatus{}, err
	}
	if err := ValidateProbeSyncStatus(value); err != nil {
		return ProbeSyncStatus{}, err
	}
	return value, nil
}

func DecodeProbeResult(raw json.RawMessage, now time.Time) (ProbeResult, error) {
	var value ProbeResult
	if err := strictProbeDecode(raw, &value); err != nil {
		return ProbeResult{}, err
	}
	if err := ValidateProbeResultAt(value, now); err != nil {
		return ProbeResult{}, err
	}
	return value, nil
}

func strictProbeDecode(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple probe payload values")
	}
	return nil
}

// ValidProbeID reports whether a task identifier is safe to retain or echo in
// a bounded protocol payload.
func ValidProbeID(value string) bool {
	return value != "" && len(value) <= MaxProbeIDBytes && utf8.ValidString(value) && !strings.ContainsFunc(value, func(character rune) bool {
		return character < 0x20 || character == 0x7f
	})
}
