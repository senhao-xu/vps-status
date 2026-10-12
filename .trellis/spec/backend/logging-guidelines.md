# Logging Guidelines

## Current and target boundary

The foundation entrypoints currently use `fmt` for their one startup/fatal message. Structured application logging is not implemented yet. The first logging implementation must be centralized in Control/Agent assembly and this guide updated with its exact API; do not create package-local logger frameworks.

## Required fields and levels

- Prefer structured key/value events with stable names.
- `debug`: bounded diagnostics useful during development, disabled by default.
- `info`: lifecycle transitions such as startup, clean shutdown, Agent connect/disconnect, migration completion, and telemetry recovery.
- `warn`: rejected/rate-limited input, history degradation, notification retry, or incompatible peer without process failure.
- `error`: an operation failed and needs operator action or exhausted retries.
- Include component, operation, stable node/task identifiers when relevant, and an error category. Avoid high-cardinality raw payload fields.

## Never log

- Authorization headers, enrollment/node credentials, session cookies, passwords, TOTP secrets, recovery codes, master keys, Telegram bot tokens, or full secret-bearing webhook URLs.
- Full Agent reports, protocol frames, notification bodies, or database rows at normal levels.
- Public-facing IP/hostname data in a log event intended for an unauthenticated response.

Redact before passing values to the logger, not in an output sink. Tests should search captured output for both the original secret and common URL/query encodings.

## Operational events

- Log credential creation only by credential ID and node ID; never log the one-time plaintext.
- Log telemetry main/WAL/SHM sizes as numbers and the reason when history writing pauses/resumes.
- Log TCP task rejection by bounded reason (`task_limit`, `interval`, `target`, `rate`) without logging arbitrary payload data.
- Aggregate repeated errors or log state transitions rather than emitting one event for every 3-second report.

## Avoid

- `fmt.Printf` in reusable packages.
- Logging an error at every call layer.
- Using logs as the only operator signal for telemetry degradation; the admin API/UI also exposes it.
- Building log messages from untrusted format strings.
