# Homepage/site settings verification

Date: 2026-10-15. User approved monitor-style homepage/site settings with “实现吧”. Research: research/public-page-positioning.md. This increment retains latency detail dialogs and monitor-style TCP configuration.

## Resulting behavior

“Public page” becomes Site settings: title, description, and secondary Access requires login. Site saves send only enabled/title/description. Node settings own independent Homepage display; there is no separate publication picker or draft preview. Homepage/navigation/footer use neutral copy in zh/en.

Schema11 only enables never-configured settings (updated_at=0). Saved access choices and every prior node membership stay unchanged. Future enrollment joins homepage membership in the same node/credential/auto-join transaction, bounded at1000 displayed nodes; registration at capacity succeeds without display. Manual enable beyond capacity fails409 homepage_node_limit and rolls back metadata too.

Node PATCH merges optional fields inside storage; omitted site node_ids preserves current membership in its transaction. Explicit legacy node_ids remains a complete replacement. Restricted status reuses existing administrator session authentication; successful access returns the identical strict status DTO with effective enabled=true. Anonymous/invalid/expired/revoked sessions cannot read restricted title/nodes. Login uses fixed /admin?next=home and returns to / without mounting private dashboard/SSE; arbitrary next URLs are ignored.

## Verification

- Frontend lint/typecheck/223 tests/production build pass. Independent trellis-check re-ran lint/typecheck/tests and found no remaining functional, data boundary or concurrency issues. Its documentation finding was fixed in README and specs.
- Go full tests, full race tests, vet, tagged webui full tests and tagged Control build pass. Tests cover schema10 immutable mode0600 backup, untouched/explicit access cases, membership retention, cap/transaction failures, gated request-body concurrent site/node writes, strict DTO allowlist in both access modes, expired/revoked/invalid authentication and internal-error redaction.
- git diff --check passes. No dependencies or Agent protocol changes. Agent published installation version remains v0.1.0.

## Isolated real backend/browser

/tmp/vps-homepage-browser/control-schema10 was copied from the current running Control. A separate HTTPS proxy/server and data directory seed a new test-only administrator, three nodes, explicit site title/access and one visible node; another node is administrator-hidden. Supported CLI creates/verifies schema10.tar.gz and restores into the test directory before migration.

Snapshot comparison after10->11 confirms all non-migration table rows are byte-for-byte logically identical, including saved site values and all memberships. Separate actual schema10 never-configured startup and fresh schema11 startup both return open empty status.

check.mjs reports ALL HOMEPAGE BROWSER CHECKS PASSED:

1. Site form emits scalar-only PUT; failure retains draft, pending disables editing and duplicate submit. A real new enrollment and per-node display change during the held site save survive its completion.
2. Real node form updates Homepage display independently from administrator hiding. Administrator-hidden nodes can appear on homepage. Extra browser checks verify failed draft/retry, one pending write, persisted update and401 login exit.
3. Real restriction returns401 anonymously and identical exact safe DTO with a session; CTA login and existing-session return both reach /. No node snapshot/private live request is made before returning home. Arbitrary external next destination does not redirect.
4.48 site/node/homepage screens across16 combinations of zh/en, Emerald/Classic, light/dark and1280/320 pass overflow checks. Homepage makes no admin requests; zero page errors. Screenshots site-zh-{1280,320}.png and homepage-zh-{1280,320}.png were inspected. Temporary scripts/logs are under /tmp/vps-homepage-browser.

## Local deployment preparation

Supported live schema10 pre-update backup verified: data/backups/control-pre-homepage-20261015.tar.gz (0600, paired key and telemetry). Original env: data/backups/deploy-pre-homepage-20261015.env (0600). Prior image retained as vps-status-control:pre-homepage-20261015. Live data has zero nodes and never-configured site settings, so intended migration opens the empty homepage only.

Schema11 cannot be opened by the old schema10 binary; rollback requires verified pre-update backup restored into a fresh data directory/volume with paired key and intended telemetry, prior image and environment. Never image-only downgrade.

