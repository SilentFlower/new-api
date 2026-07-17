# 规范化现有 Build 特有 Feature：首批技术设计

## 1. 设计目标

首批只治理 Responses Compact / Alpha Search 对上游核心文件的侵入面。结构调整前后必须保持当前 `build-bak` 的可观察行为不变，包括：

- Token 模型权限、分组、渠道亲和性、首次选择和重试。
- Compact 渠道能力门禁、基础模型计费和原始 HTTP/SSE/WS 数据。
- Alpha Search 原始字段、模型映射、纯工具计费和安全错误处理。
- BillingSession 预扣、结算、退款和 quota saturation 审计。
- 错误响应 code、错误日志、渠道 auto-ban 和请求 ID 行为。

若实施中发现现有行为缺陷，只记录证据，不在本任务修复。

## 2. 当前问题

### 2.1 渠道分发器被共享复用改写

OpenAI Compact V1/V2 对齐为了让 Responses WebSocket 复用 HTTP 分发能力，将 `middleware.Distribute` 的主体抽成 `SelectAndSetupChannel`、`ValidateTokenModelAccess` 和 `ChannelSupportsRequest`。这使 `middleware/distributor.go` 相对上游出现 `+178/-122` 的大面积改写。

该复用在普通分支中合理，但与 build 分支“允许少量重复以降低上游同步冲突”的最高优先级规则冲突。

### 2.2 Relay 核心控制器包含 build 专用实现

`controller/relay.go` 同时包含：

- 通用请求快照和 attempt 状态重置。
- 普通文本预扣和最终失败退款。
- Alpha Search 专用冻结工具计费。
- Compact 门禁、SSE bridge 和失败审计接入。

Compact/Alpha 的协议处理已经位于独立文件，但准备与计费实现仍增加核心控制器的冲突面。

## 3. 目标结构

### 3.1 HTTP 分发保持上游友好

恢复 `middleware.Distribute` 为接近首批功能接入前的顺序式控制流，不再为了 Responses WebSocket 把整个 HTTP 分发流程抽成共享函数。

必须保留当前行为：

- Token 指定渠道的解析、状态检查和错误响应。
- Token 模型权限在 `shouldSelectChannel=false` 时仍执行。
- 渠道亲和性、auto group、Advanced Custom path/model 检查和随机选渠。
- `SetupContextForSelectedChannel` 错误按当前 HTTP 契约返回。
- Responses Compact detector 继续写入 `ContextKeyResponsesCompactMode`，所有 Compact 模式继续使用基础模型选渠。

`middleware/distributor.go` 中与 Compact 相关的新增逻辑只保留 detector 调用这一处窄接入。

### 3.2 Responses WebSocket 使用独立渠道选择

新增 `middleware/responses_websocket_channel.go`，完整承载 Responses WebSocket 需要的：

- 首轮 Token 模型权限校验。
- 指定渠道读取与状态检查。
- 渠道亲和性和 auto group 选择。
- 随机渠道选择。
- Advanced Custom `/v1/responses` path/model 支持判断。
- 选中渠道后的 context 初始化。
- 后续 turn 的模型权限与当前渠道能力检查。

公开函数使用 Responses WebSocket 领域名称，避免形成新的通用分发框架。该文件可以与 HTTP 分发保留少量重复；每次上游同步 `middleware.Distribute` 时，将两者差异列入显式复核点。

`controller/responses_websocket.go` 是 build 新增文件，只修改调用名称，不改变首帧、重试、failover、计费或双向转发逻辑。

### 3.3 Relay attempt 生命周期移出核心文件

新增 `controller/relay_attempt.go`，原样迁移以下稳定职责：

- `fastTokenCountMetaForPricing`。
- `cloneRelayRequest`。
- `resetMainRelayAttemptFields`。
- `prepareMainRelayBilling`。
- `finalizeMainRelayBilling`。

新增 `controller/relay_alpha_search.go`，原样迁移 `prepareAlphaSearchBilling`。该函数是明确的 Alpha Search 纯工具计费边界，独立文件比继续留在上游核心控制器更容易识别和回滚。

`controller/relay.go` 只保留已有窄接入：

- handler switch 中的一次 Compact / Alpha Search 分派。
- 启停 Compact bridge 的条件调用。
- 每次 attempt 的 Compact 门禁或普通请求准备调用。
- Alpha Search 与普通计费准备的窄分派。
- 失败时写入 Compact 审计。

