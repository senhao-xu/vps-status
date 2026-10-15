# Public homepage positioning — competitor research

Date:2026-10-15. User requests reducing emphasis on public-page mechanics because public presentation is an intended core function, then asks to inspect competitors. Research only; no live publication policy changed by this step.

## Verified primary evidence

- monitor c532d54843b87f8001f8387a7acdf1d440b23f40: src/main.rs public_page() is enabled unless explicitly off; src/db.rs node.public defaults1. web-admin/src/components/Admin.tsx places the site-level open-public-status-page toggle in site settings, and per-node public-display switch in node editing. Normal table rows only badge nodes when not public. There is no separate administrator publication-allowlist page.
  Sources: https://github.com/monitor-probe/monitor/blob/c532d54843b87f8001f8387a7acdf1d440b23f40/src/main.rs and src/db.rs; same commit web-admin/src/components/Admin.tsx.
- Komari official internal/config/settings.go: PrivateSite defaultfalse; public-home usage is default and private-site access is the optional mode. Site title/description/theme are ordinary settings. Source: https://github.com/komari-monitor/komari/blob/main/internal/config/settings.go . Frontend checkout was interrupted after network stall; no unverified claims about exact current frontend layout are made.

## Current project gap

PublicConfigPanel separately configures enablement, published-node selection and draft preview; copy emphasizes nodes-private-until-selected and DTO/allowlist implementation details. Current backend defaults disabled and an empty publication allowlist, unlike competitor defaults. The homepage is already the root route but the setup model presents it as an optional publication feature.

## Suggested direction (not yet accepted implementation details)

Treat root as the ordinary monitoring homepage; rename publication section to site settings (name/description/presentation). Put a concise homepage-display toggle on node editing instead of a dedicated publication wizard. Put site access/login-required control in access/security or secondary settings. Remove repetitive public/private/allowlist/DTO copy from product UI; retain server-side data projection independently. Consider default-open homepage/default-visible newly added nodes to match monitor; existing publication choices need an explicit migration policy before implementation. This is a behavior/default decision as well as wording, not evidence to silently change live visibility during research.

## Accepted adaptation

User subsequently approved implementation with “实现吧”. Latest PRD/design sections specify open fresh/never-configured sites, preserve explicitly saved access and all prior node membership, default-visible future enrollment, per-node homepage display and optional login restriction. Existing safe public DTO remains the data boundary in both access modes.
