# v1.0.0-rc.24 合并台账

## 合并基线

- 目标分支：`build-bak`
- 合并前 HEAD：`63c77d5ad150cfd3a16a64389116443ad355f5ac`
- 来源标签：`v1.0.0-rc.24`
- 来源提交：`5c3abffe8572aa8a49f15c3916707d2019d66af4`
- 共同祖先：`7c28993f6bd9e92616f3f578212577f8b7c40b45`
- 双方独有提交：`build-bak=250`，`rc.24=77`
- 官方远端标签核验：annotated tag `21ee1f565b47663b2d9f791c0ddf593b096ebfe2`，解引用提交与来源提交一致。
- 备份分支：`backup/build-bak-before-rc24-merge-20260814`，已核验指向合并前 HEAD。
- 合并状态：真实 `--no-ff --no-commit` merge，`MERGE_HEAD` 精确等于来源提交，等待 Trellis 提交门禁创建 merge commit。

## 冲突统计

- 总计：96 个未合并路径。
- 类型：47 UU、28 UD、18 AU、3 AA。
- 顶层分布：`web` 66、`relay` 12、`controller` 4、`i18n` 4、`relaykit` 3，其余分别位于 `service`、`model`、`middleware`、`dto`、`go.mod`、`go.sum`、`.gitattributes`。
- 当前结果：`git ls-files -u` 为空，变更文件中未发现标准冲突标记。

## 冲突处理记录

| 分区 | 代表路径 | 双方意图 | 处理策略 | 验证证据 |
|---|---|---|---|---|
| 前端结构 | `web/`、`web/default/`、`web/classic/` | rc.24 扁平化 Default 并移除 Classic；build-bak 保留 Default 定制能力 | 采用唯一 `web/` 构建入口，迁移消息审计、泄漏扫描和公共日志等定制模块，完整删除旧目录 | `bun run typecheck`、`bun test --parallel=1`、`bun run build` 通过；旧目录不存在 |
| 认证会话 | `middleware/auth.go`、`controller/auth_session.go`、`model/user_session.go` | rc.24 JWT/会话控制；build-bak PAT、API Key 日志与旧 header 兼容 | JWT/会话作为主链，非内部令牌回退 `ValidateAccessToken`；公共日志继续使用 `TokenAuthReadOnly` | `TestAdminAuthAllowsPATWithLegacyNewAPIUserHeader`、`TestTokenAuthReadOnlyAllowsAIKeyLogQuery` 通过 |
| RelayKit/provider | `relaykit/`、`relay/`、`controller/relay*.go` | rc.24 公共 relay 抽取与新通道；build-bak Search、Compact、视觉辅助、WebSearch | 接受 RelayKit 新结构，在新 DTO、转换器和 provider 入口恢复定制协议 | 根模块、`relaykit` 全量测试及协议定向测试通过 |
| 计费 | `relay/helper/price.go`、`relay/common/relay_info.go`、`service/tiered_settle.go` | rc.24 tiered billing；build-bak 映射后上游模型计费与安全审计 | 同时保留 BillingModel 冻结、tiered snapshot、checked quota、重试与日志结算 | `price_test.go`、`billing_model_test.go`、`text_quota_test.go`、全量测试通过 |
| HTTP | `common/http_client.go`、`common/body_storage.go`、`relay/channel/api_request.go` | rc.24 HTTP/2/transport；build-bak body replay、重试与 keepalive | 统一使用可重放 body 和新 transport，保留非流式 JSON keepalive 的响应边界 | body replay、redirect、retry、keepalive 测试及 race 检查通过 |
| 数据库与其他定制 | `model/`、`controller/`、`service/`、`router/` | rc.24 schema/session/task；build-bak 审计、泄漏扫描、并发、导出等 | 采用 rc.24 schema，定制字段与独立模块回填；锁继续使用 `lockForUpdate` 并保留三方言分支 | model/controller/service/router 全量及定向测试通过 |

## 定制功能保留矩阵

