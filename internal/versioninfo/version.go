// Package versioninfo exposes build metadata shared by the project binaries.
package versioninfo

import (
	"fmt"

	"github.com/senhao-xu/vps-status/internal/protocol"
)

// Version and Commit are replaced with -ldflags for release builds.
var (
	Version = "dev"
	Commit  = "unknown"
)

// String returns a stable, human-readable build summary.
func String(component string) string {
	return fmt.Sprintf("vps-status-%s version=%s commit=%s protocol=%s", component, Version, Commit, protocol.Version)
}
