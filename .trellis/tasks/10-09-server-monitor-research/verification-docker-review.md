# Docker Agent independent review — 2026-10-25

Reviewed the Docker increment against the active task PRD/design/implementation
plan and the backend/frontend ownership, enrollment, collection, operations,
state, accessibility and quality contracts. Earlier unrelated dirty changes
were preserved.

## Findings

No actionable source defect found. No source edits were needed during review.

- CLI source flags retain native defaults and feed both hello facts and report
  collection. Explicitly missing sources remain unavailable rather than using
  container paths; the real websocket fixture verifies both consumers.
- Agent Dockerfile uses automatic BuildKit target arguments without overriding
  the target architecture. Nonroot distroless includes CA roots, and the data
  directory is explicitly copied UID/GID65532 and mode0700.
- Temporary enrollment and persistent runtime share only their credential
  volume. Runtime has no enrollment token, added capabilities, privileged mode
  or Docker socket. Host namespace/mount options match collection paths; the
  root bind is nonrecursive.
- Docker shell construction quotes actual argv, validates complete naming IDs,
  reuses a distinct data volume and short-circuits before runtime on enrollment
  failure. Actual POSIX shell tests exercise quoting and failure behavior.
- Installation method is local presentation state. It is not a polling-effect
  dependency and does not mint a token; the resolved request URL is stored with
  the enrollment. Clipboard follows the selected command. Existing address,
  release, status, revoke and regenerate behavior remains in its owner.
- GHCR publication uses job-scoped packages:write and the Actions token;
  reusable caller grants the same permission. Multiarch, source/version/SHA
  metadata and stable latest/version tag rules are present. Existing binary
  Release workflow is retained.
- User docs and owning specs cover Docker, host access, credentials, restart,
  image tag selection and separate publication/access verification.

## Checks performed by reviewer

- Frontend lint: pass.
- Frontend strict typecheck: pass.
- Frontend tests: 24 files, 273 tests pass.
- Go Agent CLI, collection/client and enrollment integration packages: pass.
- Agent Compose profile configuration: pass.
- git diff --check: pass.

Actual container/browser integration, image publication/public access and
existing Control redeployment are owned by the main session; this review does
not certify those external-state outcomes.

## Compiler and dependency security follow-up

The initial security run found inherited Go1.24 standard-library advisories.
Reviewed the scoped repair independently after the main session authorized the
required crypto-module update. No further source changes were needed by review.

- Agent and Control Docker build stages now select `golang:1.27.3-bookworm`.
  Automatic target-platform arguments and Agent filesystem permissions remain
  intact. Independent amd64 and arm64 CGO-free Agent builds record Go1.27.3;
  target architecture and new dependency metadata are confirmed by `go version -m`.
- `actions/setup-go@v5` source `src/installer.ts:parseGoVersionFile` reads the
  `go` directive and does not prioritize `toolchain`. Ordinary module commands
  then honor auto toolchain selection. Verified even a Go1.24 auto baseline
  selects Go1.27.3 in the repository. The Security job now explicitly selects
  Go1.27.3, so scanner execution does not rely on that bootstrap behavior.
- Independently reproduced govulncheck1.1.4's Go1.27 AST `KeyValueExpr` panic.
  Updating the scanner to1.9.0 is required for the patched compiler. No scanner
  severity suppression or vulnerability exception was introduced.
- Module delta is limited to x/crypto0.40→0.55 and its required x/sys0.34→0.47.
  Both new modules require Go1.25, so go.mod minimum and README now state1.25;
  selected toolchain remains1.27.3. Other dependencies are unchanged.
- `go mod verify` passes. Independent password/authentication/session/MFA/
  enrollment behavior tests pass. Deterministic Argon2id output is identical
  across crypto0.40/0.55 for actual default64MiB/3iterations/parallelism2 and
  the bounded test configuration; prior stored password hashes remain compatible.
- Independent govulncheck1.9 Agent scan exits0, with0reachable and0imported-package
  vulnerabilities. It reports3advisories in required but unused modules; this
  must not be summarized as zero advisories across every module.
- Inspected final Trivy0.70 JSON reports for the exact source overlay and both
  rebuilt Agent architectures: each has0HIGH/CRITICAL findings and0secrets.
  Reports: `/tmp/vps-status-docker-agent/compiler-check/{source,agent-amd64,agent-arm64}-trivy.json`.

Security follow-up review passes. Final GHCR access and deployment checks remain
with the main session.

## Agent cancellation race follow-up

GitHub dispatch40169653139 exposed a real handshake cancellation race: context
cancellation closes the websocket transport, so `SetReadDeadline` can return a
closed-socket error before `ReadMessage` observes `context.Canceled`. A separate
CI run passing the same source does not invalidate that race.

Reviewed the narrow `client.go` normalization and `agent_test.go` regressions:

- `sessionIOError` prioritizes `ctx.Err()` only when connect, deadline or send
  operations already failed; ordinary noncanceled errors retain their original
  wrapping. Protocol-validation failures and successful operations are unchanged.
  It returns the actual context error, preserving both canceled/deadline semantics.
- Existing read-loop cancellation handling remains. The change aligns initial
  deadline, hello send, initial heartbeat deadline, heartbeat refresh and pong/
  probe-sync sends with that established behavior; no protocol or architecture
  changes are involved.
- The test transport synchronously cancels and closes the real underlying
  connection at five selected deadline boundaries, proving the formerly racy
  ordering directly. A noncanceled closed-transport control asserts
  `errors.Is(err, net.ErrClosed)` and a still-active context.
- Independent reviewer run: original handshake regression plus new deterministic
  boundary cases under `go test -race`, repeated20times, passes. Package vet,
  gofmt and diff checks pass. The implementer's full matrix and100repeat run
  are separately recorded by the main session.

No actionable review finding remains; no additional code edits were needed.
