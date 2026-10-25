# VPS Status

VPS Status is a self-hosted, read-only Linux server monitoring project. The repository contains the Control and Agent, bounded Agent v1 protocol, Linux host collection, live/history monitoring, traffic accounting, connect-only TCP probes, live alerts, an administrator UI, and an allowlisted public status page.

Control keeps low-growth identities and configuration in `control.db` while completed host/probe rollups and the compact traffic ledger live in a physically separate `telemetry.db`. Raw three-second reports and individual TCP results stay in memory. The Linux Agent collects host metrics every three seconds and executes at most 64 assigned TCP tasks with bounded DNS, deadlines, and global concurrency. TCP checks only connect and immediately close; they never send payloads or read banners. Remote commands, Web Terminal, file management, and generic task execution are not supported.

## Docker 快速安装 / Quick install

当前 Docker 支持使用仓库内的多阶段 `Dockerfile` 从源码构建 Control 镜像，镜像内已
包含 Web 管理界面，运行时使用非 root 用户和只读根文件系统。Control 仍从源码构建。

Agent 支持 Linux amd64/arm64 的 **systemd、Docker 和手动运行二进制**。
Docker 镜像由 GitHub Actions 构建并发布到 GitHub Container Registry：
`ghcr.io/senhao-xu/vps-status-agent:latest`。容器采集宿主机指标，安装方法见
[Agent Docker 安装说明](deploy/README.md#agent-with-docker)。

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

Caddy 会自动申请和续期 HTTPS 证书。打开 `https://monitor.example.com/` 即进入公开状态页，
点击右上角“管理后台”进入 `/admin`。首次进入后台会显示创建唯一管理员的表单；
自行设置用户名和密码，没有默认账号密码，服务端不限制
管理员密码长度。
完整的 [Caddy 示例](deploy/reverse-proxy/Caddyfile) 和
[nginx 示例](deploy/reverse-proxy/nginx.conf) 均已提供。

登录后，在 **安装设置 / Installation settings** 中统一配置目标服务器能访问的面板 HTTPS
地址。留空时使用当前页面的 HTTPS 地址；通过 localhost 或 SSH 隧道访问后台时必须配置
远程节点可达的地址。仅在本机测试时，可以保持地址为空，在添加节点弹窗中勾选“仅在本机测试”，
使用当前本地地址生成安装命令；这条命令只能用于与当前浏览器在同一电脑上的 Agent，不能用于远程 VPS。
添加节点时填写名称，生成命令后选择 **systemd** 或 **Docker**，复制命令到目标服务器执行；
对话框会显示等待安装、已注册和在线状态。
安装命令只在创建时展示，过期后可重新生成，未使用的令牌可以撤销。安装程序支持 Linux amd64/arm64，
命令直接从指定的 GitHub Release 下载独立 `bootstrap-agent.sh`，再下载 Agent、安装脚本和 systemd unit，逐项校验
`SHA256SUMS`，然后创建非 root 服务账户、完成一次性注册并启动服务。页面同时保留仅注册
已有 Agent 的手动命令。详细要求和排错方式见
[Agent systemd 安装说明](deploy/README.md#agent-systemd-installation)。

### 内置主题管理

管理后台的 **主题管理 / Theme management** 提供 Emerald 和 Classic 两个内置主题，
支持预览并保存站点默认值。新安装默认使用 Emerald，采用翠绿色背景与紧凑节点卡片，
不包含 3D 地球；Classic 保留原有视觉风格。默认主题保存在 `control.db`，Control
重启后仍生效，登录页、管理界面和公开状态页使用相同设置。

页面上的主题选择允许当前浏览器会话临时使用 Emerald 或 Classic，选择“跟随站点”
即可恢复站点默认主题。亮色/暗色切换独立保存为浏览器偏好；没有偏好时跟随系统。
目前支持内置主题，不提供第三方主题上传或主题市场。

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

TLS 反向代理、备份/恢复以及 Agent 的 systemd/Docker 安装说明见
[deploy/README.md](deploy/README.md)。

## Source development requirements

- Go 1.25 or newer (the repository selects the patched Go 1.27.3 toolchain)
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
`POST /api/v1/admin/enrollments`. It downloads a pinned Linux amd64/arm64
release, verifies SHA-256 checksums, installs the non-root systemd service,
enrolls the Agent, and writes its node credential to a mode-0600 config. The
response also includes a manual enrollment command for an already-installed
Agent. A configured Agent runs with:

```bash
go run ./cmd/agent --config /etc/vps-status/agent.json
```

See [api/README.md](api/README.md) for enrollment and Agent v1 contracts.

Release builds can inject `VERSION` and `COMMIT`:

```bash
make build VERSION=v0.1.0 COMMIT="$(git rev-parse --short HEAD)"
```

The release script builds the embedded-Web Control, both Linux Agent
architectures, packages the standalone bootstrap and systemd install/uninstall assets, and writes one
`SHA256SUMS` covering every release asset:

```bash
scripts/release.sh v0.1.0 "$(git rev-parse HEAD)" dist
```

Pushing a semantic version tag runs the release workflow, verifies those
checksums, and publishes the complete `dist/` set to the matching GitHub
Release. The default one-command installer downloads only from that exact tag.

## Running Control

```bash
go run ./cmd/control --data-dir ./data --listen :8080
```

Release builds pin one-command Agent installation to the Control build version
and the repository's GitHub Releases URL. A mirror or independently selected
Agent release can be configured explicitly:

```bash
vps-status-control --agent-release-version v0.1.0 \
  --agent-release-base-url https://downloads.example.com/vps-status
```

Source builds identify as `dev`. Set an explicit published Agent version in
**Installation settings** or with `--agent-release-version` before generating
an installation command. The saved version takes precedence over the startup
default. An eligible Release must include `bootstrap-agent.sh` and the matching
Agent, systemd installer/unit, and `SHA256SUMS`; older Releases without the new
bootstrap asset cannot serve the new command. The UI does not check whether a
configured tag has been published. No Release is created merely by building
or configuring Control.

The administrator API uses `Secure` cookies. Terminate TLS at a reverse proxy before using it from a browser.

管理员可在后台 **安全 → 修改密码** 中验证当前密码并设置新密码。修改成功后所有
管理员会话失效，页面返回登录页；原有 MFA 设置保持不变，使用新密码重新登录即可。

If both TOTP and recovery codes are lost, an operator with direct access to the Control data directory can reset MFA and revoke every session:

```bash
go run ./cmd/control admin reset-mfa --data-dir ./data
```

The unauthenticated status page is served at `/`, with `/public` retained as
a compatible address. Its top-right administration link opens `/admin`, where
administrator setup and login are handled. The homepage is open by default and
new nodes appear automatically. Use **Site settings** for the site name,
description and **Access requires login**; choose **Homepage display** in each
node’s settings to control which nodes appear. Upgrades preserve saved access
settings and existing node display choices. Both open and login-required
homepages use the same limited status API, without host identity, credentials,
probe targets or history.

Deployment, non-root systemd installation, TLS reverse proxy, credential
rotation, backup/verified restore, and Docker Compose instructions are in
[deploy/README.md](deploy/README.md). The security boundary and scan workflow
are recorded in [deploy/SECURITY.md](deploy/SECURITY.md); the PRD regression
map and bounded 100/400 capacity command are in
[tests/acceptance.md](tests/acceptance.md) and [tests/README.md](tests/README.md).

## License

MIT. See [LICENSE](LICENSE).
