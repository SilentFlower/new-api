# Build 特有 Feature 盘点与首批审计

## 对比基线

- 当前分支：`build-bak`。
- 上游基线：`origin/main...build-bak`。
- 排除 Trellis、Agent 和本地工作流文件后，当前业务差异约为 196 个文件、`+26324/-1343`。
- 该差异同时包含新增独立文件、上游文件接入点、历史功能修复和当前上游尚未包含的完整功能，不能把所有差异都视为待重构代码。

## 当前 Feature 分组

| 分组 | 已确认的 build 特有能力 | 当前判断 |
| --- | --- | --- |
| Relay 协议与转发 | Responses Compact V1/V2/WS、Alpha Search、Claude WebSearch、视觉辅助、非流式保活、Anthropic effort 日志 | 优先逐项审计；协议与计费回归风险最高 |
| Relay 计费与模型 | 映射后上游模型计费、Compact 基础模型计费、纯工具调用计费、缓存 Token 统计 | 与共享计费链路耦合，必须按行为契约分批治理 |
| 管理端与 Dashboard | Excel 导出、分组与令牌筛选、公共日志统计联动、Token 日志页 | 前后端跨层功能，后续单独分批审计 |
| Token 管理 | Token 迁移到独立账号及相关管理界面 | 独立业务域，后续单独审计 |
| 构建与工作流 | Docker 构建触发、Trellis/Flower 工作流 | 不作为业务 Feature 重构范围，只记录同步风险 |

## 当前有效 Feature 清单

下表只列当前代码仍存在的业务能力。历史构建触发、任务归档提交和已被后续实现覆盖的中间修复不单独作为 Feature。

| Feature | 当前代码证据 | 治理状态 |
| --- | --- | --- |
| Responses Compact / Alpha Search / Responses WebSocket | `relay/responses_compact_passthrough.go`、`relay/alpha_search_handler.go`、`controller/responses_websocket.go`、`router/relay-router.go:81`、`router/relay-router.go:109` | 首批治理：协议实现已独立，核心 Relay/Distributor 接入面仍过大 |
| 渠道级视觉辅助 | `service/vision_assist.go:91`、`relay/vision_assist.go:42`、`dto/channel_settings.go:22` | 当前有效；独立模块较完整，后续审计 Relay 准备、错误日志和前端挂载点 |
| Claude Code WebSearch | `relay/websearch/`、`relay/claude_handler.go`、`service/tool_billing.go` | 当前有效；provider 已独立，后续审计 Claude 主链路、渠道设置和计费接入 |
| 非流式 JSON 保活 | `relay/helper/non_stream_keepalive.go`、`controller/relay.go:143` | 当前有效；核心逻辑独立，后续审计 Relay 启停接入和 writer 生命周期 |
| 映射后上游模型计费 | `dto/channel_settings.go` 的 `use_upstream_model_for_billing`、`relay/common/relay_info.go`、`relay/helper/price.go` | 当前有效；涉及共享计费模型解析，后续单独审计 |
| Anthropic effort 与缓存 Token 统计 | `relay/claude_handler.go`、`service/log_info_generate.go`、`service/text_quota.go` | 当前有效；属于日志/计费兼容能力，后续按独立契约审计 |
| Claude count_tokens 透传 | `controller/claude_count_tokens.go:49`、`router/relay-router.go:89` | 当前有效；控制器与路由已独立，后续确认共享 Header/错误处理接入是否最薄 |
| Dashboard 筛选与 Excel 导出 | `controller/usedata.go:93`、`web/default/src/features/dashboard/`、`web/classic/src/components/dashboard/modals/ExportModal.jsx` | 当前有效；跨后端和两套前端，后续作为独立批次治理 |
| 公共 API Key 日志查看 | `web/default/src/features/token-logs/`、`web/default/src/routes/log.tsx`、`web/classic/src/pages/LogViewer/index.jsx:138` | 当前有效；前端模块较独立，后续审计路由、API 和公共访问边界 |
| Token 迁移到独立账号 | `controller/token_migrate.go`、`model/token_migrate.go`、两套前端迁移弹窗 | 当前有效；独立业务域，后续单独审计数据库兼容和管理端接入 |

