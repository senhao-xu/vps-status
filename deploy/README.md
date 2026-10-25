# Deployment and operations

## Control with Docker Compose

The production image is a non-root, read-only distroless runtime containing one
CGO-free Control binary with the Web application embedded. Node.js and pnpm are
used only in the build stage and are absent from the production image.

```bash
docker compose -f deploy/docker-compose.yml build
docker compose -f deploy/docker-compose.yml up -d
docker compose -f deploy/docker-compose.yml ps
```

The named `control-data` volume persists `/data`; the host port binds only to
loopback by default. `/healthz` is liveness and `/readyz` is readiness. Both
return only `ok`; Compose and the image use the binary's bounded `healthcheck`
subcommand, so the runtime image needs no shell or HTTP utility.

Terminate public TLS at a reverse proxy and preserve the original `Host`.
Examples are in `reverse-proxy/Caddyfile` and `reverse-proxy/nginx.conf`; both
pass WebSocket upgrades for `/agent/v1/ws`, and the nginx example disables
buffering for the administrator SSE stream. Replace `monitor.example.com` and
certificate paths before use. Never expose plain HTTP/WSS on the public
Internet.

## Agent systemd installation

Configure the shared node HTTPS entry and optional Agent release override in
the administrator **Installation settings** page. When the entry is blank,
the browser's current HTTPS address is used; a localhost/tunnel entry requires
an explicit address reachable by the remote nodes. Adding a node then asks
only for its name. For local testing with blank settings and a loopback page
address, select **Local testing only** in the add-node dialog. This
explicitly uses the current local address for an Agent on the same computer;
remote servers still need a reachable HTTPS entry. It does not change the
shared installation settings.

The generated command downloads `bootstrap-agent.sh` directly from the pinned
GitHub Release (or the configured HTTPS release mirror), detects Linux amd64/arm64, verifies the Agent,
installer, and systemd unit against `SHA256SUMS`, then enrolls and starts the
service. Root is needed only to install files and the service account; runtime
uses the locked `vps-status-agent` system user. The one-time token is consumed
on successful enrollment and is never printed by either installer. The copied
command itself can remain in shell history, so use the shortest practical TTL,
run it only on the intended host, and generate a new enrollment if it expires
or may have been exposed.

For audit-sensitive environments, download and inspect the public bootstrap
script before passing the arguments shown by the UI. Replace `v0.1.0` with an
actual published version containing the standalone bootstrap:

```bash
curl -fsSLo /tmp/vps-status-install.sh https://github.com/senhao-xu/vps-status/releases/download/v0.1.0/bootstrap-agent.sh
less /tmp/vps-status-install.sh
sudo sh /tmp/vps-status-install.sh https://monitor.example.com ONE_TIME_ENROLLMENT_TOKEN \
  v0.1.0 https://github.com/senhao-xu/vps-status/releases/download
```

Control defaults to the version embedded in its binary and the versioned
GitHub Release directory. Override a mirror or version with
`--agent-release-base-url` and `--agent-release-version`; the base URL must be
HTTPS and the version is restricted to a path-safe identifier. A source build
whose version is `dev` cannot generate a command until an explicit release is
configured. A saved Agent version in Installation settings overrides the
startup default; the release mirror remains a startup setting. Configuration
validates the version format, not its presence on GitHub. The Release must
contain `bootstrap-agent.sh`, both Agent architectures, the systemd installer
and unit, and `SHA256SUMS`. Releases created before this bootstrap asset was
added need a new release or complete asset set before using the new command.

The public Control `/install-agent.sh` endpoint remains available for existing
commands. It uses the same bootstrap source and current saved release override
and accepts the original two arguments, Control URL and one-time token.

The manual fallback is to copy the matching checksum-verified Agent binary and
the adjacent Release installer/unit to the host, then run:

```bash
sha256sum -c SHA256SUMS --ignore-missing
sudo ./install-agent.sh ./vps-status-agent-linux-amd64 \
  https://monitor.example.com ONE_TIME_ENROLLMENT_TOKEN
systemctl status vps-status-agent
```

Re-running the same three-argument command retains an existing validated
credential and restarts the service with the newly installed binary. For an
upgrade, the binary alone is sufficient:

```bash
sudo ./install-agent.sh ./vps-status-agent-linux-amd64
```

The service has no inbound listener and receives no writable system paths. Its
only persistent secret is `/etc/vps-status/agent.json`, owned by the service
user with mode 0600 in a mode-0700 directory.

Uninstall keeps the credential by default so an accidental removal is
recoverable. `--purge-config` irreversibly removes the credential and account:

```bash
sudo deploy/systemd/uninstall-agent.sh
sudo deploy/systemd/uninstall-agent.sh --purge-config
```

### Credential rotation

1. In the authenticated administrator UI/API, rotate the node credential. The
   old credential is revoked immediately and the new value is shown once.
2. Put only the new value (plus a trailing newline if desired) in a temporary
   mode-0600 file owned by `vps-status-agent` on the node.
3. Stop the service, replace the local value atomically as the service user,
   remove the temporary file, and restart:

```bash
sudo systemctl stop vps-status-agent
sudo install -o vps-status-agent -g vps-status-agent -m 0600 ./new-credential \
  /etc/vps-status/new-credential
sudo -u vps-status-agent /usr/local/bin/vps-status-agent credential replace \
  --credential-file /etc/vps-status/new-credential --config /etc/vps-status/agent.json
sudo rm -f /etc/vps-status/new-credential
sudo systemctl start vps-status-agent
```

