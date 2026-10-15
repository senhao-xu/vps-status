# 服务器探针技术设计

## 1. Architecture

```text
Linux Agent (Go, non-root)
  ├─ collect /proc, /sys, statfs
  ├─ bounded TCP connect-only scheduler
  └─ WSS + versioned JSON messages
             ↓
Control (Go)
  ├─ connection registry + in-memory live state
  ├─ minute aggregators + alert evaluator
  ├─ REST admin/public APIs + browser SSE
  ├─ control.db      configuration and identities
  ├─ telemetry.db    rollups, probes, traffic ledger
  └─ embedded Web app (TypeScript/React/Vite)
             ↓
       Webhook / Telegram
```

Control 是单进程、单机服务。不引入 Redis、外部消息队列或独立时序库。Agent 只主动出站连接，不监听网络端口。

## 2. Repository boundaries

```text
cmd/control/             Control 入口和本机管理 CLI
cmd/agent/               Agent 入口
internal/control/        API、连接、告警、任务编排
internal/agent/          采集器、TCP 调度器、重连
internal/protocol/       版本化 Agent 消息契约
internal/storage/control/
internal/storage/telemetry/
internal/security/       token、密码、TOTP、session
migrations/control/      内嵌 control.db 迁移
migrations/telemetry/    内嵌 telemetry.db 迁移
api/                     HTTP API 契约
web/                     TypeScript/React/Vite Web 端
deploy/                  systemd、Docker Compose、反向代理示例
tests/                   集成、协议、负载和故障注入
```

Go 使用单 module；Web 产物在发布构建时嵌入 Control。SQLite 优先采用纯 Go driver，以保持 Linux amd64/arm64 交叉编译和单文件发布。

## 3. Agent protocol

### Connection and authentication

- Agent 连接 `/agent/v1/ws`，凭据放在 `Authorization` header，不放入 URL/query 或普通日志。
- 注册密钥和节点凭据使用至少 256-bit CSPRNG 随机值；Control 只保存凭据摘要。
- 每个连接有 server nonce、session ID 和单调消息序号；重复/倒退序号被拒绝，防止已验证会话内的帧重放。TLS 负责链路保密和完整性。
- 凭据轮换创建新凭据并立即吊销旧凭据；Control 断开使用旧凭据的现有连接。

### Message surface

MVP 只包含：

- `hello`: 协议/Agent 版本和静态主机信息。
- `report`: 动态主机指标，默认 3 秒。
- `probe.sync`: Control 下发完整的受限 TCP 任务快照及版本号。
- `probe.result`: 任务 ID、调度时间、成功/失败、延迟和有界错误类别。
- `ping/pong` 与有界 `error` 帧。

协议不定义 shell、terminal、file、process execution 或通用 RPC 消息。每帧有大小上限；读写队列有界，慢连接不能无限堆积内存。协议版本不兼容时，Control 返回可观测的拒绝原因，不默认解析未知能力。

## 4. Runtime data flow

1. `report` 通过身份、序号、频率、大小和数值边界校验。
2. 最新报告替换节点的内存快照，并通过 SSE 推送给已授权管理页和字段白名单后的公开页。
3. 同一报告进入当前 1 分钟内存聚合桶，更新 sum/count/min/max/last 和网络计数器差值。
4. 完整分钟桶进入有界写队列，由 telemetry 单写者批量 UPSERT。重启时最多丢失当前未完整分钟，不回放秒级原始值。
5. 告警评估使用已校验的内存快照与持续时间状态；历史库不可写时，实时告警不停止。

TCP 结果使用相同的分钟边界，记录 attempts/success/failure 和成功样本的 min/max/avg latency。

## 5. TCP scheduler safety

### TCP administrator page refinement

The authenticated TCP page uses existing task CRUD and telemetry APIs, with
task/node views, compact metadata rows and a native add/edit dialog. History
selection is explicit by task ID, assigned node ID and retention-compatible
range/resolution; no selection by display name or implicit first-node claim.
The existing AdminDashboard snapshot owner reloads after mutations and manual
refresh. New feature files own local editor/search/history state and cancel
requests when their selection changes. Missing samples remain unknown, and
charts retain an accessible table. Reference evidence and exact change scope:
`research/tcp-page-comparison.md`. This UI increment adds no probe protocol,
backend schema or network actions.