| 能力 | 合并后入口 | 冲突处理与状态 | 验证证据 |
|---|---|---|---|
| 消息审计与 AI 审核 | `controller/message_audit.go`、`service/message_audit*.go`、`model/message_audit*.go`、`web/src/features/message-audits/` | 迁移到扁平前端并保留清理、会话推断、AI 重审与安全 Tool 降级 | model/service/relay/router 定向测试；前端消息审计测试通过 |
| GitHub 密钥泄漏扫描 | `controller/token_leak_scan.go`、`service/token_leak_*.go`、`model/token_leak_scan.go`、`web/src/features/token-leak-scan/` | 保留任务互斥、身份锚点、通知幂等、禁用与脱敏 | controller/service/router 定向测试；前端 5 个扫描逻辑测试通过 |
| 通道级用户并发限制 | `controller/channel_user_concurrency.go`、`service/channel_user_concurrency.go`、`relay/channel_user_concurrency.go` | 保留配置校验、内存/Redis 租约、续租、取消与 fail-closed | controller/service/model 定向测试及 race 检查通过 |
| Responses Compact V1/V2/WebSocket | `relay/responses_compact_*.go`、`controller/responses_compact_passthrough_websocket.go`、`middleware/responses_compact_detection.go` | 适配 RelayKit 和 rc.24 路由，保留原始 body、SSE bridge、WS、计费退款和安全 header | controller/middleware/relay/helper/provider 测试通过 |
| Alpha Search | `controller/relay_alpha_search.go`、`relay/alpha_search_handler.go`、`relaykit/dto/alpha_search_request.go` | 保留 standalone 路径、字段透传、安全 header 和独立 WebSearch 计费 | router/controller/relay/helper/provider 测试通过 |
| 视觉辅助 | `relay/vision_assist.go`、`service/vision_assist.go` | 保留多协议图片抽取、缓存、并发、重试、失败策略、改写与审计 | service/relay/controller error-log 测试通过 |
| Claude WebSearch 模拟 | `relay/claude_websearch_emulation.go`、`relay/websearch/`、`controller/channel_websearch_setting.go` | 保留纯 WebSearch 识别、provider 调用、API Key 继承/清空与返回脱敏 | relay/websearch、Claude handler、controller channel 测试通过 |
| 映射后上游模型计费 | `relay/helper/model_mapped.go`、`relay/helper/price.go`、`relay/common/billing_model.go` | 映射完成后冻结 BillingModel，并与 tiered snapshot、重试和结算共同生效 | `TestModelPriceHelperUsesMappedUpstreamModelWhenChannelSettingEnabled` 等定向测试通过 |
| 公共 API Key 日志 | `router/api-router.go`、`middleware/auth.go`、`controller/log_public.go`、`web/src/features/token-logs/`、`web/src/routes/log.tsx` | 保留 `/api/log/token*` 只读鉴权与公共 `/log` 页面 | `TestTokenAuthReadOnlyAllowsAIKeyLogQuery`、前端构建与路由生成通过 |
| 控制台 Excel 导出与筛选 | `controller/dashboard_export.go`、`model/dashboard_export.go`、`web/src/features/dashboard/components/models/dashboard-export-dialog.tsx` | 保留时间、group、token 重复 query key、统计与 Excel 下载 | `TestExportQuotaDataExcelIncludesGroupsAndPreservesTokenStatistics`、dashboard filter 测试通过 |
| Token 迁移 | `controller/token_migrate.go`、`model/token_migrate.go`、`web/src/features/keys/components/api-keys-migrate-to-accounts-dialog.tsx` | 保留 Root 批量迁移、逐项结果、所有权校验与前端入口 | controller/model token migrate 测试通过 |
| User-Agent 日志 | `model/log_user_agent.go`、`service/log_info_generate.go`、`web/src/features/usage-logs/components/dialogs/request-user-agent-detail.tsx` | 保留采集、存储、普通用户脱敏和管理员展示 | `model/log_user_agent_test.go` 与全量测试通过 |
| 非流式 JSON keepalive | `relay/helper/non_stream_keepalive.go`、`setting/operation_setting/general_setting.go`、前端系统设置 | 保留允许列表、串行 writer、取消、错误与最终 JSON 语义 | `relay/helper/non_stream_keepalive_test.go`、race 检查、生产构建通过 |

## ai-fund 兼容结论

- `/root/project/ai-fund/worker/src/rankings.js` 的管理员调用继续使用 `Authorization: Bearer <PAT>`，新认证链在 JWT 不匹配时回退到 `model.ValidateAccessToken`。
- 旧客户端携带 `New-Api-User` 不参与新认证判断，也不会导致请求失败；兼容测试使用非数字旧 header 验证通过。
- `/root/project/ai-fund/frontend/src/views/Logs.vue` 的 API Key Bearer 调用继续命中 `/api/log/token`、`/stat`、`/data` 的 `TokenAuthReadOnly`。
- 不需要修改 `/root/project/ai-fund`。

## 验证结果

| 类别 | 命令/证据 | 结果 |
|---|---|---|
| Git 范围 | `MERGE_HEAD` 与 `v1.0.0-rc.24^{commit}` 比对；备份分支比对；post-tag 19 个提交审计 | 通过，精确停在 rc.24，post-tag 提交未进入合并前分支 |
| 冲突 | `git ls-files -u`、冲突标记扫描、`git diff --check` | 通过，0 个未合并条目、0 个冲突标记、0 个 whitespace error |
| Go 格式 | 对变更 Go 文件执行 `gofmt` | 通过 |
| Go 测试 | 根模块 `go test ./...` | 通过 |
| RelayKit 测试 | `cd relaykit && go test ./...` | 通过 |
| Go 静态检查 | 根模块与 `relaykit` 的 `go vet ./...` | 通过 |
| Race | `go test -race ./middleware ./controller ./relay -run 'Auth|Concurrency|Keepalive|ResponsesCompact|WebSocket|Retry' -count=1` | 通过 |
| 定制能力 | model/controller/middleware/service/relay/router R4 定向测试 | 通过 |
| 前端依赖 | `bun install --frozen-lockfile` | 通过 |
| 前端测试 | `bun test --parallel=1` | 198 通过，0 失败 |
| 前端类型 | `bun run typecheck` | 通过 |
| 前端格式/版权 | `bun run format:check`、`bun run copyright:check` | 通过 |
| 前端 lint | `bun run lint` | 通过，0 error；保留仓库现有 warning |
| i18n | `bun run i18n:sync` 与 `_sync-report.json` | 7 个 locale 均为 0 missing、0 extra、0 untranslated |
| 前端构建 | `bun run build` | 通过 |

## 剩余风险与提交后检查

- 当前尚未创建 merge commit，因此 `git merge-base --is-ancestor <rc24> build-bak` 需在提交后执行最终确认；当前 `MERGE_HEAD` 已精确证明待提交第二父提交为 rc.24。
- 未使用真实生产凭据对 `ai-fund` 发起只读联调；兼容结论基于调用源码、认证数据流和自动化契约测试，避免读取或打印密钥。
- 前端 lint 仍输出 rc.24/现有代码的非阻塞 warning，但无 lint error，且类型、测试和生产构建均通过。
