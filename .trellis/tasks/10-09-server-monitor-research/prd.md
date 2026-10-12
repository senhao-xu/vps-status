# 服务器探针方案

## Goal

为个人和小团队提供一个单管理员、可自托管的服务器探针系统：用户可以低运维成本接入 Linux 服务器，查看实时/历史健康状态、流量、受限 TCP 探测和告警，同时不让 Agent 获得通用远程控制能力。

## Background

- 目标 Git 仓库为 `https://github.com/senhao-xu/vps-status.git`；2026-10-09 检查时为空仓库，没有既有业务代码或技术栈约束。
- 系统由 Linux Agent、中心服务 `Control`、管理/公开 Web 界面与通知出口组成。`Control` 是控制面名称，不代表它具备远程主机控制权限。
- 竞品产品边界见 `research/competitor-notes.md` 和 `research/monitor-komari-deep-dive.md`；存储与保留证据见 `research/storage-retention.md`；TCP 限制证据见 `research/tcp-probe-limits.md`。
- 方案已获实现授权，当前按已审阅的里程碑计划迭代交付。

## Requirements

### Product and administration

- Control 采用单管理员自托管模型，支持首次创建管理员、登录、退出和会话吊销，不提供成员邀请和角色权限。每个管理员最多保留 32 个活跃会话，超限时淘汰最旧会话；启用 MFA 时保留当前会话并吊销其他会话。
- 管理员密码使用 Argon2id 等 memory-hard KDF 存储，登录端点限速。
- 管理员可选启用 TOTP；启用时生成只展示一次的恢复码，服务端只存摘要。TOTP 和恢复码均遗失时，只能通过可直接访问 Control 数据目录的本地 CLI 重置 MFA。
- 节点支持创建、分组、重命名、隐藏、删除和凭据轮换。
- 新节点使用一次性注册密钥与单行安装命令接入；注册后使用独立节点凭据。凭据只展示一次，Control 仅保存摘要并支持吊销/轮换。

### Agent and host monitoring

- MVP 仅支持 Linux amd64/arm64 Agent；Agent 为单文件，支持 systemd，默认以非 root 用户运行并主动通过 WSS 连接 Control。
- Agent 严格只读，只采集系统指标和执行受限 TCP 建连探测，不提供远程命令、Web Terminal、文件管理或通用任务执行。
- Agent 上报主机名、OS、内核、架构、虚拟化、CPU、负载、内存、Swap、磁盘、网络速率/累计流量、运行时间、进程数和 TCP/UDP 连接数。
- 默认上报间隔为 3 秒。原始样本只更新内存实时状态和当前聚合桶，不逐条落盘。
- 管理面板提供节点总览、在线/离线状态、实时指标、节点详情和历史趋势。

### TCP probing

- 管理员可创建 TCP 探测任务，配置 `域名/IP:端口`、执行节点和间隔，并查看实时延迟、历史趋势与失败率。
- 每节点最多执行 64 个 TCP 任务，间隔必须在 5–3600 秒之间。Control 在配置写入时拒绝超限，Agent 也独立限制任务数和间隔。
- 允许公网、私网和回环目标；禁止未指定地址、组播地址和端口 `0`。
- TCP 探测只允许建立连接后立即关闭，不发送载荷、不读取 Banner。Agent 还必须限制 DNS 解析结果数、单次超时、并发和结果上报速率，错过的调度不得追赶式补发。

### History and storage

- 使用两个物理独立的 SQLite 文件：`control.db` 只保存管理员/会话、节点元数据/凭据摘要、探测任务定义、告警/通知和公开页配置；`telemetry.db` 保存主机历史、TCP 探测历史和紧凑的累计/月度流量账本。
- 两库之间不依赖跨库外键或原子事务；遥测以稳定 node/task ID 作逻辑维度，删除节点/任务后由幂等后台清理历史。
- 主机指标和 TCP 探测历史的默认保留梯度为：1 分钟粒度保留 24 小时、10 分钟粒度保留至 7 天、1 小时粒度保留至 30 天。90/365 天小时历史只作为管理员显式开启的扩展选项。
- `telemetry.db` 使用单写者、批量写入、有界 WAL/checkpoint、分批过期删除与受控空闲页回收。管理界面展示主文件/WAL/SHM 体积与最早保留时间。
- 清空历史曲线时保留 `telemetry.db` 中的累计/当月流量账本；只有独立、明确标记为破坏性的“重置全部遥测”才重建整个文件。
- `telemetry.db` 缺失、损坏、只读、达到文件预算或磁盘空间阈值时，Control 停止历史写入但保持登录、配置、实时状态、在线/离线判定与实时阈值告警可用。

### Alerts and public status

