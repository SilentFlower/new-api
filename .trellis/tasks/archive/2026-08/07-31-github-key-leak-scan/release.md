# Release Operations

## Conclusion

Needs human review.

代码、数据库迁移入口、管理 UI、GitHub 公共代码扫描和通知链路已经实现并提交；目标服务器也已注入运行凭据并完成真实令牌全量扫描。受控假 Key 精确命中、finding 持久化、站内通知、钉钉告警、失败场景和完整回滚尚无生产全链路验收证据，不能把当前状态视为全部上线门禁已通过。

## Evidence Checked

- `task.json`、`brief.md`、`prd.md`、`design.md`、`implement.md`
- `implement.jsonl`、`check.jsonl`
- 业务提交 `19300bf72` 与任务记录提交 `8445cef62`
- 当前 Git 状态与 `build-bak` / `origin/build-bak` 同步状态
- 当前会话中的目标服务器容器、环境配置、GitHub Code Search 和系统任务只读验证结果

## Drift Check

Missing release.md. 当前任务记录仍写明生产部署与受控假 Key 验收未执行，但本次会话已在用户授权后完成镜像重建、四项环境变量注入和两轮 94 个真实用户 Key 的全量扫描。任务记录与实际外部系统状态存在漂移，需要人工复核并以本文件的上线事项为准。

## SQL Changes

- 无手工 SQL。
- 应用启动时通过 GORM AutoMigrate 创建或更新 `token_leak_scan_states`、`token_leak_findings`、`token_leak_notifications`，并扩展 `system_tasks` 使用新的任务类型。
- 上线前后需确认 SQLite、MySQL 和 PostgreSQL 的迁移路径保持兼容；生产目标已观察到 MySQL 8.2 正常创建并写入扫描状态。

## Configuration Changes

- 在 new-api 进程环境中配置 `GITHUB_TOKEN_LEAK_SCAN_TOKEN`。
- 配置至少 32 字节的 `GITHUB_TOKEN_LEAK_SCAN_SECRET`。
- 配置 `DINGTALK_TOKEN_LEAK_WEBHOOK_TOKEN`。
- 钉钉机器人启用加签时配置 `DINGTALK_TOKEN_LEAK_WEBHOOK_SECRET`。
- 管理后台配置 `token_leak_scan.enabled` 和 1～168 小时的 `token_leak_scan.interval_hours`。
- 生产 GitHub 凭据应为独立、短有效期、无额外仓库权限的 fine-grained PAT。当前实际凭据来自 GitHub CLI OAuth，包含 `repo`、`workflow`、`gist` 和 `read:org` 宽权限，正式长期运行前必须轮换。
- 钉钉机器人凭据曾在交互会话中明文提供，正式长期运行前必须在钉钉侧轮换并同步更新服务器环境变量。

## Batch / Deployment Scripts / Data Repair

- 发布包含业务提交 `19300bf72` 的镜像并重建 new-api 容器，确认健康检查通过。
- 首次启用会立即创建周期扫描任务；不要同时手动触发全量扫描，避免连续执行两轮。
- 当前生产记录显示手动任务和首次周期任务各扫描 94 个未软删除用户 Key，均为 `processed=94`、`failed=0`、`incomplete=0`、`found=0`。
- 不需要数据修复脚本；关闭功能后保留扫描状态、finding 和通知审计。

## External Systems / Dependent Platforms

- GitHub Code Search：验证专用凭据可调用全局代码搜索；查询不带 `repo:`、`user:` 或 `org:` 限定，私有候选由应用拒绝。
- 钉钉自定义机器人：需要验证加签、Markdown、`@所有人`、失败重试与脱敏错误码。当前只确认凭据已注入，尚未通过真实 finding 完成消息验收。
- 公网入口：`www.havefun.eu.cc` 当前返回 `fq.747698.xyz` 的 TLS 证书，证书与域名不匹配。修复反向代理和证书前，外部处置链接与未来跨服务器 Worker API 不应视为可安全使用。

## Release Order

1. 创建并注入最小权限 GitHub PAT、HMAC 密钥和已轮换的钉钉机器人凭据。
2. 发布镜像并保持 `token_leak_scan.enabled=false`，确认数据库迁移、容器健康和凭据布尔状态。
3. 修复公网 TLS 证书与反向代理。
4. 使用受控假 Key 完成 GitHub 候选下载、本地完整匹配、finding、站内通知和钉钉告警全链路验收，并清理测试数据。
5. 验证鉴权失败、限流、超时、任务互斥、关闭开关和日志脱敏。
6. 验收通过后开启周期任务；首次启用期间不要重复手动发起全量扫描。

## Rollback Notes

1. 在管理后台关闭 `token_leak_scan.enabled`，确认活动任务停止继续发起外部请求。
2. 移除四项泄露扫描环境变量并重建 new-api 容器。
3. 必要时回退到不包含 `19300bf72` 的上一镜像。
4. 保留新增表和审计历史，不执行破坏性删表。
5. 验证原有 API 转发、用户 Key 鉴权、计费、Redis 和数据库连接正常。

## Post-release Verification

- 管理页面四项凭据均显示已配置，GitHub 鉴权状态在实际扫描后为正常。
- 受控假 Key 能生成唯一 finding，并同时触发 root、所属用户和钉钉告警；消息不包含完整 Key 或 16 位锚点。
- 重复扫描不重复通知；处置后再次重开使用新的通知代次。
- 私有或可见性未知候选不会下载或产生 finding。
- 扫描任务遵守每分钟最多 8 次 Code Search 请求，关闭开关后停止新增请求和写入。
- 生产日志、任务 payload/state/result、数据库和通知审计均不包含 Key、锚点、PAT 或 Webhook URL。
- 修复后的 `https://www.havefun.eu.cc` 证书与域名匹配，安全告警处置链接可正常访问。