## Completed local Docker update

- First Dockerfile frontend resolution timed out at Docker Hub; unchanged retry succeeded through the normal two-file Compose build. Final image vps-status-control:local-homepage-b46faf8acb; binary version=dev, commit=927ce78193dae18ef36ac93e098bb0477e4fbaa8-dirty-b46faf8acb, protocol=v1. Runtime source fingerprint rechecked before activation.
- Only deploy-control-1 recreated with --no-deps/--wait. Docker health, healthz and readyz pass. Original deploy_control-data:/data and127.0.0.1:18080 retained; Agent install version stays v0.1.0; other containers unchanged.
- Supported post-update backup verified at data/backups/control-post-homepage-20261015.tar.gz (0600). Pre/post comparison confirms schema10->11, identical paired master key, and20 configuration tables preserved except the intended never-configured public_page.enabled0->1; updated_at, title, description and all other rows remain unchanged. SQLite quick_check passes.
- Served index-Cqc5SmdB.js and index-jR5IFICY.css equal the verified build byte-for-byte. Deployed Chinese homepage and administrator-login browser smoke passes1280/320 with no private homepage requests, page errors or overflow. Existing administrator retained; smoke does not log in or mutate real credentials.
- Local service http://localhost:18080 is healthy. Isolated test servers stopped after verification. Source remains uncommitted; workflow commit approval/journal/archive remain pending and parent task stays active for unrelated security/capacity acceptance work.

## Screenshot follow-up: homepage link button (2026-10-15)

User screenshot identifies the administrator top-right Homepage ↗ link. AdminDashboard.tsx adds homepage-button; styles.css shares existing button padding/background/border/hover tokens, removes underline and keeps native link/new-tab/noreferrer semantics plus focus-visible. No API/data changes or new tests. Independent check clean; lint/typecheck/223 tests/build, tagged Go tests/build pass. Isolated browser16 language/theme/mode/viewport combinations verify border/padding/height, no underline/overflow, keyboard focus, hover and real new-tab homepage destination. Screenshot inspected.

Updated only deploy-control-1 to local-home-button-3c3ba5ecab (dev / HEAD-dirty-3c3ba5ecab), preserving volume,18080 and Agent v0.1.0. Verified0600 pre/post-home-button backups, all control tables/schema11 and key identical; served index-Bhvdw88w.js/index-CQ15YFdW.css match build; health and deployed1280/320 public/login smoke pass. Source remains uncommitted.

### Homepage button label without arrow

User requested removing ↗; both translations now read Homepage / 首页. Button styling, / target, new-tab behavior remain. Independent review, lint/typecheck,223 frontend tests/build, tagged Go tests/build pass. First Docker build failed downloading SQLite dependency archives; unchanged retry succeeded. Updated only Control to local-home-label-01d948f0a3. Health/ready and served index-uWEBtIA9.js/index-CQ15YFdW.css verified against build; deployed bundle contains both arrow-free labels. Verified pre/post-home-label backups preserve every configuration table/schema11 and master key. Source remains uncommitted.

### Remove login-page homepage link

Removed only the trailing Open homepage / 打开首页 link from LoginForm in AuthGate.tsx. Other homepage links and authentication/return-home flows unchanged. Independent review clean; lint/typecheck/223 tests/build and tagged Go tests/build pass. Deployed only Control image local-login-home-ceb61c25ca. Real deployed zh/en login forms at1280/320 have no homepage link, retain fields and show no overflow/page errors. Health/ready and served index-pbET2hYQ.js/index-CQ15YFdW.css match build. Verified pre/post-login-home backups preserve every control table/schema11 and master key. Source remains uncommitted.

### Source submission

User explicitly authorized committing and pushing this complete increment on2026-10-15. All listed source/spec/research/evidence changes belong to this conversation; no runtime data, secrets, ignored deployment settings or generated assets are staged. The local image continues to identify the previously verified dirty snapshot; this submission does not rebuild or change running services. Parent task remains active for outstanding security/capacity work.
