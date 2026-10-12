// Package telemetry owns the physically independent telemetry.db lifecycle,
// bounded writer, rollups, retention, traffic ledger, and disk safeguards.
// It never imports or writes the control database.
package telemetry
