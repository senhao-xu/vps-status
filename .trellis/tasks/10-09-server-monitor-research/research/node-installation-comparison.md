# 节点接入与安装入口对比

调研日期：2026-10-14。用户要求针对 Control 地址与安装命令参考 Komari 和 monitor；本次读取官方源码，未修改业务代码。

## 源码版本与证据

- Komari Web：`9d2c144fcbde53858e175ecd70287fa6428048ef`。
  - [安装命令生成](https://github.com/komari-monitor/komari-web/blob/9d2c144fcbde53858e175ecd70287fa6428048ef/src/pages/admin/index.tsx#L1581)：优先读取 `settings.script_domain`，为空时使用 `window.location.origin`；面板地址作为 `-e` 参数传给 Agent。
  - [GitHub 脚本入口](https://github.com/komari-monitor/komari-web/blob/9d2c144fcbde53858e175ecd70287fa6428048ef/src/pages/admin/index.tsx#L1666)：从 `raw.githubusercontent.com/komari-monitor/komari-agent/refs/heads/main/install.sh` 下载，可配置 GitHub 代理。
  - [站点配置](https://github.com/komari-monitor/komari-web/blob/9d2c144fcbde53858e175ecd70287fa6428048ef/src/pages/admin/settings/site.tsx#L158)：在站点设置保存 `script_domain`，不是每个节点都要求填写。
- Komari Agent：`828afafe7fb5b1b58c1232094e1e46320719016e`。
  - [Release 下载](https://github.com/komari-monitor/komari-agent/blob/828afafe7fb5b1b58c1232094e1e46320719016e/install.sh#L359)：默认使用最新版本，可通过 `--install-version` 指定版本，通过 GitHub Release 下载二进制。
- monitor：`c532d54843b87f8001f8387a7acdf1d440b23f40`。
  - [接入地址来源](https://github.com/monitor-probe/monitor/blob/c532d54843b87f8001f8387a7acdf1d440b23f40/web-admin/src/App.tsx#L192)：使用服务端返回的 `site`，为空时使用 `location.origin`；服务端 `site` 来自启动参数 `--site`。
  - [命令生成](https://github.com/monitor-probe/monitor/blob/c532d54843b87f8001f8387a7acdf1d440b23f40/web-admin/src/components/Admin.tsx#L1200)：命令下载面板自身 `/install.sh`，通过 `--server` 传入面板地址。
  - [二进制中转](https://github.com/monitor-probe/monitor/blob/c532d54843b87f8001f8387a7acdf1d440b23f40/install.sh#L234)：节点请求面板 `/agent/{arch}`；面板从 `monitor-probe/agent` GitHub 最新 Release 获取二进制。
  - [本地地址处理](https://github.com/monitor-probe/monitor/blob/c532d54843b87f8001f8387a7acdf1d440b23f40/src/api.rs#L772)：安装要求 HTTPS 域名；通过本地隧道访问时必须配置节点可达的 `--site`，不能直接生成指向 localhost 的命令。

## 对照当前实现

| 项目 | 接入地址 | 脚本下载 | Agent 下载 |
| --- | --- | --- | --- |
| Komari | 全局 `script_domain`，否则当前页面 origin | GitHub Raw，可用代理 | GitHub Release，默认 latest，可指定版本 |
| monitor | `--site`，否则当前页面 origin | 面板 `/install.sh` | 面板中转 GitHub Release |
| VPS Status 当前 | 每次添加节点的 `control_url`，初始为当前页面 origin | Control `/install-agent.sh` | 节点直接从固定版本 GitHub Release 下载 |

Control 地址是 Agent 的注册与上报目的地；GitHub 是软件分发地址，两者都可能出现在同一安装命令中。使用面板脚本地址是 monitor 的明确设计，不能单凭 URL 判断安装方式错误。

当前的实际体验缺口：

1. 没有统一配置节点可达的接入地址，用户每次添加节点都看到 Control 地址输入框。
2. 通过 localhost 打开面板时，默认生成的地址不能用于其他服务器。
3. 源码和 Compose 默认版本为 `dev`，安装会请求 GitHub `releases/download/dev`；需要已发布版本或显式设置 `--agent-release-version`。
4. Release 里的 `deploy/systemd/install-agent.sh` 接受本地 Agent 二进制路径，不是独立的联网引导脚本，不能直接替换当前下载 URL。

## 建议方案（待实施）

- 将“节点接入地址”作为统一配置：配置值优先，其次使用有效的当前 HTTPS 页面地址。常规添加节点只填写名称。
- 从 localhost / 回环入口访问而又没有可供远程节点使用的地址时，提示配置接入地址，避免生成容易误用的远程安装命令。保留本机开发安装的显式途径。
- 采用用户期望的 GitHub 下载入口时，新增独立的联网引导脚本，并将其纳入 Release；命令传入面板地址、一次性注册令牌和明确的 Agent 版本。
- 保留现有固定版本、HTTPS 下载、SHA-256 校验、非 root Agent 服务与令牌只展示一次的约束。
- 引导脚本优先引用版本化 Release，避免跟随 main 的变更；需要面板中转下载的网络环境可作为后续独立需求，不在本次默认增加。
- `dev` 构建缺少已发布 Agent 版本时，应明确显示安装配置缺失，不能生成看似可用的 GitHub `dev` 下载命令。

以上是源码对比与建议，业务实现、发布和部署均未执行。