Longer history uses one fixed query clock and all non-overlapping retained
tiers through existing endpoints. Windows align with retention's ceil-to-10m
and ceil-to-hour boundaries and are adjacent half-open intervals; any failed
tier rejects the entire history. TCP charts place samples by timestamp.
The existing server `updated_at` field is strictly decoded; because the
database stores it in seconds, latest samples from before or during the
configuration-update second are treated as waiting until a later sample.

- Control 在一个事务内验证每节点最多 64 个任务与 5–3600 秒间隔；Agent 再次独立校验并拒绝/截断超界快照。
- 目标支持 DNS、IPv4 和 IPv6；允许私网与回环，禁止 unspecified、multicast 和端口 0。DNS 解析后再校验地址类型。
- 每次最多取 3 个解析地址，串行尝试；每个连接上限 1 秒，整个检查上限 3 秒，首个成功即结束，不做额外重试。
- 每任务只允许一个 in-flight 检查，错过 tick 直接跳过；Agent 另有全局探测 semaphore，防止 64 个任务同时涌入。
- TCP 成功后立即关闭 socket，无 `Write`/`Read`。Control 对每连接的 `probe.result` 做与任务数/间隔对应的结果速率限制。

## 6. Persistence design

### control.db

使用独立迁移版本，关键表为：

- `admins`, `sessions`, `recovery_codes`
- `nodes`, `node_credentials`, `enrollment_tokens`
- `probe_tasks`, `probe_assignments`
- `alert_rules`, `alert_states`, `notification_channels`
- `public_page`, `public_nodes`, `settings`

会话、恢复码和一次性注册密钥都有过期/消费机制，使配置库保持有界。配置库不接收 Agent 样本或探测结果。

### telemetry.db

使用针对固定指标的紧凑宽表，避免高基数 EAV/JSON 行：

- `host_1m`, `host_10m`, `host_1h`
- `probe_1m`, `probe_10m`, `probe_1h`
- `traffic_ledger` （node + accounting period 紧凑累计行）
- 遥测库自身的 schema/maintenance metadata

主键以 `(dimension_id, bucket_start)` 开头，时间查询和过期删除共用索引前缀。对完整时间窗做幂等聚合：1m → 10m → 1h；先 UPSERT 粗粒度，后分批删除已过期细粒度，不因清理任务落后丢失可聚合数据。

SQLite 创建时启用 WAL、`synchronous=NORMAL`、busy timeout 与 `auto_vacuum=INCREMENTAL`；两库分别设置 `wal_autocheckpoint` 和 `journal_size_limit`。大批清理后执行受控 checkpoint/incremental vacuum；完整 `VACUUM` 只在管理员显式执行且剩余磁盘足够时进行。

### Disk guard and recovery

- 定期检查 `telemetry.db` + WAL/SHM 总体积与数据目录剩余空间；文件预算和最小剩余空间可配置。
- 达到阈值后丢弃待持久历史桶，不让队列在内存无限增长；用滞回阈值恢复写入。
- 实时快照、配置、身份验证和实时告警不依赖 telemetry writer。界面和系统日志显示降级原因。
- 恢复时从新的完整分钟开始写入，不伪造缺口数据。

## 7. HTTP, browser updates, and public boundary