Do not pass the replacement credential directly on a command line because it
can be visible in process listings and shell history.

## Agent with Docker

The administrator add-node dialog offers **systemd** and **Docker** after
creating an enrollment. Switching the method reuses the same one-time token
and installation status. Select Docker and copy the whole command onto the
Linux host to monitor. Docker Engine with host network/PID/UTS support is
required; this deployment targets Linux amd64 and arm64. Docker Desktop on
Windows/macOS monitors its Linux VM rather than the desktop host.

The image is hosted on GitHub Container Registry:
`ghcr.io/senhao-xu/vps-status-agent:latest`. GitHub Actions publishes both
architectures with OCI source/version labels. Use an immutable digest or a
published version tag for a pinned deployment; `latest` follows the latest
published build. On first publication, the package owner must set the package visibility to
**Public** in GitHub Package settings before anonymous pulls work. Repository
access inheritance alone does not make a new package public.

The systemd release override in Installation settings does
not select the Docker image tag. Existing address and enrollment-release
eligibility checks still apply to command generation.

The generated command pulls the image, creates a distinct named data volume,
runs enrollment once in a temporary container, and starts a persistent Agent
only if enrollment succeeds. The runtime has no enrollment token in its
arguments or environment. Its node credential lives in `/data/agent.json`
(mode 0600, directory 0700, owned by UID/GID 65532). Preserve this volume
across restarts and upgrades; a lost volume requires a new enrollment. The
copied enrollment command itself contains a short-lived secret and can remain
in shell history.

The Agent runs as a non-root user with a read-only container filesystem, all
capabilities dropped and `no-new-privileges`. It shares the host network,
PID and UTS namespaces and reads these host bind mounts:

| Host source | Container destination | Purpose |
| --- | --- | --- |
| `/proc` | `/host/proc` | CPU, memory, uptime, processes, network, boot ID |
| `/sys` | `/host/sys` | Host hardware/virtualization facts |
| `/` | `/host/root` | Host root filesystem usage; nonrecursive bind |
| `/etc/os-release` | `/host/os-release` | Resolved host OS identity |

Every host mount is read-only. The nonrecursive root bind avoids importing
other host mounts; `/proc` and `/sys` are provided separately. Docker Engine
must support `bind-recursive=disabled` (Engine 25 or newer is recommended).
Host access restrictions such as hidepid or unreadable kernel files can
leave individual metrics unavailable. No Docker socket or privileged mode is
needed. This is host monitoring, not a per-container metrics inventory.

For Compose, use `deploy/docker-compose.agent.yml` (a current Compose version
supporting `bind.recursive: disabled` is required):

```bash
export VPS_STATUS_AGENT_IMAGE=ghcr.io/senhao-xu/vps-status-agent:latest
sudo -E docker compose -f deploy/docker-compose.agent.yml pull agent
sudo -E docker compose -f deploy/docker-compose.agent.yml --profile enroll run --rm enroll \
  enroll --control-url https://monitor.example.com --token ONE_TIME_ENROLLMENT_TOKEN \
  --config /data/agent.json
sudo -E docker compose -f deploy/docker-compose.agent.yml up -d agent
sudo -E docker compose -f deploy/docker-compose.agent.yml logs --tail=50 agent
```

Run `up` only after enrollment succeeds. Choose a distinct Compose project
name (`-p NAME` on every invocation) when registering another Agent on the
same Docker host. Do not re-enroll an already configured volume. For an
upgrade, pull the selected image and recreate only the runtime service:

```bash
sudo -E docker compose -f deploy/docker-compose.agent.yml pull agent
sudo -E docker compose -f deploy/docker-compose.agent.yml up -d --no-deps agent
```

`docker compose down` preserves the credential volume; `down -v` destroys it.
Native binary collection defaults remain `/proc`, `/sys`, `/`, and
`/etc/os-release`. The image supplies `--proc-root`, `--sys-root`, `--rootfs`
and `--os-release` to select the mounted host paths; overrides must be absolute
local paths. The same sources drive hello facts and periodic reports.

## Backup and verified restore

Control configuration and its deployment master key are backed up by default;
telemetry is separate and opt-in because it is high-growth and reconstructable.
The online SQLite snapshots include committed WAL data. Every archive has an
allowlisted manifest and SHA-256 checksums.

```bash
vps-status-control backup create --data-dir /data --output /backups/control.tar.gz
vps-status-control backup create --data-dir /data --output /backups/full.tar.gz --include-telemetry
vps-status-control backup verify --input /backups/control.tar.gz
```

Test restores into a new path that does not yet exist. Verification runs archive path checks,
checksums, SQLite `quick_check`, required-schema checks, and master-key shape
validation before publishing files:

```bash
vps-status-control backup restore --input /backups/control.tar.gz --data-dir /tmp/vps-status-restore
```

For production recovery, stop Control, restore into a new empty directory,
start Control against that directory, confirm `/readyz` and administrator
login, then switch the volume/path. Never merge files from different backups;
`control.db` and `master.key` must remain paired. Telemetry has no cross-file
atomicity guarantee and may be omitted.
