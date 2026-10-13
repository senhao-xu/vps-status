# 安全模块修改密码验收

Date: 2026-10-13. Scope: 管理后台安全模块添加修改密码；验证当前密码、设置并确认新密码，成功撤销所有管理员会话并提示重新登录，沿用既有密码规则和 MFA 设置。

## 自动化检查

以下检查通过：

```sh
# web/，pnpm 10.18.0
corepack pnpm lint
corepack pnpm typecheck
corepack pnpm test
corepack pnpm build

# repository root，集成测试需允许临时端口与 Go 缓存写入
go test ./...
go test -race ./...
go vet ./...
go test -tags webui ./...
go build -tags webui -o /tmp/vps-status-control-password-test ./cmd/control
gofmt -l cmd internal
git diff --check
```

Frontend: 14 test files / 88 tests passed。独立复核修正 `AuthGate.refreshSession`：仅当前仍已认证且会话 ID 一致才应用刷新响应，避免晚到的旧会话响应恢复改密后已卸载的后台。修正后 lint/typecheck 和最终生产构建通过，浏览器回归验证该修正。

HTTP/存储测试覆盖：认证、CSRF、同源检查、独立限速、缺失/null/额外字段、当前密码错误、空/短/长新密码、cookie 清除、所有旧会话失效、旧密码拒绝/新密码登录、MFA 与恢复码保留、过期认证快照、事务回滚、并发改密唯一成功，以及旧密码并发登录不能产生存活会话。

## 实际浏览器验证

使用编译后的嵌入 Web Control，独立临时数据库 `/tmp/vps-status-password-browser-data-20261013`，通过仅本机的临时 HTTPS 代理验证 Secure cookie 完整流程。没有读取或修改部署中的真实管理员密码。

临时脚本 `/tmp/vps-status-password-browser-check.mjs` 与 `/tmp/vps-status-password-test-proxy.mjs`；Control 为 `/tmp/vps-status-control-password-test`。

- 两个独立浏览器上下文使用临时管理员登录。
- 新密码确认不一致：前端提示，无改密请求。
- 当前密码错误：真实 API 返回 `400`，两个会话仍有效。
- 延迟改密请求时重复触发 submit：只有一次请求；处理中切换语言、主题及 320px 布局正常。
- 改密真实 API 返回 `204`，当前后台/SSE 卸载并出现中文成功提示；切换英文后提示仍在。
- 第二浏览器会话及当前旧会话均为 `401`；旧密码登录拒绝，新密码通过表单登录成功。
- 再次将临时密码设为空：确认校验、改密和空密码重新登录成功，与既有 setup 规则一致。
- MFA confirm 响应使用夹具，仅用于触发会话刷新；真实旧 `GET /session` 响应延迟至改密完成后返回，不再恢复后台或覆盖提示。MFA 保留由后端真实存储/API 测试覆盖。
- 密码没有进入 localStorage/sessionStorage，无未捕获 JavaScript 错误；手机截图人工检查通过。

截图：`/tmp/vps-status-password-form-mobile.png`。

## 交付边界

无需数据库迁移，没有新增 CLI 密码重置接口；公开页和节点凭据不变。部署中的实际密码仅在管理员自己使用新表单后才改变。主任务容量基线仍未完成，不归档。提交及部署结果另记 developer journal。