- 修改密码：`POST /api/v1/admin/password` 需要 session + CSRF，JSON 包含 `current_password`、`new_password`（两个字段必须存在，可为空字符串），成功返回 `204` 并清除 cookie。当前密码错误返回 `400 current_password_invalid`，沿用登录限速器但使用独立 `password:<adminID>` key，超限返回 `429 rate_limited`。存储通过条件更新匹配已验证的旧摘要、当前有效 session 与 MFA 状态，在同一事务中更新 Argon2id 摘要及撤销全部会话；认证状态过期返回 `409 auth_state_changed`。登录发放 session 时同时匹配认证时的密码摘要与 MFA 状态，阻止旧密码并发登录在改密后创建有效会话。无需数据库迁移。
- 安全模块新增独立密码表单，不复用后台数据刷新 mutation，因为成功必须退出登录。新密码/确认密码一致才发起请求，处理中禁止重复提交；密码只在表单和请求内存中存在，成功/卸载清除，不能写入偏好存储。成功回到登录页，展示可随语言切换的成功提示；会话 `401` 走原认证失效处理。不增加 CLI 重置密码能力。
- Web 根路径 `/` 与兼容地址 `/public` 渲染独立公开页；只有 `/admin` 与 `/admin/` 子路径挂载 `AuthGate` 和管理界面。公开页右上角始终提供 `/admin` 链接，覆盖加载、错误、关闭和成功状态；登录页与后台公开页链接统一返回 `/`。未知非管理路径不进入认证边界。Go 静态资源 handler 已支持 SPA 路由回退，无需改变 API、公开白名单或数据库默认值。
- 管理 API 使用 JSON REST，有服务端 session、HttpOnly/Secure/SameSite cookie 和 CSRF 防护。每个管理员最多保留 32 个活跃会话，创建新会话时清理失效行并淘汰最旧的超限活跃会话；会话创建事务先取得写锁并验证认证所依据的 MFA 状态，避免与 TOTP 启用并发时产生绕过。TOTP 确认事务保留当前会话并吊销其他会话。浏览器实时更新使用 SSE，不与 Agent WSS 协议复用。
- 公开 API 与管理 DTO 使用不同类型/查询，由服务端显式构造白名单字段，禁止直接序列化内部 node model。
- 管理 API 对登录、注册密钥、凭据轮换、任务配置和破坏性遥测操作使用独立限速/确认边界。

## 8. Alerts

- 规则定义包含节点/分组范围、指标、阈值、持续时间、恢复滞回和通知渠道。
- 状态机为 `normal → pending → firing → resolved`；只在转移时通知，可选有界 reminder，避免每次上报都发送。
- Webhook 和 Telegram 通过有界队列、超时和指数退避重试；日志脱敏 URL token、Authorization 和 Telegram bot token。
- 通知配置敏感值使用部署级 master key 加密，不在 API 中读回完整明文。

## 9. Compatibility and migrations

- HTTP API 与 Agent 协议从 `v1` 开始；后续只在明确的版本协商内增加字段。Control 在连接时记录 Agent 版本和兼容性状态。
- `control.db` 与 `telemetry.db` 有各自的单调 schema version；先备份、后迁移。任一库的迁移不可通过 `ATTACH` 制造跨库原子依赖。
- 默认备份只包含配置库和部署密钥；遥测备份为可选的独立产物，两者不承诺同一时刻的跨库快照。

## 10. Operational and rollback design

- Control 启动顺序：验证配置 → 打开/迁移 control.db → 尝试打开 telemetry.db → 启动 API/WSS → 启动后台任务。遥测库失败不得阻止配置面启动，但必须显著告警。
- 发布前备份 control.db；遥测 schema 升级失败时保留原文件并以历史降级方式启动。
- Agent 采用 systemd 重启策略和有抖动的指数重连；MVP 不自动更换二进制。

## 11. Key trade-offs and risks

- JSON/WSS 比二进制协议占用更多带宽，但在 100 节点/3 秒基线下可控，且更易调试与版本化。
- 两个 SQLite 文件隔离了增长与故障域，代价是无跨库原子事务；设计通过幂等清理和稳定 ID 接受最终一致。
- 允许私网/回环提高实用性，但存在低速端口探测风险；任务数、间隔、DNS、并发、超时和结果速率均需双端硬限制。
- 内存聚合使原始数据保持小体积，代价是 Control 崩溃可丢失不到一分钟的未完整历史；实时监控不承诺秒级持久性。
- 纯 Go SQLite driver 降低发布复杂度，但需通过 100/400 压测验证写入、查询和 vacuum 行为；如不达标，先优化模型/批处理，不在 MVP 中仓促增加多数据库后端。

## 12. Emerald UI and node information architecture

`/root/komari-theme-emerald` 当前本地工作树仅作为 MIT 许可的只读设计参考（HEAD `c2c5e88`，另有用户未提交改动）。实现继续使用现有 React、TypeScript 和普通 CSS，不修改或依赖该工作树，也不把 Vue、Tailwind、reka-ui、3D 地球或远程资源加入构建依赖。

视觉 token 以 Emerald 的特征重新定义现有变量：

