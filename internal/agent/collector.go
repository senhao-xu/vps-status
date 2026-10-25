package agent

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/senhao-xu/vps-status/internal/protocol"
)

// SupportedPlatform reports whether this binary can collect host metrics.
func SupportedPlatform() bool { return runtime.GOOS == "linux" }

// SourcePaths selects operator-local read-only sources for host collection.
// These paths are runtime options, not part of the persisted Agent config.
type SourcePaths struct {
	ProcRoot  string
	SysRoot   string
	RootFS    string
	OSRelease string
}

// DefaultSourcePaths returns the native Linux host source locations.
func DefaultSourcePaths() SourcePaths {
	return SourcePaths{ProcRoot: "/proc", SysRoot: "/sys", RootFS: "/", OSRelease: "/etc/os-release"}
}

// Validate requires explicit absolute paths without requiring readable sources.
// Missing sources remain partial-unavailable metrics during collection.
func (paths SourcePaths) Validate() error {
	for _, source := range []struct{ flag, path string }{
		{"--proc-root", paths.ProcRoot}, {"--sys-root", paths.SysRoot},
		{"--rootfs", paths.RootFS}, {"--os-release", paths.OSRelease},
	} {
		if !filepath.IsAbs(source.path) || strings.ContainsRune(source.path, '\x00') {
			return fmt.Errorf("%s must be a non-empty absolute path", source.flag)
		}
	}
	return nil
}

func (paths SourcePaths) withDefaults() SourcePaths {
	defaults := DefaultSourcePaths()
	if paths.ProcRoot == "" {
		paths.ProcRoot = defaults.ProcRoot
	}
	if paths.SysRoot == "" {
		paths.SysRoot = defaults.SysRoot
	}
	if paths.RootFS == "" {
		paths.RootFS = defaults.RootFS
	}
	if paths.OSRelease == "" {
		paths.OSRelease = defaults.OSRelease
	}
	return paths
}

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
