# monitor 与 Komari 深度对比

调研时间：2026-10-09。以下结论来自 Hub 与 Agent 官方仓库的 README、依赖清单和关键实现，而不只来自产品截图。

## monitor-probe/monitor

检视版本：

- Hub `d8d8283ee9a637b3896f7e89808f8610a4462076`
- Agent `c85572170630b4d8558a7f0f8c48a2aeee078a23`
- Hub 1.4.0、Agent 1.2.0，均为 MIT。

### 架构

```text
Linux Agent (Rust, musl 单文件)
  └─ WSS + Authorization: Bearer + JSON-RPC 2.0
       ├─ hello：静态主机事实
       ├─ report：秒级实时指标
       └─ ping.result：Agent 发起的 TCP 握手探测结果
            ↓
Hub (Rust / Axum)
  ├─ 内存：在线状态和最新值
  ├─ SQLite WAL：配置、分钟历史、小时聚合、流量账本
  ├─ 内嵌管理前端
  └─ 可安装公开页主题
```

### 值得借鉴

- **边界克制**：Agent 只读 `/proc`、`/sys` 与 `statvfs`，不提供命令执行、终端或文件管理；这使节点 token 泄漏的后果主要局限在伪造遥测数据。
- **Agent 简单**：Linux 静态单文件、无运行时依赖、无状态，不在本机保存累计流量；重装和升级成本低。
- **凭据传输更稳妥**：token 放在 `Authorization` 头而不是 URL；非回环地址默认拒绝明文 `ws://`，减少代理访问日志和明文链路泄漏。
- **实时与历史分层**：秒级数据只用于内存实时视图；Hub 每分钟写一条均值/峰值记录，7 天后折叠为小时粒度，避免“1 秒一次上报 = 1 秒一次落库”。
- **SQLite 使用有容量意识**：单写连接、独立只读连接、WAL、busy timeout、缓存、分批清理和小时聚合都在代码中明确处理。
- **不信任被盗 Agent token**：Hub 限制上报频率与探测结果频率；Agent 也限制最多 64 个探测任务、最短 5 秒间隔，防止被恶意 Hub 当成扫描/放大器。
- **安装体验完整**：Hub 生成安装命令、支持 systemd/OpenRC、Agent 二进制中转和版本展示；产品价值不只在仪表盘。

### 局限与不应照搬之处

- Linux-only，且指标集合刻意较小；没有容器、GPU、SMART、多磁盘细节等能力。
- 探测仅做 TCP handshake，不等价于 ICMP、HTTP 内容、DNS 或 TLS 证书监控。
- Hub 以单机 SQLite 和单管理员模型为中心，不适合直接演化为多租户 SaaS。
- Hub 为了能反复展示安装命令，把 Agent token 明文存入 SQLite。新项目更推荐“注册密钥只展示一次 + 服务端只存哈希”，需要重装时轮换，而不是可逆读取旧密钥。
- Rust 能带来紧凑二进制和资源优势，但会提高全栈开发、贡献者招募和迭代门槛；语言选择不能仅因为竞品使用 Rust。

## komari-monitor/komari

检视版本：

- Hub `e48aa1e76931132fb5dc93ce53763dd59df83d9f`
- Agent `828afafe7fb5b1b58c1232094e1e46320719016e`
- Hub/Agent 均为 MIT。

### 架构

```text
跨平台 Agent (Go)
  ├─ WebSocket + JSON-RPC 2.0（首选）
  ├─ HTTP POST + long-poll（降级）
  ├─ 秒级系统/GPU指标与分布式 Ping
  └─ 可选能力：命令、终端、文件、配置下发、版本切换
            ↓
Hub (Go / Gin)
  ├─ 主配置库：SQLite + GORM
  ├─ 指标库：SQLite / MySQL / PostgreSQL
  ├─ 原始数据批写 + 多级 rollup
  ├─ 管理/公开 Web、2FA、OAuth、通知
  └─ 主题与具有服务端权限的 JavaScript 插件
```