- 亮色以中性白/浅灰表面配合 emerald/lime 柔光背景，暗色使用低饱和 slate 表面和较弱绿色光晕。
- 卡片采用紧凑间距、小圆角、轻边框/阴影和可选半透明表面；交互强调色统一为 emerald，错误和离线仍使用红色。
- 在线状态同时使用圆点、文本和适度动效；所有动效在 `prefers-reduced-motion` 下关闭。
- CPU、内存和磁盘使用带文字数值的细进度条；网络速率和运行时间保持可扫描的成对信息。
- Emerald 是初始站点默认主题；内置 Classic 复用现有色板和表面风格。两个视觉主题与亮/暗模式正交，明暗模式首次访问跟随系统偏好并复用现有版本化 localStorage 契约。

页面边界：

- 管理节点总览：汇总计数、分组筛选、搜索、卡片/列表切换、节点卡片和“添加节点”主操作。
- 管理节点详情：状态/最近上报、资源指标、主机身份、网络/月流量、历史图表和管理操作。敏感主机事实仍只在管理员页面出现。
- 公开状态页：使用相同节点卡片语言，但只消费既有公开 DTO；不增加真实主机名、OS/kernel、IP 或历史接口。
- 其他管理/认证页面：仅复用全局 token 和基础控件样式，不在本里程碑重排业务结构。

可复用 React 组件按现有目录边界拆分为主题背景/页面头部、状态徽标、资源进度条、节点卡片/列表、节点详情区块和 enrollment 对话框；展示组件接收经过 decoder 验证的类型，不在 JSX 中解析原始 API payload。

## 13. Enrollment interaction and status contract

节点接入状态的数据流为：

```text
Add-node dialog
  → POST /api/v1/admin/enrollments
  → one-time install/enroll commands + enrollment id/expiry
  → Agent downloads, verifies, installs and consumes token
  → enrollment_tokens.consumed_node_id
  → GET /api/v1/admin/enrollments/{id}
  → pending | registered | online | expired
  → dialog feedback + node collection refresh
```

- `POST` 仍是唯一返回令牌明文和安装命令的边界；关闭对话框后无法读回旧命令。
- 新增管理员状态读取端点只返回 enrollment ID、节点显示名称、过期时间、稳定状态枚举，以及消费后才出现的 node ID；绝不返回 token digest、token 明文或节点凭据。
- `pending` 表示未消费且未过期；`registered` 表示已消费并创建节点但当前不在线；`online` 结合 `LiveStore` 的节点快照计算；`expired` 表示未消费且过期。状态计算由服务端拥有，前端不根据本地时间或节点名称推断。
- 对话框打开且命令已生成时以有界轮询刷新状态；到达 `online`/`expired`、关闭对话框或组件卸载时停止。进入 `online` 后刷新节点集合并聚焦新节点。
- `DELETE /api/v1/admin/enrollments/{id}` 只撤销尚未消费的 enrollment；“重新生成”先撤销仍 pending 的旧记录，再创建新记录。并发消费时以服务端最终状态为准，不误删已创建节点。
- 已消费 enrollment 至少保留到原过期时间，支持短期状态查询；创建时继续清理过期记录，活跃 token 上限保持 256。消费记录增长与实际新建节点同阶且受最长 24 小时 TTL 限定。
- 安装命令改为指定版本 Release 的独立 `bootstrap-agent.sh` 入口；固定 `/install-agent.sh` 作为兼容入口继续提供。TLS 限制、版本固定、SHA-256 校验、非 root systemd 账户、0600 配置和一次性 token 保持。

### 2026-10-14: unified installation and layout

用户在审阅节点接入对比与建议后明确要求“实现吧，还有一些页面的排版也参考下”。本次增量范围是节点接入链路、节点总览、安装对话框及新安装设置页；参考证据见 `research/node-installation-comparison.md`。

