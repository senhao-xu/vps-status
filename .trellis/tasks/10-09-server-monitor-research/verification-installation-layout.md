# 统一节点接入与页面排版验收

Date: 2026-10-14. 用户在审阅 Komari/monitor 源码对比后授权实施，并要求参考页面排版。范围：统一安装设置、name-only 添加节点、GitHub Release bootstrap、节点总览/安装对话框/安装设置布局。

## 实现

- Control schema v9 singleton installation，保存共享 HTTPS 地址与可选 Agent 版本；管理员 GET/PUT 安装配置，认证/CSRF 与严格字段校验。
- 地址优先已保存值，兼容旧客户端显式地址；未传地址时验证请求 Origin。新 UI 不生成指向 localhost 的默认远程命令。
- 未配置可用发布版本时，服务端在创建 token 前返回 `409 agent_release_required`，UI 引导安装设置。
- 命令从指定 GitHub Release 下载 `bootstrap-agent.sh`，四个参数全部 shell quote。 canonical `internal/control/bootstrap-agent.sh` 同时嵌入兼容面板端点并纳入 Release；Agent/installer/unit 保留 SHA-256 校验。
- 节点总览采用分区工具栏、汇总、过滤和空状态主操作；安装设置用分区卡片、保存/取消操作；对话框仅填写名称，展示自动接入地址并保留原安装状态机。

## 自动化证据

通过：

```sh
# 使用项目固定 pnpm 10.18.0，web/
pnpm lint
pnpm typecheck
pnpm test  # 143 tests / 17 files
pnpm build

# 仓库根目录，集成测试允许临时本机端口
GOCACHE=/tmp/vps-status-go-cache go test ./...
GOCACHE=/tmp/vps-status-go-cache go test -race ./...
GOCACHE=/tmp/vps-status-go-cache go vet ./...
GOCACHE=/tmp/vps-status-go-cache go test -tags webui ./...
GOCACHE=/tmp/vps-status-go-cache go build -tags webui -o /tmp/vps-status-install-browser/control-next ./cmd/control
docker compose -f deploy/docker-compose.yml config --quiet
GOCACHE=/tmp/vps-status-go-cache go run ./tests/load --agents 5 --tasks-per-agent 4 --duration 15s
git diff --check
gofmt -l internal/control internal/storage/control

# 本地发布形状验证；没有创建 Git tag 或上传 GitHub
GOCACHE=/tmp/vps-status-go-cache sh scripts/release.sh v1.2.3 4f809e5da19a /tmp/vps-status-install-release
cd /tmp/vps-status-install-release
sha256sum -c SHA256SUMS
cmp bootstrap-agent.sh /root/vps-status/internal/control/bootstrap-agent.sh
```

所有 7 个发布文件 checksum 通过，Control/Agent 内嵌版本与 commit 校验通过，两个 Linux Agent 架构完成构建。 shell syntax 在逐个文件的检查循环中验证；不能使用 `sh -n file1 file2` 假定所有文件均被检查。

有界端到端回归：5 个 Agent 全部在线、20 个 TCP assignment，主机/探测 series、告警 firing、维护和可写遥测通过，零 dropped writes。该回归不是 100 Agent / 400 TCP、2 vCPU / 2 GiB 的容量基线。

迁移测试从 v1–v8 升级，校验模式 0600 的备份仍是原 schema 且不包含新表；种子管理员身份/摘要/时间在升级与备份保留，v8 Classic 主题也保留。覆盖设置持久化、空值恢复默认、非法地址/版本/字段、缺失/null PUT、取消、认证/CSRF、地址和版本优先级、失败前不发 token。

独立复核修正并回归：浏览器可识别的数字型 IPv4 回环别名（127.1、整数、八进制、十六进制）；镜像 URL 路径含 @ 可用而 userinfo 仍拒绝。可执行 bootstrap 测试模拟 amd64/arm64、下载失败、缺失/重复/错误 checksum、二进制/installer/unit 篡改、未知 OS/架构、非 root、非法版本/base，失败不执行 installer、不打印 token，并清理临时目录。

## 实际浏览器证据

使用独立临时数据 `/tmp/vps-status-install-browser/data`，临时 Control/HTTPS 代理只监听本机。测试代码 `/tmp/vps-status-install-browser/check.mjs`，没有使用真实部署管理员或修改真实凭据。

- localhost + dev：名称输入取得焦点，只有一个输入，没有 Control 地址表单；提示安装设置，命令按钮禁用。
- 从 modal 进入设置：关闭 modal 后设置标题取得焦点；非法 localhost 保存被拒绝。
- 真实保存接入地址/版本，延迟 PUT 时连续触发 submit，只发送一次请求；重启 Control 后配置仍存在。
- 命令实际来自创建 enrollment API，下载 URL 为 GitHub `releases/download/v1.2.3/bootstrap-agent.sh`，参数含已配置远程地址，不含 localhost。
- 实际复制反馈与等待安装状态；使用真实 `/agent/v1/enroll` 消费临时 token，UI 从 pending 更新为 registered，重新生成操作消失。没有模拟远程安装成功或在线上报。
- Escape 关闭并恢复添加节点按钮焦点；令牌没有进入 browser storage；没有未捕获 JavaScript 错误。
- Emerald/Classic × light/dark、中文、320px 设置与对话框均无横向文档溢出；截图视觉检查通过。
- 清空保存地址后，从测试 HTTPS 域名访问，新对话框自动使用该浏览器 origin 且无需填写地址。

截图：`/tmp/vps-status-install-browser/nodes-desktop.png`、`settings-zh-mobile.png`、`blocked-mobile.png`。

## 交付边界

仓库查询时没有 GitHub Releases。本次准备并验证发布产物，没有提交 Git tag、上传 Release、更新真实部署或在真实远端主机安装。可复制 GitHub 命令需要先发布包含独立 bootstrap 的版本；配置格式合法不表示资产存在。旧 Control 因 schema 校验不能直接打开 v9 数据库，回退需恢复升级前备份。主任务容量基线保持未完成，不归档。