### 值得借鉴

- **跨平台与指标覆盖广**：Linux/Windows Agent、GPU、多挂载点、网卡筛选、流量重置等更接近完整产品。
- **传输降级**：WebSocket 失败时以 POST 上报并 long-poll 控制事件，适应不支持 WebSocket 的代理环境；代价是双路径状态机更复杂。
- **指标存储认真**：指标库与主配置库分离；支持 SQLite/MySQL/PostgreSQL；原始样本短期保留，再按 1 分钟、5 分钟、1 小时、1 天分层聚合。
- **运维闭环完整**：自动发现、2FA、登录审计、OAuth、通知、备份、主题、插件、在线更新和节点版本切换都已有实现。
- **高风险操作有部分防线**：终端/命令入口可要求 2FA，Agent 可用 `disable_web_ssh` 关闭远程控制，文件操作也有根目录删除保护与传输会话约束。

### 复杂度与安全代价

- **它不是单纯探针**：Agent 声明 `exec/terminal/file/startup_config/switch_version` 等能力，默认 `disable_web_ssh=false`，即远程控制默认开启。Hub 或高权限凭据失陷时，影响可扩大为节点命令执行和文件读写。
- **控制通道与遥测凭据耦合**：同一个 Agent token 既用于上报，也参与接收控制事件。新项目应让遥测、升级和远程控制采用不同 capability 与凭据。
- **token 出现在 URL query**：WebSocket/POST 地址使用 `?token=`，更容易被代理、错误追踪或访问日志记录。应改用请求头，并避免在普通日志中打印完整连接 URL。
- **敏感值明文持久化**：Agent token 和会话 token 直接存库；新项目应默认只保存不可逆摘要，并提供轮换与吊销。
- **本地密码哈希不宜复用**：当前实现为 SHA-256 加固定 salt，不是抗暴力破解的 memory-hard KDF。新项目必须使用 Argon2id、scrypt 或合适参数的 bcrypt，并加登录限速。
- **扩展系统扩大信任边界**：插件可拦截 HTTP/WebSocket，获得管理员级能力，并可选择文件、网络和子进程权限；这要求签名、权限审批、隔离、审计与供应链治理。
- **两套数据库、多协议、多平台、插件和远程控制**都会提高测试矩阵和迁移成本，不适合作为从零项目的首个里程碑。

## 对本项目的建议

### 推荐吸收

- 产品边界与基础安全模型以 monitor 为起点：只读 Agent、主动 WSS、Header token、实时内存态、分钟级落库、小时聚合、单机部署。
- 数据模型借鉴 Komari：从一开始把 metric definition、采样点和 rollup 策略做成可扩展边界，但 MVP 只实现 SQLite。
- 安装体验同时借鉴两者：单行安装、systemd/OpenRC、明确版本、可观测的 Agent 在线状态和安全升级路径。
- 告警先做离线、CPU/内存/磁盘/流量阈值和 Webhook；通知渠道适配器可以后加。

### 推荐明确拒绝进入 MVP

- Web Terminal、任意命令执行、文件管理。
- JavaScript 插件或服务端任意代码插件。
- Agent 自动更新默认开启；应改为签名验证、显式渠道和可回滚的管理员触发更新。
- WebSocket + POST 双传输并行实现；首版只做 WSS，等真实部署数据证明需要再加降级。
- MySQL/PostgreSQL 与 SQLite 同时支持；先稳定一个存储契约，再按目标规模决定扩展。

## 仍需用户决定

两个项目代表的不是技术栈偏好，而是产品权限边界：

- `monitor` 路线：轻量探针 + 状态面板，凭据失陷时尽量不获得主机控制权。
- Komari 路线：监控 + 远程运维平台，以更高攻击面换取终端、命令、文件和插件能力。

这一选择会改变 Agent 权限、协议 capability、鉴权、审计、更新、测试和发布流程，不能在实现阶段临时追加。
