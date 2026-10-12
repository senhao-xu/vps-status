// Package telemetry embeds the independent telemetry.db migration stream.
package telemetry

import "embed"

// Files contains the monotonic telemetry database migrations.
//
//go:embed *.sql
var Files embed.FS
