package agent

import (
	"runtime"
	"time"

	"github.com/senhao-xu/vps-status/internal/protocol"
)

// SupportedPlatform reports whether this binary can collect host metrics.
func SupportedPlatform() bool { return runtime.GOOS == "linux" }

// Collector is the read-only host sampling boundary used by the outbound
// Agent client.
type Collector interface {
	Collect(time.Time) (protocol.Report, error)
}

// HostFacts are low-frequency identity facts sent in ClientHello.
type HostFacts struct {
	Hostname       string
	OS             string
	Kernel         string
	Architecture   string
	Virtualization string
	BootID         string
}
