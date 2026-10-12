# 服务器探针实施计划

## Delivery strategy

这是从空仓库启动的复杂项目。当前 Trellis 任务按下列里程碑迭代，每个里程碑都要独立完成实现、测试和评审后再继续。如某个后续里程碑扩大到需要单独审批、分支或发布，再将它拆成子任务；不为已完成的基础骨架补建虚假子任务。

## Milestone 0: repository and build foundation

- [x] 将工作区初始化为 Git 仓库，默认分支为 `main`，建立版本信息策略。
- [x] 由仓库所有者选择并添加 MIT 许可证。
- [x] 建立 Go module、`cmd/control`、`cmd/agent`、`internal/*`、`api`、`web`、`deploy` 与 `tests` 目录。
- [x] 初始化 TypeScript/React/Vite Web 工程，完成开发代理和构建产物嵌入 Control 的最小闭环。
- [x] 配置统一 lint、format、unit test、构建和 CI 命令，提交可复现的 pnpm 锁文件。
- [x] 增加版本信息注入，Control/Agent 能输出版本、commit 和协议版本。

Validation:

```bash
go test ./...
go vet ./...
pnpm --dir web lint
pnpm --dir web typecheck
pnpm --dir web test
pnpm --dir web build
```

Rollback point: 只有骨架与 CI，不存在数据格式兼容负担。

## Milestone 1: control database and administrator security

- [x] 引入纯 Go SQLite driver、嵌入式迁移和 control.db 连接/PRAGMA 管理。
- [x] 实现首次创建唯一管理员、Argon2id 密码摘要、登录限速、会话 cookie、CSRF、退出和会话吊销。
- [x] 实现 TOTP 启用/验证/关闭、一次性恢复码和本机 `control admin reset-mfa` CLI。
- [x] 实现部署级 master key 加载/生成策略，用于通知渠道等可逆秘密。
- [x] 增加身份认证、限速、cookie/CSRF、密钥脱敏和 CLI 权限测试。

Validation:

```bash
go test ./internal/security/... ./internal/storage/control/... ./internal/control/...
go test -race ./internal/security/... ./internal/control/...
```

Rollback point: control.db migration 每个版本有前置备份；不在未验证的 down migration 上承诺无损回退。

## Milestone 2: enrollment, protocol, and connection lifecycle

- [x] 定义 Agent v1 JSON 消息契约、大小上限、序号、错误类型和版本协商。
- [x] 实现一次性注册密钥、节点凭据摘要、吊销/轮换和单行安装配置输出。
- [x] 实现 `/agent/v1/ws`、header 凭据、会话 nonce/序号、有界读写队列、heartbeat 和断线清理。
- [x] 实现 Agent 指数退避+抖动重连，确保不会因 Control 故障形成重连风暴。
- [x] 添加过期/吊销凭据、重复帧、乱序帧、超大帧、慢读写与不兼容版本测试。

Validation:

```bash
go test ./internal/protocol/... ./internal/control/... ./internal/agent/...
go test -race ./internal/protocol/... ./internal/control/... ./internal/agent/...
```

Rollback point: Agent v1 契约在首个公开发布前可调整；公开后必须通过协议版本兼容。

## Milestone 3: Linux collection and live monitoring

- [x] 实现 Linux `/proc`、`/sys`、statfs 采集器和无特权降级语义。
- [x] 实现 CPU/负载/内存/Swap/磁盘/网络/运行时间/进程/连接数的平台解析和边界测试。
- [x] 实现 3 秒上报、Control 频率/数值校验、内存快照、在线/离线判定与管理 SSE。
- [x] 提供节点 CRUD、分组、重命名、隐藏和实时 API。
- [x] 以 fixtures 覆盖内核版本、缺失文件、计数器回绕/重置、网卡变更和磁盘不可访问。

Validation:

```bash
go test ./internal/agent/... ./internal/control/...
go test -race ./internal/agent/... ./internal/control/...
```

## Milestone 4: telemetry database, traffic ledger, and retention

- [x] 创建独立 telemetry.db 迁移、单写者、有界 batch queue 和只读查询边界。
- [x] 实现主机 1 分钟内存聚合与 `host_1m`，确保秒级报告不落盘。
- [x] 实现重启/计数器重置感知的流量差值与累计/月度账本。
- [x] 实现 1m → 10m → 1h 幂等 rollup，24h/7d/30d 分批过期与 90/365 天扩展选项。
- [x] 实现 WAL/SHM/主库体积、最早保留时间、freelist 和清理状态 API。
- [x] 实现 `Clear history`、保留 traffic ledger 的表级重建、单独的 `Reset all telemetry`、checkpoint/incremental vacuum 和手动 full vacuum。
- [x] 实现文件预算/剩余磁盘 guard、滞回恢复与历史降级显示。

