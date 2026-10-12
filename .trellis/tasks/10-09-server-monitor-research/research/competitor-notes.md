# 同类方案初步调研

调研时间：2026-10-09。证据来自各项目官方 GitHub 仓库当前默认分支；以下结论用于产品定位，不代表最终技术设计。

## 1. 哪吒监控（Nezha）

- 定位：自托管、轻量的服务器/网站监控与运维工具。
- 能力：主机状态、HTTP/TCP/Ping、证书、告警、计划任务、Web Terminal。
- 技术形态：Go 服务端；Agent 与服务端之间使用双向流式 gRPC/Protobuf，上报系统状态的同时承载任务和 IO 流；服务端默认可使用 SQLite。
- 可借鉴：Agent 主动连接中心端，适合 NAT 后服务器；二进制协议和双向流便于实时状态及控制消息复用。
- 风险提示：监控与远程执行共用信任通道会显著扩大攻击面。MVP 若没有明确运维需求，不应默认加入命令执行和终端。
- 许可证：Apache-2.0。
- 证据：`nezhahq/nezha` 的 `README.md`、`proto/nezha.proto`、`go.mod`；检视提交 `9881c47f0cc92256ba203919da5a25fab79a6bad`。

## 2. Beszel

- 定位：轻量服务器监控，强调主机、Docker/Podman、历史数据与告警。
- 能力：CPU/内存/磁盘/网络、容器、温度、GPU、SMART、ZFS，多用户、OAuth/OIDC、备份。
- 技术形态：Go Hub + Go Agent；Hub 基于 PocketBase。新版 Agent 优先主动建立 WebSocket 到 Hub，使用 CBOR 二进制消息和挑战/签名校验；也保留 Hub 通过 SSH 拉取的兼容模式。单容器即可部署 Hub。
- 可借鉴：轻量、开箱即用和主动 WebSocket 对家用/小团队场景很合适；主机与容器指标结合，产品价值直接。
- 风险提示：同时维护 WebSocket 与 SSH 两种连接模式会增加状态机、认证与故障恢复复杂度，不适合作为首版目标。
- 许可证：MIT。
- 证据：`henrygd/beszel` 的 `readme.md`、`agent/client.go`、`agent/server.go`、Docker Compose 示例；检视提交 `bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3`。

## 3. Uptime Kuma

- 定位：易用的自托管可用性监控，接近 Uptime Robot，而不是主机资源探针。
- 能力：HTTP(S)、TCP、关键字、JSON Query、WebSocket、Ping、DNS、Push、证书、通知和多状态页；官方说明最短 20 秒检测间隔。
- 技术形态：Node.js + Vue；SQLite；Socket.IO 驱动实时 UI；Docker 单实例部署。
- 可借鉴：状态页、服务分组、事件时间线和丰富通知渠道是用户能直接感知的价值。
- 局限：从中心节点发起的黑盒检测不能替代主机 CPU、内存、磁盘、网络等内部指标；单检测点也无法代表多地域可用性。
- 许可证：MIT。
- 证据：`louislam/uptime-kuma` 的 `README.md`、`package.json`。

## 4. monitor-probe/monitor

- 定位：面向 Linux VPS 的极简自托管监控与公开状态页。
- 技术形态：Rust Agent 通过 WSS/JSON-RPC 2.0 主动连接 Rust/Axum Hub；SQLite 保存分钟历史并做小时聚合；Hub 和 Agent 均为静态单文件取向。
- 关键优势：Agent 无状态且只读，不做命令/终端/文件管理；token 使用 Authorization Header；对明文 WebSocket、上报频率和探测任务都有防护。
- 局限：Linux-only、指标和探测类型较窄、单机/单管理员取向；Hub 明文保存 Agent token 以便重复展示安装命令。
- 结论：目前最接近推荐 MVP 的参考实现，但凭据持久化需要重新设计。
- 详细证据：`research/monitor-komari-deep-dive.md`。

## 5. Komari

- 定位：由服务器监控扩展到远程控制、文件管理、插件、主题和多数据库的综合平台。
- 技术形态：跨平台 Go Agent 主动 WebSocket/JSON-RPC，上报失败时可退回 HTTP POST + long-poll；Go/Gin Hub 使用 SQLite 配置库，并支持 SQLite/MySQL/PostgreSQL 指标库和多级 rollup。
- 关键优势：功能完整、跨平台、指标丰富，具备自动发现、OAuth/2FA、备份、通知和升级能力。
- 主要代价：遥测与远程控制共享 Agent 通道；远程控制默认开启；传输/数据库/插件矩阵复杂。当前 token query、明文 token/session 与本地密码 KDF 等实现不应照搬。
- 结论：适合研究长期能力边界，不适合把完整功能面直接定义为首版。
- 详细证据：`research/monitor-komari-deep-dive.md`。

## 初步结论

三者不是同一产品的不同实现，而是三种边界：

1. 主机指标探针：回答“服务器现在是否健康、资源用在哪里”。
2. 黑盒可用性监控：回答“用户是否能访问服务”。
3. 远程运维平台：回答“发现问题后能否直接操作服务器”。

推荐 MVP 只把第一类做深，吸收第二类的状态页/告警表达，但不在首版加入远程命令执行。`monitor` 证明这条窄边界可以形成完整产品；Komari 则证明一旦加入控制、文件、插件与多存储，系统会快速变成另一类平台。主机指标构成最小闭环；黑盒监控可以作为中心服务内的独立采集器后加；远程执行需要更强的密钥管理、审计、授权和供应链安全。

产品定位尚未确认前，技术上只形成以下暂定原则：

- Agent 主动出站连接，避免要求被监控服务器开放入站端口。
- Agent 只读采集且以非 root 身份运行；需要特权的指标显式降级，而不是默认提权。
- 控制面与遥测数据面分离建模，即使首版复用一条连接，也不让“上报凭据”天然获得远程执行权限。
- 单机自托管优先时可从 SQLite 起步；多租户 SaaS 则应从 PostgreSQL、租户隔离、配额与审计起步，二者不能靠后期简单切换来等价替代。
