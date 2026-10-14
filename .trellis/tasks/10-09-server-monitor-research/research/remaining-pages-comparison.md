# Remaining page comparison — 2026-10-14

Reference snapshots: Komari web 9d2c144fcbde53858e175ecd70287fa6428048ef; monitor c532d54843b87f8001f8387a7acdf1d440b23f40. Local sources are read-only.

- Komari `src/pages/admin/notification/channels.tsx`: selected provider controls its configuration fields; settings cards separate labels, explanation and controls. Adopt conditional Webhook/Telegram fields, readable channel/rule management, empty states.
- Komari `src/pages/admin/settings/site.tsx`: distinct cards for site identity and publication/configuration; monitor `web-admin/src/components/Admin.tsx` site settings around 2820–2900 uses field hints and a save footer. Adopt publication identity, searchable/grouped allowlist, selected count and a safe draft preview using public-approved fields only.
- Komari `src/pages/admin/account.tsx`: account and 2FA are separate sections with explicit status and setup/disable steps. Adopt security overview, coherent password/MFA cards, inline pending/error feedback and memory-only secrets.
- monitor Admin notification section around 3020–3180: channel-specific options and named operation feedback. Avoid showing unrelated secret fields; scope selection uses node display names/group names rather than requiring raw IDs.
- monitor Admin backup section around 3368–3505: maintenance and destructive restore are visually separate. Adopt storage summary, routine maintenance cards and distinct destructive confirmations. Existing exact `clear_history`/`reset_all_telemetry` contracts remain.
- Both references use compact headers, cards and clear form actions. Apply a shared settings page rhythm to appearance/install/security, shell and independently rendered auth/public pages without new libraries or backend endpoints.

Current gaps: four large inline panels in AdminDashboard; alert forms dominate their lists, all provider secret fields visible; public allowlist lacks discovery/count/preview; storage combines statistics, maintenance and destructive controls; MFA errors use blocking alerts. Fix within existing API contracts.