`classic -> default` 同步属于上述功能在两套前端之间的实现一致性，不作为单独业务 Feature。Docker `[build]` 触发和 Trellis/Flower 文件不进入业务治理清单。

## 首批范围：Responses Compact / Alpha Search

### 已符合规范的部分

- `relay/responses_compact_passthrough.go` 独立承载 Compact 能力门禁、原始 HTTP 透传、usage 观察、退款和响应头安全处理。
- `relay/alpha_search_handler.go` 独立承载 Alpha Search 请求体处理、上游 URL、Header、响应透传和安全错误处理。
- `controller/responses_websocket.go`、`relay/responses_websocket.go`、`controller/channel_test_responses_compact.go` 等 build 专用流程已经使用独立文件。
- `router/relay-router.go:81`、`router/relay-router.go:109` 只注册 Responses WebSocket 和 Alpha Search 路由，属于可接受的最薄接入。
- `controller/relay.go:50` 至 `controller/relay.go:57` 的 handler 分派本身较薄。

### 需要治理的冲突面

- `controller/relay.go` 相对上游为 `+253/-88`，首批三个提交又在该文件累计增加约 93 行。
- `controller/relay.go:138` 至 `controller/relay.go:239` 的共享重试循环直接包含 Compact 门禁、Alpha Search 计费、Compact 审计和 SSE bridge 生命周期判断。
- `controller/relay.go:310` 之后包含 Alpha Search 请求克隆、专用计费和 Compact/普通请求共用的 attempt 状态重置，build 专属职责仍停留在核心控制器。
- `middleware/distributor.go` 相对上游为 `+178/-122`。OpenAI Compact V1/V2 对齐为了让 Responses WebSocket 复用渠道选择，把原有 `Distribute` 主流程整体抽成共享函数，造成大面积上游文件重写。
- `middleware/distributor.go` 的改造虽然减少了逻辑重复，但与 build 分支“允许少量重复以降低上游冲突”的最高优先级规则冲突。

### 推荐结构边界

1. 恢复 `middleware.Distribute` 接近上游结构；Responses WebSocket 在独立文件中保留一份所需的模型权限、亲和性和渠道选择流程，并用行为测试防止语义漂移。
2. 将 Alpha Search 专用请求克隆、预扣和冻结工具计费移动到独立的 controller 文件；核心 `Relay` 只保留一次窄分派。
3. 将 Compact 专用门禁、SSE bridge、失败审计等 attempt 生命周期组合移动到独立 controller 文件；核心重试循环只调用稳定的准备/收尾接口。
4. 不重写 `relay/responses_compact_passthrough.go` 和 `relay/alpha_search_handler.go` 已独立且有回归保护的协议实现。
5. 不在首批治理中改变 API、选渠、计费、重试、日志或响应字节语义。

## 风险

- 渠道选择逻辑复制后，未来上游修改 `Distribute` 时需要显式同步检查 Responses WebSocket 的独立实现。
- 结构迁移若同时修复行为问题，会难以判断回归来自重构还是业务变更。
- Compact 的 HTTP、SSE、WebSocket 与渠道测试共享契约，必须保留完整回归矩阵，不能只依赖编译通过。

## 基线验证

规划阶段已在结构调整前执行以下基线命令：

```bash
go test ./controller ./dto ./middleware ./relay ./relay/channel/openai ./relay/channel/codex ./router ./service -count=1
```

结果全部通过。现有测试已覆盖：

- Alpha Search 请求校验、原始字段保留、URL/query 优先级、认证替换、响应头过滤、非 2xx、工具计费结算和敏感 URL 日志隔离。
- Compact HTTP 模式检测、渠道能力门禁、基础模型、路径矩阵、原始 body/SSE、usage 严格校验、退款和响应头过滤。
- Responses WebSocket 的 Compact 检测、重试提交边界、多轮普通/Compact 交替、取消、关闭码、usage 结算与退款。
- Compact 渠道测试、审计日志、BillingSession 幂等与路由注册。

该结果作为首批结构治理的前置行为快照；实施后必须重复执行，并补充定向 race、全仓测试、vet 和差异检查。
