# 公开首页入口修正验收

Date: 2026-10-13. Scope: `/` 直接显示公开页面，右上角进入 `/admin`；公开节点仍由管理员显式启用及选择白名单。

## 自动化检查

全部通过：

```sh
# web/，使用项目锁定的 pnpm 10.18.0
corepack pnpm lint
corepack pnpm typecheck
corepack pnpm test
corepack pnpm build

# repository root，Go 集成测试需允许本机临时监听端口
go test -tags webui ./...
go build -tags webui -o /tmp/vps-status-control-public-homepage ./cmd/control
git diff --check
```

Frontend: 13 test files / 85 tests passed。路由回归覆盖 `/`、`/public` 及其子路径、`/admin` 及其子路径，以及 `/administrator`、`/admin-tools` 等非后台路径。公开页加载、关闭、空列表、成功状态的入口及中英文文案有断言。

独立代码复核通过；修正一处文件末尾空行，未发现路由认证或公开数据边界问题。

## 浏览器检查

Chromium 访问本机临时 Vite 服务 `127.0.0.1:18182`；公开及后台 API 由夹具模拟，没有修改真实管理员或公开配置。临时检查脚本：`/tmp/vps-status-public-homepage-check.mjs`。

- `/` 显示公开节点；无登录状态及已登录管理员状态下均不请求管理 API。
- 右上角 Management / 管理后台链接进入 `/admin`；未登录显示登录表单，未初始化显示创建管理员表单，已登录进入管理界面。
- 登录页公开链接返回 `/`；后台公开页链接指向 `/`。
- `/public`、`/public/` 兼容；`/admin/nodes` 在认证边界内，`/administrator` 在公开边界内。
- 加载中、503 错误、公开关闭、无节点和正常公开页均保留后台入口。
- 中英文及 Emerald/Classic 下入口可见；320px 无水平视口溢出。
- 无未捕获 JavaScript 错误；桌面和中文手机截图人工复核完成。

截图：`/tmp/vps-status-public-homepage.png`、`/tmp/vps-status-public-homepage-mobile.png`。

## 交付状态

README、任务 PRD/design/implement 和 frontend monitoring spec 已同步。用户已确认将本次修正及文档合并为一个提交。没有修改公开配置默认值、节点白名单、认证 API 或数据库；未部署、未推送本次改动。主任务容量验收仍未完成，不归档主任务。