不为这些窄调用新增通用插件体系，也不重写整个 `Relay` 主循环。

## 4. 文件职责

### 4.1 新建文件

| 文件 | 完整职责 |
| --- | --- |
| `middleware/responses_websocket_channel.go` | Responses WebSocket 专用模型权限、亲和性、渠道选择和当前渠道能力校验 |
| `middleware/responses_websocket_channel_test.go` | WS 专用分发行为、错误 code、基础模型和当前渠道能力回归 |
| `middleware/distributor_http_contract_test.go` | 结构调整前后 HTTP `Distribute` 的可观察错误与模型行为契约 |
| `controller/relay_attempt.go` | Relay 请求快照、attempt 重置、普通计费准备和最终失败退款 |
| `controller/relay_alpha_search.go` | Alpha Search 冻结工具计费和预扣准备 |

### 4.2 必须修改的原有上游文件

| 文件 | 最薄接入或修改 | 必要性 |
| --- | --- | --- |
| `middleware/distributor.go` | 恢复顺序式 HTTP 分发；保留 Compact detector 窄接入 | 移除 WebSocket 复用造成的大面积重写，同时保持 HTTP 行为 |
| `controller/relay.go` | 删除迁移到新文件的函数体，保留现有调用 | 减少核心控制器中的 build 实现行数，不改变主循环 |

### 4.3 Build 新增文件的调整

| 文件 | 修改 |
| --- | --- |
| `controller/responses_websocket.go` | 改用 Responses WebSocket 专用渠道选择 API |
| `middleware/distributor_model_access_test.go` | 由领域化测试文件替代，避免继续锁定已移除的通用 helper |

### 4.4 明确不修改

- `relay/responses_compact_passthrough.go`。
- `relay/alpha_search_handler.go`。
- `relay/responses_websocket.go`。
- `service/tool_billing.go`、`service/billing_session.go`。
- `dto.ChannelSettings`、前端渠道设置和 i18n。
- 数据库模型、迁移、配置格式和 API 路由。

## 5. 行为保护策略

1. 先补充 HTTP `Distribute` 的可观察契约测试，再做结构迁移。
2. 复用现有 Compact/Alpha/WS 协议测试作为黄金行为基线。
3. 不修改测试期望来迁就结构调整；只有测试本身直接引用被移除的内部 helper 时，改为断言等价公开行为。
4. 对 HTTP 和 Responses WebSocket 的模型权限、错误 code、亲和性、Advanced Custom 支持分别建立回归。
5. 实施后重复规划阶段已通过的相关包基线，并执行定向 race、全仓测试和 vet。

## 6. 兼容性与数据流

- 不增加数据库字段或迁移。
- 不改变 Channel `setting` JSON。
- 不改变请求/响应 DTO。
- 不改变 Compact/Alpha 路由、上游 path、Header、query 或 body。
- 不改变普通 HTTP Relay、Responses WebSocket 或渠道测试的对外契约。
- 不改变旧记录兼容性。

## 7. 冲突面与上游同步复核

预期结果：

- `middleware/distributor.go` 不再因 WS 复用而重排整个函数。
- `controller/relay.go` 删除大段 build helper 实现，仅保留窄分派。
- 新增文件可独立删除，回滚时只需恢复两个上游文件和 WS 调用点。

每次后续合并 `main` 时必须复核：

- 上游 `Distribute` 是否改变 Token 权限、指定渠道、亲和性、auto group 或 Advanced Custom 语义。
- `SetupContextForSelectedChannel` 是否增加新的 context 字段。
- 上游 Relay 预扣、重试或错误收尾是否改变。
- Responses WebSocket 的独立副本是否需要同步相同的安全或选择规则。

## 8. 回滚

- 删除本任务新增的 controller/middleware 文件。
- 恢复 `middleware/distributor.go`、`controller/relay.go` 和 `controller/responses_websocket.go` 到任务前版本。
- 恢复对应测试文件。
- 无数据、配置或外部系统回滚步骤。

## 9. 验收口径

本地协议模拟、相关包回归、定向 race、全仓测试、vet 和差异检查是阻塞门槛。真实 OpenAI/sub2api 联调在具备安全环境时作为非阻塞补充验证。
