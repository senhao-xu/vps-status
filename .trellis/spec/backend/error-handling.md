# Error Handling

## Current Go pattern

- Wrap errors at the boundary that adds useful context and preserve the original cause with `%w`.
- Compare sentinel/system errors with `errors.Is`, as `cmd/control/main.go` does for `http.ErrServerClosed`.
- Entry points print a concise terminal failure to stderr and return a non-zero exit code. Library packages return errors; they do not call `os.Exit` or panic for expected runtime failures.
- Panics are reserved for impossible initialization failures in static build assets. `internal/control/webui/assets_built.go` and `assets_fallback.go` panic only if their compile-time embedded subtree cannot be constructed.

## Boundary-specific behavior

- HTTP handlers convert internal errors to a stable code/message response and never expose SQL text, filesystem paths, credentials, or wrapped implementation details.
- Agent protocol errors use a bounded taxonomy owned by `internal/protocol`; never transmit raw Go errors or remote-controlled format strings.
- A full active enrollment-token set is a stable configuration conflict (`409 enrollment_token_limit`), not an authentication or internal-storage failure.
- Background workers report failures to their supervisor and use bounded retry/backoff. They must honor context cancellation.
- Notification channel errors are classified as retryable or terminal before entering the retry queue.

## Degraded telemetry mode

A telemetry failure is not a fatal Control failure after `control.db` is available. The storage boundary must return a typed availability state that lets Control:

- stop and drain/drop bounded history writes;
- continue authentication, configuration, current-state display, and real-time alerts;
- expose a clear operator-facing degraded reason;
- resume only after the disk/storage guard's recovery condition is met.

Do not scatter `if telemetryErr != nil { ignore }` branches across handlers. One telemetry service boundary owns degradation and state transitions.

## Tests

- Test errors by behavior or `errors.Is`/typed fields, not full wrapped strings.
- Every new external boundary needs invalid, timeout, cancellation, and redaction cases.
- HTTP and protocol tests assert stable public codes and absence of sensitive details.

## Avoid

- Swallowing an error without updating observable degraded state.
- Logging and returning the same sensitive error at multiple layers.
- Using panic for malformed requests, unavailable peers, SQLite busy states, or missing optional telemetry.
- Returning a successful response after a partial configuration write.
