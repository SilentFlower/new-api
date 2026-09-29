# 预算 v2 生产部署记录

日期：2026-09-13（Asia/Shanghai）。用户已授权部署，并明确要求触发 new-api 构建。

## 发布结果

| 服务 | 发布版本 | 结果 |
| --- | --- | --- |
| new-api | `build-ad8e96d`，提交 `ad8e96d4d21f05179298325f5ffb015b9432ba1a` | amd64、arm64 与多架构清单构建成功；生产容器健康，公网状态接口返回新版本 |
| ai-fund Worker | `51cf4603-64a0-4eb6-a3eb-66393c451371` | `ai-hub` 部署成功，既有定时任务与队列绑定生效 |
| ai-fund Pages | `132085ae`，源码提交 `5e88b88a4fe564c0c0d63d12aa4ef6ac3c03bde9` | 发布到 `ai-hub-all` 的生产分支 `main`；生产 HTML 与相关 JS/CSS 均与本次构建逐字节一致 |

- 构建记录：https://github.com/SilentFlower/new-api/actions/runs/34720795785
- 镜像：`ghcr.io/silentflower/new-api:build-bak-latest`，部署前核对 OCI revision 与目标提交完全一致。
- new-api：https://ai.flower-cli.com
- 门户：https://ai.hub.flower-cli.com
- Worker：https://ai-api.hub.flower-cli.com
- Pages 本次部署：https://132085ae.ai-hub-all.pages.dev

new-api 使用带 `[build]` 的空提交触发既有构建流程，未修改业务文件。部署顺序为 new-api → Worker → Pages。

## 迁移与用量核验

- `main.flower-cli.com` 上的 `/root/new-api` 是本次生产部署目录；Compose 仅重建 `new-api` 服务。
- 启动迁移：`migrated=2 skipped=23 failed=0 overrides=0`。
- 渠道 52：策略 revision 10、schema 2，共 3 条预算；渠道 80：revision 5、schema 2，共 4 条预算。
- 使用生产既有管理凭据完成两个渠道的策略读取及全部 7 条预算用量 GET，均返回对应预算身份与 Redis 存储模式。
- 迁移前后核对 52 个 Redis 用量键、312 个计数值：键缺失 0、计数下降 0。
- 旧个人覆盖表共 3 行，其中唯一有额度覆盖值的记录已经过期；因此本次新增有效预算提额记录为 0，符合迁移规则。
- Worker 新预算路由的匿名访问返回 401；本次未执行带门户登录态的完整 UI → BFF → new-api 写入场景，也未调用真实模型供应商。此记录不替代父任务其余集成验收。

## 回滚保留

生产服务器备份目录：`/root/new-api/backups/budget-v2-20260912T214946Z`，目录与文件仅供 root 访问。

- 备份旧三表：`channel_period_policies`、`channel_user_limit_overrides`、`channel_user_period_overrides`；升级前新表 `channel_user_budget_overrides` 不存在。
- SQL 备份 6115 字节，SHA-256：`ab66357526d68631f3877a3a6c38711603e053c0bf25a83a6b2991381f8f1466`。
- 旧镜像：`ghcr.io/silentflower/new-api:rollback-budget-v1-3d2d3b2-20260913`。
- 备份目录同时保存原 Compose 配置、容器信息、目标镜像信息及迁移/API/Redis 核验记录。
- ai-fund 前一 Worker 版本：`01ab105b-5017-41c7-b576-b5307366a0cf`；前一 Pages 部署：`c227969b-eb2e-44c3-88c2-0b80c929f0fd`。
- 如需回退 new-api，必须同时恢复旧预算表并恢复新表的升级前不存在状态；仅回退镜像不足以兼容 v2 配置。按子任务 `research/rollback-drill.md` 执行。

## 前端交付

发布前生产构建通过，源码、配置及实际构建产物的公网 API 地址检查通过。核验入口 HTML、主 JS/CSS、号池页面和预算管理弹窗资源与本地构建一致。部署前已打开的浏览器页面需要刷新一次。

本次 ai-fund 使用 auto-loop 已完成的本地提交发布；未额外推送其 `master` 分支，也未归档任务。
