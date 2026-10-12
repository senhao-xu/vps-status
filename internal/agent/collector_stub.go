//go:build !linux

package agent

import (
	"errors"
	"os"
	"runtime"
	"time"

	"github.com/senhao-xu/vps-status/internal/protocol"
)

type unsupportedCollector struct{}

// NewCollector preserves cross-platform compilation while the runtime remains
// intentionally Linux-only.
func NewCollector() Collector { return unsupportedCollector{} }

func (unsupportedCollector) Collect(time.Time) (protocol.Report, error) {
	return protocol.Report{}, errors.New("host collection is supported only on Linux")
}

// CollectHostFacts returns only portable facts for build verification; the
// Agent entrypoint rejects unsupported platforms before connecting.
func CollectHostFacts() HostFacts {
	hostname, _ := os.Hostname()
	return HostFacts{Hostname: hostname, OS: runtime.GOOS, Architecture: runtime.GOARCH}
}
