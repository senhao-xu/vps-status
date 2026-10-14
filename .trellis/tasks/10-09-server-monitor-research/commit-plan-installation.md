# 提交计划：统一节点接入与页面排版

实现和验证已完成；用户已明确批准“提交推送吧”，授权按下列计划提交并推送。

## 1. feat: unify node installation and refine admin layouts

本次代码、回归测试、CI/发布资产、用户文档、规范与任务验收作为一个完整功能提交。

文件清单：

- `.github/workflows/release.yml`
- `.trellis/spec/backend/agent-protocol-guidelines.md`
- `.trellis/spec/backend/database-guidelines.md`
- `.trellis/spec/backend/index.md`
- `.trellis/spec/backend/installation-guidelines.md`
- `.trellis/spec/backend/operations-guidelines.md`
- `.trellis/spec/frontend/monitoring-ui-guidelines.md`
- `.trellis/tasks/10-09-server-monitor-research/check.jsonl`
- `.trellis/tasks/10-09-server-monitor-research/commit-plan-installation.md`
- `.trellis/tasks/10-09-server-monitor-research/design.md`
- `.trellis/tasks/10-09-server-monitor-research/implement.jsonl`
- `.trellis/tasks/10-09-server-monitor-research/implement.md`
- `.trellis/tasks/10-09-server-monitor-research/prd.md`
- `.trellis/tasks/10-09-server-monitor-research/research/node-installation-comparison.md`
- `.trellis/tasks/10-09-server-monitor-research/verification-installation-layout.md`
- `README.md`
- `api/README.md`
- `deploy/README.md`
- `.github/workflows/ci.yml`
- `internal/control/agent_install.go`
- `internal/control/agent_install_test.go`
- `internal/control/api.go`
- `internal/control/api_test.go`
- `internal/control/bootstrap-agent.sh`
- `internal/control/installation_api.go`
- `internal/control/installation_api_test.go`
- `internal/control/node_api.go`
- `internal/control/node_api_test.go`
- `internal/storage/control/installation.go`
- `internal/storage/control/installation_test.go`
- `internal/storage/control/store_test.go`
- `migrations/control/009_installation.sql`
- `scripts/release.sh`
- `tests/acceptance.md`
- `web/src/api/contracts.ts`
- `web/src/api/installation.test.ts`
- `web/src/features/admin/AdminDashboard.tsx`
- `web/src/features/admin/EnrollmentDialog.tsx`
- `web/src/features/admin/InstallationPanel.test.tsx`
- `web/src/features/admin/InstallationPanel.tsx`
- `web/src/features/admin/NodesPanel.tsx`
- `web/src/features/admin/installationAddress.test.ts`
- `web/src/features/admin/installationAddress.ts`
- `web/src/i18n.tsx`
- `web/src/styles.css`

## 未识别改动

无。本次所有脏文件均由主代理或本次实现/复核代理产生；此前创建的接入对比记录也纳入同一功能。生成产物、临时数据库/浏览器脚本/发布包均不提交。

主任务容量基线仍未完成，不归档。本次授权包括提交与推送，不包括发布 GitHub Release 或部署。
