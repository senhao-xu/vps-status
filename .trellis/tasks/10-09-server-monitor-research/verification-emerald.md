# Emerald 与节点安装交付验收

Date: 2026-10-13. Scope: Milestone 7.1，内置 Emerald/Classic 主题管理、节点页面与安装交互；不包含 3D 地球。

## 自动化检查

以下检查全部通过：

```sh
# repository root
go test ./...
go test -race ./...
go vet ./...
gofmt -l cmd internal
git diff --check

# web/; packageManager pnpm 10.18.0
corepack pnpm lint
corepack pnpm typecheck
corepack pnpm test
corepack pnpm build

# root, after the final frontend build
go test -tags webui ./...
go build -tags webui -o /tmp/vps-status-control-emerald ./cmd/control
```

Frontend: 13 test files / 72 tests passed. 无新增运行时依赖；Agent/protocol 不增加远程命令、终端或文件能力。Web 源码无 globe/three/WebGL 依赖，也不在运行时读取本地参考主题目录。

Storage/API tests cover appearance 默认值、关闭/重新打开持久化、非法枚举、认证/CSRF、公开 exact-key DTO，以及 v8 前备份和旧版本拒绝新 schema。Enrollment tests cover pending/registered/online/expired、稳定 node ID、状态不含凭据、消费/撤销并发、已消费记录短期保留及过期清理。

Frontend tests cover DTO decoding、公开敏感字段负面断言、偏好 v1 兼容、加载失败回退、轮询不重叠/取消/有界重试、已消费或记录缺失时不自动生成第二个节点，以及快照刷新中的有界 SSE replay。

## 浏览器交互验收

使用 Chromium 与临时 Vite 服务（127.0.0.1:18181），API 由测试夹具模拟，不读取或修改真实管理员数据。临时脚本为 `/tmp/vps-status-ui-check.mjs` 与 `/tmp/vps-status-ui-fixtures.mjs`；它们依赖本机缓存 Playwright，不属于项目运行时依赖。

已实际检查：

- Emerald 初始主题；分组、搜索、卡片/列表切换；切换视觉主题不丢失筛选。
- 保存 Classic、预览 Emerald 不修改站点值、恢复跟随站点、保存 Emerald。
- PUT 延迟时切换中英文不会触发第二次管理员 GET、不会解锁或重复保存；成功提示使用当前语言。
- 添加节点原生弹窗打开后聚焦名称；填写信息、创建和复制命令；剪贴板不可用时显示手动选择提示。
- pending → registered → online；online 后停止轮询，节点集合刷新并选中新节点。
- Tab 焦点留在弹窗内，Esc 关闭并恢复添加节点按钮焦点。
- 重新生成先撤销旧命令再 POST；撤销后恢复信息表单。
- 320px 管理页、安装弹窗和公开页无水平视口溢出。
- 安装中延迟站点主题加载导致 Emerald → Classic 时，弹窗、命令与安装状态保持；命令未写入 localStorage/sessionStorage。
- 登录页跟随站点 Emerald；公开页不渲染管理员私有主机名；亮/暗及 Classic 页面截图检查。
- 页面没有未捕获 JavaScript 错误；已修正主按钮 hover 对比度。

临时截图位于 `/tmp/vps-status-*.png`，包含节点总览、主题管理、安装流程、节点详情、320px 安装/公开页，以及 Emerald 暗色、Classic 和登录页。截图人工检查完成。

## 复核修正与后续边界

- 快照请求期间最多暂存 512 条 SSE 事件，返回后回放；满时取消/重新加载，不让慢响应覆盖更新状态。
- 主题保存取消旧读取，并通过 provider revision 防止晚到的公共 bootstrap 覆盖保存结果。
- 撤销 404 不证明 token 未消费：可能已清理消费记录，显示检查节点列表的提示，不自动 POST 替代 token。
- 先 showModal 再通过 input ref 聚焦；避免 React autoFocus 与原生 dialog 初始焦点冲突。

未执行真实远端 systemd 安装，未重新部署现有服务，未提交或推送。工作树中既有 release/install 等改动保持不变；本验收不追溯扩大到所有既有改动。整个服务器探针任务仍有 100 Agent / 400 TCP 任务容量里程碑未完成，因此不归档主任务。

回退视觉可选择 Classic；回退旧二进制则必须恢复迁移前 control.db 备份，不能直接用旧程序打开 v8 数据库。
