# v0.1.0 publication and local deployment

Date: 2026-10-14. User explicitly authorized publication and local deployment.

## Publication

- Release: https://github.com/senhao-xu/vps-status/releases/tag/v0.1.0
- Annotated tag commit: `4a05543041aebbd13389e1e9ed81f158156dcee3`.
- Release workflow `38533219223` and tag CI `38533219369` passed.
- Downloaded all eight GitHub assets into `/tmp/vps-status-v0.1.0-download`.
  `sha256sum -c SHA256SUMS` passed for all seven covered files.
- Native Control and Agent `--version` match the tag and full commit.
- Downloaded `bootstrap-agent.sh` matches the canonical source byte for byte.
- Release notes document features, installation documentation and outstanding
  security/capacity evidence. No capacity promise was added.

## Deployment

- Container: `deploy-control-1`, Compose project: `deploy`.
- Image: `vps-status-control:v0.1.0`.
- Original volume retained: `deploy_control-data`; host port retained:
  `127.0.0.1:18080` -> container `8080`.
- Ignored local `deploy/.env` records release version, tag commit and port.
  Routine management command:

  ```sh
  docker compose --env-file deploy/.env -p deploy -f deploy/docker-compose.yml ps
  ```

- Dockerfile build and Compose configuration validation passed. Only Control
  was recreated, with `--no-deps`; existing panel/Agent and other containers
  remain running.
- Docker health `healthy`; `/healthz` and `/readyz` return `ok`.
- Binary metadata is `version=v0.1.0`, `commit=4a05543041aebbd13389e1e9ed81f158156dcee3`.
- Compatibility `/install-agent.sh` returns GitHub download defaults with
  `default_release_version='v0.1.0'`; shell syntax check passed.
- `/api/v1/admin/setup` remains `configured=true`.
- Real Chromium checks passed for public root and administrator login pages,
  at desktop and 320px, without horizontal overflow or JavaScript errors.
  Screenshots: `/tmp/vps-status-v0.1.0-{public,admin}-{1280,320}.png`.

## Data and rollback

- Before replacement, created and verified an online backup containing
  `control.db`, paired `master.key`, and `telemetry.db`:
  `/data/control-pre-v0.1.0-20261014.tar.gz` in the retained volume.
- Host copy: `data/backups/control-pre-v0.1.0-20261014.tar.gz`, mode 0600.
- Old image retained as `vps-status-control:pre-v0.1.0-ab20759`.
- Post-upgrade verified snapshot:
  `data/backups/control-post-v0.1.0-20261014.tar.gz`, mode 0600.
- Read-only comparisons of snapshot databases confirm schema 8 -> 9; every
  pre-existing table other than migration metadata has identical records,
  including the administrator, sessions, appearance and public-page settings.
- New installation defaults are empty strings; the runtime release default is
  v0.1.0. No remote HTTPS Control address was invented for localhost access.
- Rollback must stop the new Control, restore the **pre-upgrade** archive using
  the backup CLI into a new, nonexistent data directory, and cut over using the
  retained old image and restored data. The old binary cannot use the v9 DB;
  merely switching the image is insufficient. Keep backups private.

## Outstanding security and capacity evidence

- Initial Security workflow could not resolve `trivy-action@0.28.0`.
  `4a05543` corrected the tag prefix, revealing the old action's own missing
  `setup-trivy@v0.2.1` dependency.
- `fc7e7bd` pins official Trivy action v0.36.0 by its full commit, with a valid
  pinned setup dependency. This CI-only follow-up does not change v0.1.0 code
  or deployed binary metadata. Its functional CI `38533439614` passed.
- Security run https://github.com/senhao-xu/vps-status/actions/runs/38533439699
  now executes but **fails**: govulncheck reports 45 reachable Go standard
  library advisories with its Go 1.24 toolchain; the production image scan
  reports 36 findings (35 HIGH, 1 CRITICAL). These are scan findings, not a
  verified exploit assessment. Some latest fixes require Go 1.26.9 or newer.
  Raw logs: `/tmp/vps-status-v0.1.0-security.log`.
- The dependency job stops at govulncheck, so its later web audit/filesystem
  steps did not execute. Do not report the security gate as passed.
- Runtime/toolchain upgrades require their own compatibility verification;
  they were not silently bundled into this deployment.
- Required 100-Agent / 400-TCP-task baseline under external 2 vCPU / 2 GiB
  constraints remains open. Parent task remains `in_progress`.
