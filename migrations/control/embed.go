// Package control embeds the monotonic control.db migration stream.
package control

import "embed"

// Files contains the SQL migrations applied by internal/storage/control.
//
//go:embed *.sql
var Files embed.FS