- 新增 control.db singleton `installation`（接入地址、可选 Agent 版本）；以既有升级前备份和 schema 版本校验迁移，不持久化遥测或令牌。
- 管理员 `GET/PUT /api/v1/admin/installation`：GET 返回 `control_url`, `agent_release_version`（保存值），`effective_agent_release_version`（保存覆盖或启动配置），`agent_release_base_url`（启动配置），`release_configured`（有效版本不是 dev/unknown）。PUT 只接受前两个字段，需认证与 CSRF。空值表示自动地址或默认版本。配置的接入地址要求非回环 HTTPS；版本为安全路径段。
- Enrollment 的 `control_url` 字段保留给旧客户端/显式本机开发；已保存的统一地址优先，空字段时回退经过验证的请求 Origin。新 UI 自动传当前页面地址，只允许有效的非回环 HTTPS 地址生成远程安装命令，不能依赖内部 Host/代理头来推导公网入口。缺少可用发布版本在创建 token 之前返回 `409 agent_release_required`。
- 新增独立静态 `bootstrap-agent.sh` 并随 Release 发布/校验；脚本接收 Control URL、一次性 token、明确版本和 HTTPS release base，架构检测与逐文件校验保持。Go 从同一脚本模板嵌入生成兼容面板入口，避免两份下载逻辑漂移。
- 新的安装设置页使用上述 DTO；添加节点对话框加载设置，地址自动带入并以文字显示，配置缺失时提供前往安装设置的操作。秘密仅保留在对话框内存。
- 节点总览统一工具栏、汇总、筛选和空状态；安装设置采用分区卡片与独立保存栏；排版复用现有 CSS token，320px 下单列，正常键盘/状态/错误行为保留。
- 不执行真实节点安装，不修改真实站点配置、凭据或密码；GitHub Release 的新脚本需随包含本次改动的版本发布后才可实际下载。旧版本目录不会自动获得新资产。

## 14. Built-in theme management

主题管理是内置视觉主题注册表与站点配置，不执行用户上传内容。注册表只包含 `emerald` 与 `classic`，复用相同页面组件和数据边界。DOM 使用独立的视觉主题属性与明暗属性，使现有 `data-theme="light|dark"` 和 `vps-status.theme` v1 明暗偏好保持兼容。

- 增加独立 singleton appearance 配置及 control.db 加法迁移，初始 `default_theme` 为 `emerald`；保存不涉及节点配置或 telemetry.db。
- `GET/PUT /api/v1/admin/appearance` 读取/保存站点默认主题，写入沿用管理员认证与 CSRF；未知主题 ID 返回 `invalid_input`。保存成功后当前页面同步默认值。
- `GET /api/v1/public/appearance` 仅返回 `{default_theme}`，用于登录页和公开页启动；即使公开状态页关闭也可读取主题，不返回节点、凭据或其他管理设置。
- ThemeProvider 负责读取/解码公开展示配置、主题选择与 DOM 属性；读取失败回退 Emerald 并保持页面可用。公共配置是显式 decoder 契约，不在展示组件中自行解析。
- 视觉主题解析优先级为浏览器临时覆盖 → 站点默认 → Emerald。覆盖用独立、版本化 sessionStorage 项保存，值包含 `emerald|classic`；“跟随站点”删除覆盖。自动读取站点默认不写成个人覆盖。
- 明暗模式继续使用现有 localStorage v1 结构，视觉主题切换不修改亮/暗偏好；存储不可用时仍支持当页切换。
- 管理“主题管理”页提供两张实际色板/组件预览卡和当前站点默认标记，保存按钮明确写入站点设置；浏览器偏好入口使用独立主题选择控件，包含“跟随站点”。
- Classic 保留既有视觉色板并适配新的节点卡片和安装对话框；切换主题不丢失节点选择、筛选或安装中的状态。

回退方案：站点设置可切回 Classic。数据库仍执行单调版本校验，旧程序会拒绝新 schema；如需回退旧 Control 二进制，必须恢复升级前的配置库备份，并评估升级后的配置变更。两个主题的样式随 Web 产物内嵌发布，不依赖本地参考目录。

### 2026-10-14: remaining pages design boundary

Implement existing business panels at their feature ownership under web/src/features/admin, extracting the four inline panels only as needed for distinct interaction responsibility. AdminDashboard retains snapshot/SSE/navigation ownership; feature mutations use the established API client with synchronous pending guards, local errors and existing unauthorized callback, then refresh through the parent. Never assume the legacy mutate helper propagates failure (it catches errors). Preserve drafts on failed requests, clear secrets only on success/unmount, use controlled selection so filtering never drops hidden allowlisted nodes. Storage cleanup has two independent exact confirmations and pending guard. Security setup/confirm/disable report inline errors and 401 through auth; password change still revokes all sessions. Use native semantic controls and existing dialog patterns, tokens, strict translations. Expected files: dashboard, four new feature panels and local presentation helpers if justified, AppearancePanel/ChangePasswordPanel, styles/i18n, minor auth/public layout and meaningful behavior tests. No API/database changes. Reference evidence: research/remaining-pages-comparison.md. Rollback by previous source image; back up live data using backup CLI before redeploy.

### Latency detail dialog increment (2026-10-15)

