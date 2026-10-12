# VPS Status

VPS Status is a self-hosted, read-only Linux server monitoring project. The repository contains the Control and Agent, bounded Agent v1 protocol, Linux host collection, live/history monitoring, traffic accounting, connect-only TCP probes, live alerts, an administrator UI, and an allowlisted public status page.

Control keeps low-growth identities and configuration in `control.db` while completed host/probe rollups and the compact traffic ledger live in a physically separate `telemetry.db`. Raw three-second reports and individual TCP results stay in memory. The Linux Agent collects host metrics every three seconds and executes at most 64 assigned TCP tasks with bounded DNS, deadlines, and global concurrency. TCP checks only connect and immediately close; they never send payloads or read banners. Remote commands, Web Terminal, file management, and generic task execution are not supported.

## Docker 快速安装 / Quick install

当前 Docker 支持使用仓库内的多阶段 `Dockerfile` 从源码构建 Control 镜像，镜像内已
包含 Web 管理界面，运行时使用非 root 用户和只读根文件系统。仓库目前未配置 GHCR
或其他镜像仓库的预构建镜像发布，因此安装时需要在本机构建。

Docker Compose 部署的是 Control；Agent 暂无配套 Docker 部署，使用 Linux
amd64/arm64 二进制和 systemd 安装到被监控主机，详见下方的 Agent 接入说明。

准备一台安装了 Git、Docker Engine 和 Docker Compose v2 或更新版本的 Linux 主机。
宿主机无需安装 Go、Node.js 或 pnpm；首次构建需要联网拉取基础镜像及 Go/pnpm 依赖。
以下命令均在仓库根目录执行：

```bash
git clone https://github.com/senhao-xu/vps-status.git
cd vps-status
docker compose -f deploy/docker-compose.yml up -d --build
```

检查容器、Control 健康状态和日志：

```bash
docker compose -f deploy/docker-compose.yml ps
curl -fsS http://127.0.0.1:8080/healthz
curl -fsS http://127.0.0.1:8080/readyz
docker compose -f deploy/docker-compose.yml logs --tail=100 control
```

Compose 默认只监听宿主机回环地址 `127.0.0.1:8080`，不要改为公网暴露。如果本机
8080 端口已被占用，可以保持回环绑定并修改宿主机端口：

```bash
VPS_STATUS_PORT=18080 docker compose -f deploy/docker-compose.yml up -d --build
```

`VPS_STATUS_PORT` 只对当前命令生效；后续每次执行 `up` 或重新构建时都要传入相同值，
并把健康检查 URL 和反向代理上游端口一并改为 `18080`。

数据保存在 Compose 中名为 `control-data` 的命名卷（默认实际 Docker 卷名为
`deploy_control-data`）挂载的 `/data` 目录中。管理员会话 Cookie 始终带有 `Secure`
属性：上面的 HTTP 地址只适用于本机健康检查，浏览器中的管理员初始设置和登录必须通过
Caddy 或 nginx 反向代理的 HTTPS 地址进行。

### HTTPS 与首次访问

以安装在同一宿主机上的 Caddy 为例：将你的域名（下面用 `monitor.example.com`
占位）的 DNS A/AAAA 记录指向该主机，并放行入站 TCP 80/443。在已有的
`/etc/caddy/Caddyfile` 中添加以下站点配置，替换为你的实际域名：

```caddyfile
monitor.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

如果修改过 `VPS_STATUS_PORT`，同步修改上面的上游端口。确认 Caddy 已安装并作为
systemd 服务运行后，验证并重新加载配置：

```bash
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

Caddy 会自动申请和续期 HTTPS 证书。打开 `https://monitor.example.com/`，首次访问
会显示创建唯一管理员的表单；自行设置用户名和至少 12 位密码，没有默认账号密码。
完整的 [Caddy 示例](deploy/reverse-proxy/Caddyfile) 和
[nginx 示例](deploy/reverse-proxy/nginx.conf) 均已提供。

登录后，在 **Nodes → Enroll a Linux node** 中填写节点名称和 Control 的 HTTPS
地址，生成一次性注册密钥。该页面的命令仅负责注册已有 Agent，不会下载二进制或安装
systemd 服务；请先准备匹配主机架构且通过 checksum 校验的 Agent 二进制，再按
[Agent systemd 安装说明](deploy/README.md#agent-systemd-installation)完成安装和注册。

### 更新与停止

更新代码并重新构建：

```bash
git pull --ff-only
docker compose -f deploy/docker-compose.yml up -d --build
```

临时停止可以使用 `stop`，随后用 `start` 恢复；`down` 会删除容器和 Compose 网络，
但保留命名卷中的数据：

```bash
docker compose -f deploy/docker-compose.yml stop
docker compose -f deploy/docker-compose.yml start
docker compose -f deploy/docker-compose.yml down
```

**警告：** `docker compose -f deploy/docker-compose.yml down -v` 会删除 `control-data`
命名卷及 Control 数据，仅应在确认已备份且需要销毁数据时执行。

TLS 反向代理、备份/恢复以及 Agent 的 systemd 安装说明见
[deploy/README.md](deploy/README.md)。

## Source development requirements

- Go 1.24 or newer
- Node.js 22 or newer
- pnpm 10

## Development

Run the Go checks without installing any web dependencies:

```bash
make test-go
make build-go
```

After installing the web dependencies, run all checks:

```bash
pnpm --dir web install
make test
make build
```

`make build` writes `bin/control` and `bin/agent`. The Control build uses the React assets generated into `internal/control/webui/dist`. A normal Go build without the `webui` tag embeds a small fallback page, so Go tests and local backend work do not depend on Node.js.

Both binaries expose build metadata:

```bash
go run ./cmd/control --version
go run ./cmd/agent --version
```

An administrator can create a one-time installation command through
`POST /api/v1/admin/enrollments`. The command enrolls the Agent and writes its
node credential to a mode-0600 config. A configured Agent runs with:

```bash
go run ./cmd/agent --config /etc/vps-status/agent.json
```

See [api/README.md](api/README.md) for enrollment and Agent v1 contracts.

Release builds can inject `VERSION` and `COMMIT`:

```bash
make build VERSION=v0.1.0 COMMIT="$(git rev-parse --short HEAD)"
```

The release script builds the embedded-Web Control, both Linux Agent
architectures, and `SHA256SUMS`:

```bash
scripts/release.sh v0.1.0 "$(git rev-parse HEAD)" dist
```

## Running Control

```bash
go run ./cmd/control --data-dir ./data --listen :8080
```

The administrator API uses `Secure` cookies. Terminate TLS at a reverse proxy before using it from a browser. If both TOTP and recovery codes are lost, an operator with direct access to the Control data directory can reset MFA and revoke every session:

```bash
go run ./cmd/control admin reset-mfa --data-dir ./data
```

The administrator UI is served at `/`. The unauthenticated `/public` page is
disabled by default; enable it and explicitly select published nodes in the
Public page section. Its API is a separate server-side allowlist projection
that does not expose host identity, credentials, probe targets, or history.

Deployment, non-root systemd installation, TLS reverse proxy, credential
rotation, backup/verified restore, and Docker Compose instructions are in
[deploy/README.md](deploy/README.md). The security boundary and scan workflow
are recorded in [deploy/SECURITY.md](deploy/SECURITY.md); the PRD regression
map and bounded 100/400 capacity command are in
[tests/acceptance.md](tests/acceptance.md) and [tests/README.md](tests/README.md).

## License

MIT. See [LICENSE](LICENSE).
