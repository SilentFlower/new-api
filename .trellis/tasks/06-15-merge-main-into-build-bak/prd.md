# 合并 main 到 build-bak 并解决冲突

## Goal

将本地 `main` 分支合并到当前 `build-bak` 分支，并逐个解决 Git 冲突，使 `build-bak` 能吸收 `main` 的最新代码、前端目录迁移和依赖更新，同时保留 `build-bak` 上已有的业务功能改动。

## Background / Known Context

- 当前分支是 `build-bak`。
- 工作区存在一个未跟踪文件 `.trellis/workflow.md.bak`，本任务不处理该文件。
- 只读模拟显示合并 `main` 到 `build-bak` 有 8 个冲突项。
- `main` 已将旧前端 `web/src` 迁移到 `web/classic/src`，因此部分 `build-bak` 新增文件需要落到 `web/classic/src` 对应位置。
- 受项目政策保护的 `new-api` 与 `QuantumNous` 相关品牌、归属、元数据和版权信息不得删除或替换。

## Requirements

- 执行 `main` 到 `build-bak` 的本地合并。
- 解决以下内容冲突：
  - `.gitignore`
  - `AGENTS.md`
  - `docker-compose.yml`
  - `model/token.go`
  - `web/classic/src/components/layout/PageLayout.jsx`
  - `web/classic/src/i18n/locales/zh-CN.json`
- 解决以下文件位置冲突：
  - `web/src/components/dashboard/modals/ExportModal.jsx` 应迁移到 `web/classic/src/components/dashboard/modals/ExportModal.jsx`
  - `web/src/components/table/tokens/modals/MigrateToAccountsModal.jsx` 应迁移到 `web/classic/src/components/table/tokens/modals/MigrateToAccountsModal.jsx`
- 冲突解决应优先保留两边有效改动：`main` 的结构迁移和新代码，以及 `build-bak` 上的导出、令牌迁移等业务改动。
- 不做与合并冲突无关的重构、格式化或功能变更。
- 不处理或删除未跟踪文件 `.trellis/workflow.md.bak`，除非后续用户明确要求。

## Acceptance Criteria

- [ ] `git merge main` 不再处于冲突状态。
- [ ] `git status` 不再显示 unmerged paths。
- [ ] 上述 8 个冲突项均已逐个处理，并能说明处理策略。
- [ ] 前端迁移后的新增文件位于 `web/classic/src` 对应路径，旧 `web/src` 冲突残留被清理。
- [ ] 受保护项目标识未被删除或替换。
- [ ] 对受影响 Go 代码运行针对性格式化或检查；对前端冲突文件进行基本静态检查。

## Notes

- 这是轻量任务，PRD-only 足够。
- 本任务不包含提交、推送或归档。
