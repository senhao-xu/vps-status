package protocol

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func validReport(now time.Time) Report {
	return Report{
		CollectedAtUnixMilli: now.UnixMilli(),
		CPU:                  CPUStats{UsagePercent: 25.5},
		Load:                 LoadStats{One: .1, Five: .2, Fifteen: .3},
		Memory: MemoryStats{
			TotalBytes: 1024, UsedBytes: 512, AvailableBytes: 512,
			SwapTotalBytes: 256, SwapUsedBytes: 64,
		},
		Disk:          DiskStats{TotalBytes: 4096, UsedBytes: 1024, FreeBytes: 3072},
		Network:       NetworkStats{ReceiveBytesPerSecond: 10, TransmitBytesPerSecond: 20, ReceiveBytesTotal: 100, TransmitBytesTotal: 200},
		UptimeSeconds: 60,
		ProcessCount:  10,
		Connections:   ConnectionStats{TCP: 2, UDP: 1},
	}
}

func TestReportValidationAndStrictDecode(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	report := validReport(now)
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeReport(raw, now)
	if err != nil || decoded.ProcessCount != report.ProcessCount {
		t.Fatalf("DecodeReport()=%#v, %v", decoded, err)
	}

	invalid := []json.RawMessage{
		append(append(json.RawMessage(nil), raw...), []byte(` trailing`)...),
		json.RawMessage(`{"collected_at_unix_milli":1700000000000,"extra":true}`),
	}
	for _, candidate := range invalid {
		if _, err := DecodeReport(candidate, now); err == nil {
			t.Fatalf("DecodeReport(%q) accepted invalid payload", candidate)
		}
	}
}

func TestReportRejectsNumericRelationalTimestampAndAreaViolations(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	tests := []struct {
		name   string
		mutate func(*Report)
	}{
		{"nan CPU", func(report *Report) { report.CPU.UsagePercent = math.NaN() }},
		{"infinite load", func(report *Report) { report.Load.One = math.Inf(1) }},
		{"used memory", func(report *Report) { report.Memory.UsedBytes = report.Memory.TotalBytes + 1 }},
		{"memory relation", func(report *Report) { report.Memory.AvailableBytes-- }},
		{"swap", func(report *Report) { report.Memory.SwapUsedBytes = report.Memory.SwapTotalBytes + 1 }},
		{"disk", func(report *Report) { report.Disk.FreeBytes = report.Disk.TotalBytes + 1 }},
		{"disk relation", func(report *Report) { report.Disk.FreeBytes-- }},
		{"network rate", func(report *Report) { report.Network.ReceiveBytesPerSecond = MaxRateBytesPerSecond + 1 }},
		{"process count", func(report *Report) { report.ProcessCount = MaxProcessCount + 1 }},
		{"unknown area", func(report *Report) { report.Unavailable = []MetricArea{"filesystem-path"} }},
		{"duplicate area", func(report *Report) { report.Unavailable = []MetricArea{AreaCPU, AreaCPU} }},
		{"stale timestamp", func(report *Report) {
			report.CollectedAtUnixMilli = now.Add(-MaxReportClockSkew - time.Millisecond).UnixMilli()
		}},
		{"future timestamp", func(report *Report) {
			report.CollectedAtUnixMilli = now.Add(MaxReportClockSkew + time.Millisecond).UnixMilli()
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := validReport(now)
			test.mutate(&report)
			if err := ValidateReportAt(report, now); err == nil {
				t.Fatal("invalid report accepted")
			}
		})
	}
}

func TestReportEnvelopeRequiresCanonicalPayload(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	if _, err := NewEnvelope(TypeReport, "session", 1, validReport(now)); err != nil {
		t.Fatalf("canonical report rejected: %v", err)
	}
	if _, err := NewEnvelope(TypeReport, "session", 1, map[string]any{"collected_at_unix_milli": now.UnixMilli()}); err == nil {
		t.Fatal("untyped report payload accepted")
	}
}
