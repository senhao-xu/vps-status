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