Validation:

```bash
go test ./internal/storage/telemetry/... ./internal/control/...
go test -race ./internal/storage/telemetry/...
```

必做故障注入：只读文件、磁盘阈值、写入超时、WAL 膨胀、Control 在 rollup/清理中途崩溃。

Rollback point: control.db 不依赖 telemetry schema；任一 telemetry 迁移失败时可保留原文件并降级为无历史运行。

## Milestone 5: bounded TCP probes

- [x] 实现 probe task/assignment CRUD 和每节点 64、5–3600 秒的 Control 事务校验。
- [x] 实现带版本的 `probe.sync`，Agent 独立限制 64 任务/间隔，并报告被拒绝的超界配置。
- [x] 实现任务内串行、全局 semaphore、missed-tick skip、DNS 最多 3 地址、1 秒单地址/3 秒总超时。
- [x] 允许 public/private/loopback，拒绝 unspecified/multicast/port 0；用测试保证 TCP 成功路径不发送或读取字节。
- [x] 实现结果频率限制、分钟聚合、probe rollup/过期与实时/历史 API。
- [x] 添加恶意 Control 快照、DNS 多答案、慢 DNS、慢 TCP、大量同时 tick 和结果洪泛测试。

Validation:

```bash
go test ./internal/agent/... ./internal/control/... ./internal/storage/telemetry/...
go test -race ./internal/agent/... ./internal/control/...
```

## Milestone 6: alerts and notifications

- [x] 实现 offline/recovery 与 CPU/memory/disk/traffic 规则、持续时间、恢复滞回和状态机。
- [x] 实现 Webhook 签名/超时与 Telegram Bot，通知秘密加密存储且 API 不读回明文。
- [x] 实现有界队列、退避重试、转移去重、可选有界 reminder 和通知测试功能。
- [x] 覆盖持续超阈、抖动、Control 重启、渠道超时/429/5xx 与 telemetry 降级测试。

## Milestone 7: admin UI and public status page

- [x] 实现管理员登录/MFA、节点列表/详情、实时 SSE、历史图表和月流量。
- [x] 实现 TCP 任务/延迟/失败率、告警规则/渠道、存储状态与历史清理 UI。
- [x] 实现节点默认私有的公开页配置和单独 public DTO/API。
- [x] 用 API/contract 测试对真实 hostname、IP、OS/kernel、凭据、历史数据做负面断言。
- [x] 完成键盘操作、基本无障碍、时区/数值单位和移动端布局验证。

Validation:

```bash
pnpm --dir web lint
pnpm --dir web typecheck
pnpm --dir web test
pnpm --dir web build
go test ./internal/control/...
```

## Milestone 8: packaging, operations, and full-scope verification

- [x] 生成 Control 单二进制、Agent linux/amd64 + linux/arm64、checksum 和版本信息。
- [x] 提供非 root Agent systemd 用户/单元、安装/卸载/轮换凭据流程。
- [x] 提供 Control Dockerfile/Compose、持久化数据目录、health/readiness 和 TLS 反向代理示例。
- [x] 实现 control.db 默认备份、telemetry 可选备份和恢复验证。
- [x] 完成安全评审：日志脱敏、公开 API 白名单、CSRF/CORS、WSS 限速、依赖/镜像扫描。
- [ ] 在 2 vCPU / 2 GiB 环境运行 100 Agent + 400 TCP 任务持续负载，验证连接、聚合、查询、告警、清理、WAL 和磁盘 guard。
- [x] 跑全量回归，对照 PRD 每个验收条目留下可复现证据。

Final validation commands:

```bash
go test ./...
go test -race ./...
go vet ./...
pnpm --dir web lint
pnpm --dir web typecheck
pnpm --dir web test
pnpm --dir web build
docker compose -f deploy/docker-compose.yml config
```

## Review gates

1. 任何协议消息新增前，检查它是否会突破“只读采集 + 受限 TCP”边界。
2. 任何数据表新增前，确认它属于 control 还是 telemetry，不让样本/流量/探测值进入 control.db。
3. 任何公开 API 变更都要更新白名单负面测试。
4. 任何 TCP 扩展都必须在 Control 与 Agent 两侧有等价上限。
5. 任何迁移或清理都要证明中断后可幂等恢复。

## Follow-up checks before activation

- [x] 用户已审阅并明确批准最新 PRD/design/implement 摘要。
- [x] 确认仓库初始化方式和主分支，不覆盖用户数据。
- [x] 确认 Trellis 实施/检查上下文不再是 `_example` 占位行。
- [x] 只在上述条件满足后运行 `task.py start`。
