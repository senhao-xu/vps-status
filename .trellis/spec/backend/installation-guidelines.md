# Shared Installation Settings

## 1. Scope / Trigger

Use when changing administrator installation defaults, enrollment address
selection, release eligibility, or the name-only add-node flow. The canonical
bootstrap/release packaging contract lives in [Operations](./operations-guidelines.md).

## 2. Signatures

- Schema v9: singleton `installation(singleton_id, control_url, agent_release_version)`.
- Store: `InstallationConfig(ctx)` and `UpdateInstallation(ctx, Installation)`.
- Authenticated `GET/PUT /api/v1/admin/installation`; PUT requires CSRF.
- PUT fields: `control_url` and `agent_release_version` as strings; blank restores defaults.
- Response fields: saved `control_url`, saved `agent_release_version`,
  `effective_agent_release_version`, startup `agent_release_base_url`, and
  boolean `release_configured`. No public installation-config endpoint.

## 3. Contracts

- Save only low-frequency defaults in `control.db`. No token, credential,
  host report, traffic counter or probe result belongs in this singleton.
- Saved version overrides startup `--agent-release-version`, which defaults
  to the build version. The release base remains a validated startup setting.
- Saved URLs require non-loopback HTTPS, no userinfo/query/fragment, and a
  maximum of 2048 bytes. Localhost (including subdomains), unspecified and
  loopback IPs cannot serve as shared remote installation defaults. Private
  network HTTPS is supported; validation does not resolve DNS.
- Explicit versions are bounded safe path segments. `dev` and `unknown` are
  development placeholders, not published-version overrides. The effective
  startup placeholder makes `release_configured` false.
- Address priority for enrollment: saved address, explicit `control_url`, then
  validated non-loopback HTTPS request Origin. Explicit old-client loopback
  input remains supported for local development. Do not reconstruct public
  addresses from an internal proxy Host or forwarded scheme.
- Resolve settings and reject a missing release before creating any token.
- `release_configured` does not check remote asset availability. A Release
  must contain the standalone bootstrap and all matching checksum-covered
  assets; changing a URL does not publish missing files.
- Schema migration makes a mode-0600 pre-upgrade backup. Old binaries reject
  v9 databases; reverting the binary requires restoring its matching backup.

## 4. Validation & Error Matrix

| Condition | Result |
| --- | --- |
| Unauthenticated config read/write | `401 unauthorized` |
| Missing CSRF for config write | Existing `403` CSRF error |
| Invalid saved address/version or extra fields | `400 invalid_input` |
| Enrollment effective version is dev/unknown | `409 agent_release_required`, no new token |
| Missing address and invalid/missing remote Origin | `400 control_url_required`, no new token |
| Store failure | `500 internal_error`, no raw database/path details |
| Valid version format but missing Release asset | Installation download fails; never claim config GET verified publication |

## 5. Good / Base / Bad Cases

- Good: a localhost admin opens settings and saves the node-reachable HTTPS
  entry and a published version; future nodes reuse them after restart.
- Base: a tagged deployment with blank saved defaults uses the browser HTTPS
  entry and its build version automatically.
- Bad: a source build generates `releases/download/dev`; reject before minting
  a token and show settings guidance.
- Bad: the user downloads the systemd installer as though it were a network
  bootstrap. That installer requires a local binary; use `bootstrap-agent.sh`.

## 6. Tests Required

- Store defaults, invalid inputs, update/reopen persistence, and v8→v9 backup
  lacking the new table while retaining preexisting appearance/admin settings.
- API authentication, CSRF, exact DTO keys, saved/default priority, invalid
  settings/Origin, placeholder eligibility and unchanged token count on guard failure.
- Frontend strict decoder, address resolution, name-only form/settings link,
  canceled/stale config requests and preservation of enrollment terminal/race behavior.
- Browser: localhost guidance, save settings, direct GitHub command, blank
  address automatic HTTPS origin, keyboard and 320px theme layouts.

## 7. Wrong vs Correct

Wrong: show an install command using `window.location.origin` even when that
origin is localhost, or interpret a nonempty version as proof of a published Release.

Correct: prefer saved remote defaults, reject automatic local/non-HTTPS
entries in the UI, and show eligible version/source separately from publication.