ProbesPanel owns a nullable detail task/node selection instead of auto-selecting a first task and rendering inline history. Task names, explicit view actions and per-node result entries open a native modal. The detail derives the current task from refreshed props; removed tasks close the view, and removed assignments invalidate a selected node. The dialog shows task metadata, assigned-node latest outcomes and explicit node/range controls; use existing loadProbeHistory, latestProbeResult and MetricChart semantics. History requests exist only for an open valid selection, abort on close/change, preserve 401 handling and expose retry/loading/empty states. Keep stable IDs and all TCP bounds; naming is a presentation change, not a new ICMP/HTTP capability. Reuse existing native modal focus lifecycle where suitable. No new dependencies. Primary owns task/spec/evidence; implement agent owns frontend changes. See research/latency-dialog-comparison.md.

### monitor-style TCP configuration (2026-10-15)

Frontend target is one validated host:port string, serialized through existing host/port fields; format IPv6 via existing probeTarget. Add a persisted control-only auto_join boolean (migration010 defaultfalse), DTO emits auto_join, POST defaultfalse for existing callers, PATCH optional *bool preserves omitted values. Frontend decodes autoJoin (missing legacy field defaultsfalse; malformed value rejected); UI new-task defaulttrue, default60s and existing nodes selected. Group filter uses a sentinel separate from arbitrary group names. Selection operations affect visible IDs only and preserve hidden assignments. Edit has no implicit selection of newly appearing nodes except actual refreshed assignments for auto_join.

Storage enrollment consumption assigns all enabled auto_join tasks to a new node within the existing enrollment/node/credential transaction and advances the config snapshot version once if assignments exist. Cap enabled auto_join definitions at64 transactionally so future nodes never exceed the protocol limit; fail task create/update atomically with a stable quota error. Turning auto_join on/off affects future node registration only; existing assignments change solely through submitted node_ids. No Agent payload/protocol changes; its first hello snapshot reads committed assignments. Migration retains existing tasks/assignments with auto_join false, backups before mutation, and rollback after schema10 requires restoring pre-update paired backup, not image-only downgrade. Backend implementer owns Go/migration/API docs/tests; frontend implementer owns Web source/tests/styles/i18n; primary owns task/spec/integration/deployment.

Concurrency review refinement: partial PATCH and single-assignment mutations merge against committed state in one storage transaction (PatchProbeTask / ChangeProbeAssignment / common mutateProbeTask), so future-node enrollment cannot be overwritten by stale handler snapshots. Explicit node_ids remains a deliberate complete replacement. Deterministic gated-body enrollment regression required.

### First-class homepage and site settings (2026-10-15)

Use the existing public_page/public_nodes as the storage owner. Schema011 sets enabled=1 only for never-configured singleton rows (updated_at=0); preserve all existing membership and explicitly saved access choices. Existing enabled represents anonymous access; false now means require administrator login to view the same safe homepage. For false public status calls existing auth(false,...) and on success builds the same strict public DTO with effective page.enabled=true. Anonymous401 is rendered as a login-required state with a fixed safe /admin?next=home link; successful auth returns to /, while ordinary /admin remains dashboard. No extra public fields, private SSE or public rendering of admin Node.

Enrollment atomically adds a homepage membership by default (within existing1000 display-node cap; if full registration still succeeds with homepageVisiblefalse, manual enabling returns409 homepage_node_limit). Derive admin Node.homepageVisible / homepage_visible from membership, with backward legacy decoderfalse. PATCH /admin/nodes/{id} optional homepage_visible merges node metadata/current membership in one storage transaction, independent of hidden. Site PUT /admin/public-page accepts optional node_ids for legacy full replacement; omitting preserves membership inside the same transaction, and scalar settings updates cannot overwrite concurrent enrollment/visibility. Site UI sends only enabled/title/description; remove public-node picker/draft preview from product flow. Existing public_selection draft acknowledgement can remain for setting-state compatibility if useful.

Backend implementer owns Go/storage/migration/API docs/tests. Frontend implementer owns Web site settings, admin node setting/type/decoder, public login state and safe login-return path/tests/styles/i18n. Primary owns specs/task/browser/deployment. No authentication expansion beyond existing single administrator, no public host facts/history and no theme/plugin/toolchain changes. Schema11 rollback requires paired pre-migration backup, not image-only downgrade.