- 告警覆盖离线/恢复、CPU、内存、磁盘和流量阈值，支持通用 Webhook 和 Telegram Bot，并防止持续超阈时重复轰炸。
- 提供无需登录的公开状态页。节点默认私有，管理员逐个公开。
- 公开页仅显示节点别名、分组/地区、在线状态、运行时间、CPU、内存、磁盘、实时网速和月流量摘要。公开 API 使用服务端字段白名单，不返回真实主机名、IP、OS/内核或历史曲线。

### Delivery and capacity

- Control 和 Agent 使用 Go，Web 前端使用 TypeScript；单仓库共享协议定义和版本契约。
- 发布两个二进制：Control 内嵌 Web 产物，Agent 提供 Linux amd64/arm64 版本。Control 同时提供 Docker Compose 部署，默认 SQLite，生产环境不依赖 Node.js。
- 公网部署必须使用 TLS/WSS；可通过反向代理终止 TLS。
- MVP 单机容量验收基线为 100 个在线 Agent、合计 400 个 TCP 任务、2 vCPU / 2 GiB Control 主机。这是压测基线，不是全局任务硬上限；更大规模不作为 MVP 承诺。

## Acceptance Criteria

- [ ] 首次启动可创建唯一管理员；登录、退出、会话吊销、限速和密码摘要均有测试。
- [ ] 管理员可启用/验证/关闭 TOTP 并使用一次性恢复码；仅本机 CLI 可在直接访问 Control 数据目录时重置 MFA。
- [ ] 管理员可通过一次性注册密钥与单行命令接入 Agent；Control 无法读回节点凭据明文，吊销/轮换后旧凭据立即失效。
- [ ] Linux amd64/arm64 Agent 可在非 root systemd 服务中采集要求指标，主动 WSS 重连且无命令、终端或文件 API。
- [ ] 实时页可观察 3 秒上报；原始秒级样本不在任一 SQLite 表中逐条累积。
- [ ] 主机与 TCP 历史按 1m×24h、10m×7d、1h×30d 聚合/过期；每个主机或主机/探测序列最多约保留 2,856 个不重叠历史桶。
- [ ] 新增和清理指标、TCP 探测和流量数据时，`control.db` 的行数/文件大小不随历史样本数持续增长。
- [ ] 清空历史不改变任何配置或流量账本；重置全部遥测有单独的破坏性确认。
- [ ] `telemetry.db` 不可写时，Control 仍可登录、修改配置、展示内存实时状态并评估在线/离线与实时阈值告警；界面明确显示历史降级。
- [ ] 管理界面可查看 `telemetry.db` 主文件/WAL/SHM 体积和最早保留时间，过期删除分批进行并能受控回收空闲页。
- [ ] Control 拒绝给单节点保存第 65 个 TCP 任务或 5–3600 秒范围外的间隔；Agent 对超界下发执行同样的独立拒绝/截断。
- [ ] TCP 探测可连接公网、私网和回环目标，但无法发送载荷、读取 Banner、使用端口 `0` 或访问未指定/组播地址。
- [ ] 离线/恢复和 CPU/内存/磁盘/流量阈值可通过 Webhook 与 Telegram 发送，重复超阈不会造成通知风暴。
- [ ] 公开 API 的自动化测试验证字段白名单，真实主机名、IP、OS/内核和历史数据不会泄露。
- [ ] 发布 Linux amd64/arm64 Agent 和内嵌 Web 的 Control；Control 可以单二进制或 Docker Compose 运行，生产环境无需 Node.js。
- [ ] 在 2 vCPU / 2 GiB 环境中持续压测 100 个在线 Agent、400 个 TCP 任务和默认保留策略时，连接、实时状态、分钟聚合、历史清理与告警链路保持可用。

## Out of Scope

- 远程命令、Web Terminal 和文件管理；这是长期安全边界，不是仅延后到下个版本。
- 多账号、多租户、角色权限、节点级用户授权、SaaS 计费与工单。
- Docker/Podman 容器状态和资源指标；Agent 不访问容器运行时 socket。
- 业务日志、分布式链路追踪、应用级 APM 和任意自定义代码插件。

## Deferred

- HTTP、DNS、TLS/Certificate 等黑盒探测。
- GPU、SMART 和多磁盘细节。
- Windows、macOS、ARMv7 等其他 Agent 平台。
- 第三方主题/主题市场、签名自动升级、PostgreSQL 和 Control 多实例。

## Technical Notes

- monitor 证明了只读 Agent、主动 WSS、内存实时态、分钟/小时聚合和双端 TCP 限制的可行性；其单 SQLite 设计和明文 Agent token 不照搬。
- Komari 证明了配置库与指标库物理分离的可行性；Beszel 的激进降采样和 Nezha 的低磁盘降级用于完善容量边界。
- 不同步实现 WebSocket + HTTP 双传输；MVP 只做 WSS，等真实部署数据证明需要时再增加降级通道。
