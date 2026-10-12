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

Copy the matching checksum-verified Agent binary and this directory to the
host, then run the installer as root. Root is needed only to install files and
the service account; runtime uses the locked `vps-status-agent` system user.

```bash
sha256sum -c SHA256SUMS --ignore-missing
sudo deploy/systemd/install-agent.sh ./vps-status-agent-linux-amd64 \
  https://monitor.example.com ONE_TIME_ENROLLMENT_TOKEN
systemctl status vps-status-agent
```

Re-running the same three-argument command retains an existing validated
credential and restarts the service with the newly installed binary. For an
upgrade, the binary alone is sufficient:

```bash
sudo deploy/systemd/install-agent.sh ./vps-status-agent-linux-amd64
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
