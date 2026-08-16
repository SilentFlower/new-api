# 优化视觉辅助模型选择与消息审计设计

## 问题

视觉辅助配置要求管理员手填渠道 ID 和模型名，容易填错且无法感知渠道停用或模型变化。同时，视觉辅助内部请求直接调用渠道适配器，不经过主 Relay Controller，导致消息审计只能看到用户主请求，看不到真正交给辅助模型的提示词、用户问题和媒体摘要。

本任务增加一个低敏感度的渠道模型选项契约，并在视觉辅助最终 DTO 准备完成后建立独立审计生命周期。辅助记录与主请求通过显式关联字段连接，但不参与主请求的会话归并。

## 数据流

### 配置选择

```text
渠道编辑 Drawer
  -> GET /api/channel/model-options
  -> 启用渠道 [{id, name, models}]
  -> 可搜索渠道 Combobox
  -> 所选渠道模型 Combobox
  -> 保持现有 setting.vision_assist JSON 保存契约
```

### 视觉辅助审计

```text
主请求审计 capture
  -> 视觉辅助提取图片
  -> 请求内缓存 / Redis / 内存缓存
  -> 缓存未命中
  -> 构造识图单元请求
  -> 切换辅助渠道
  -> 转换为最终 OpenAI / Responses / Claude / Gemini DTO
  -> 独立视觉辅助审计 capture
  -> token / 定价 / 并发 / 预扣费 / 上游调用 / 响应处理
  -> 独立视觉辅助审计 finalize
  -> 写回主请求
```

每次视觉辅助重试都会重新进入上述 capture/finalize 生命周期；缓存命中不会进入辅助 caller，因此不生成记录。

## 渠道模型选项契约

新增管理 API：

```text
GET /api/channel/model-options
```

权限沿用渠道管理路由的 `AdminAuth + ChannelRead`。成功响应使用现有管理 API 包装：

```json
{
  "success": true,
  "message": "",
  "data": [
    {
      "id": 12,
      "name": "Gemini Vision",
      "models": ["gemini-2.5-flash", "gemini-2.5-pro"]
    }
  ]
}
```

Model 层新增稳定领域类型 `ChannelModelOption` 和 `ListEnabledChannelModelOptions`：

- GORM 只选择 `id, name, models`。
- 只返回启用渠道，按 ID 升序。
- 使用 `Channel.GetModels()` 解析模型列表。
- 不返回密钥、Base URL、代理、请求头覆盖或其他敏感配置。

消息审计 AI 重审现有选项查询可复用该稳定能力并保持原 JSON 契约，避免两处相同数据库查询漂移。

## 前端选择器

新增独立组件 `vision-assist-model-fields.tsx`，只负责辅助渠道和辅助模型两个字段；现有 `build-channel-settings.tsx` 保留一个薄组件接入点，不搬动其他视觉辅助设置。

### 渠道控件

- 使用项目现有 `Combobox`，选项标签为 `<渠道名> (#<ID>)`。
- 支持按名称和 ID 搜索，不允许创建自定义渠道 ID。
- 查询加载时禁用用户切换，但保持当前表单值。
- 当前已保存 ID 不在接口结果中时，合成一个当前选项并标记“不可用”，满足 Base UI 受控值契约。
- 用户实际切换渠道时，将 `vision_assist_model` 清空并标记表单 dirty，防止旧模型误配到新渠道。

### 模型控件

- 选项来自所选渠道的 `models`。
- 使用支持 `allowCustomValue` 的 `Combobox`，允许搜索、选择和输入未登记模型。
- 当前模型不在选项中时合成当前选项并显示不可用提示，但不改写历史值。
- 未选择渠道时禁用；已选择历史失效渠道时仍允许保留或输入自定义模型。
- 查询错误、空渠道和空模型以字段级状态展示，不关闭 Drawer，不清空表单。

新增交互文案通过 `i18n-translate` 工作流同步七语言，不直接手改 locale JSON。

## 消息审计数据契约

在 `model.MessageAuditRequest` 增加：

```go
RequestKind      string `json:"request_kind" gorm:"type:varchar(32);index"`
RelatedRequestID string `json:"related_request_id" gorm:"type:varchar(64);index"`
```

语义：

- `request_kind = client`：普通客户端请求。
- `request_kind = vision_assist`：视觉辅助内部请求。
- `related_request_id`：视觉辅助记录关联的主请求 ID；普通请求为空。
- 历史空 `request_kind` 读取时按 `client` 处理，不设置 GORM 数据库默认值。
- 不建立数据库外键。消息审计异步入队、保留清理和整库清空不依赖父记录写入顺序或存在性。

`MessageAuditCaptureInput` 同步增加 `RequestKind`、`RelatedRequestID` 和非持久化 `Standalone`：

- 空 `RequestKind` 在 service 归一化为 `client`。
- `Standalone=true` 时跳过会话前缀和压缩指纹构造。
- Model 层收到空指纹后按现有逻辑创建新的独立 `AuditSessionID`；不修改或复用 `ParentRequestID` 的会话链语义。

列表和详情查询的显式选择列必须加入两个新字段。详情响应增加精简的 `related_requests` 列表：

- 打开普通主请求时，按 `related_request_id = request_id` 查询全部视觉辅助记录，按创建顺序展示。
- 打开视觉辅助记录时，通过自身 `related_request_id` 提供主请求跳转。
- 关联目标已被保留策略删除时保留原 ID，跳转失败沿用现有详情加载错误，不影响当前记录查看。

## Relay 审计生命周期

新增 `relay/vision_assist_audit.go` 承载视觉辅助审计输入构造和结束状态映射，避免把完整逻辑铺入现有视觉辅助主文件。

开始时机：`prepareVisionAssistRequest` 成功并完成 `ModelMappedHelper` 后、token 估算前。此时：

- `prepared.req` 是准备交给辅助渠道的最终协议 DTO。
- `assistInfo.RelayFormat` 和 `RequestURLPath` 已对应实际端点。
- `assistInfo.RequestId` 是当前辅助尝试的独立请求 ID。
- 主请求 ID 仍可从 parent `RelayInfo` 获取。

Capture 字段：

- `RequestID = assistInfo.RequestId`
- `RelatedRequestID = parent.RequestId`
- `RequestKind = vision_assist`
- `Standalone = true`
- 用户、Token 和权限元数据继承主请求上下文。
- `ModelName` 初始记录配置模型，finalize 时按现有 `ConsumeLogModelName` 写入映射后计费模型。
- `RequestPath` 和 `Protocol` 使用辅助端点的实际值。
- `Request = prepared.req`

`callVisionAssistModel` 使用统一 defer 完成 finalize：

- 成功：`status=succeeded`、HTTP 200、记录实际耗时。
- 失败：`status=failed`、记录 `NewAPIError` 的错误码和 HTTP 状态。
- capture 未入队：finalize 为 no-op，不改变视觉辅助执行。
- capture 之后任一提前返回都必须经过同一 finalize，不在各错误分支复制审计代码。

## 审计界面

- 消息审计列表在模型单元格附近显示“视觉辅助”Badge，普通历史记录不增加视觉噪音。
- 详情元数据展示请求类型。
- 视觉辅助详情展示可点击的主请求 ID。
- 普通主请求详情展示关联视觉辅助调用列表，包含时间、模型、状态和请求 ID，可打开对应详情。
- 视觉辅助记录仍可单独执行 AI 重审，但其独立会话不会进入主请求会话的审核来源集合。

## 兼容性

- 新字段由现有 GORM `AutoMigrate` 添加，字段类型和索引兼容 SQLite、MySQL、PostgreSQL。
- `relaykit` 仅增加向后兼容的可选 `combined_max_images` JSON 字段，不依赖根模块，并继续保持独立可构建。
- 历史审计记录的空类型按普通请求处理。
- 不修改消息审计加密格式或 Blob schema；新字段只属于明文元数据。
- 不改变缓存键、视觉辅助重试次数、并发、计费和失败策略。
- 消息审计不可用时 `CaptureMessageAudit` 返回 false，视觉辅助继续执行。

## Combined 模式有界分批

`ChannelVisionAssistSettings` 增加可选整数 `CombinedMaxImages`，JSON 字段为 `combined_max_images`。后端统一归一化为 `1-64`，缺失、`0` 或越界均使用默认值 `5`。该字段位于 `relaykit/dto`，因此变更后必须使用 `GOWORK=off go build ./...` 验证独立模块。

`buildVisionAssistUnitPlan` 在 `combined` 模式下按用户消息分别构造批次，并按原始图片顺序贪心装箱：

1. 当前批次达到 `combined_max_images` 时结束该批次。
2. 当前批次加入下一张内联图片后预计超过固定 `8 MiB` 请求体安全上限时，提前结束该批次。
3. 单张图片自身超过安全上限时独立成批，不在本层拒绝，由现有辅助调用和失败策略处理。
4. `separate` 模式继续保持一图一单元，不读取该上限改变分组结果。

分批只改变识图单元边界，不改变图片对象的全局 `Index` 和 `MessageIndex`。因此识别结果仍可按全局索引排序并注入目标模型，跨批次综合由目标模型基于全部文字结果完成。

缓存键继续包含当前识图单元的图片序列。配置上限变化会自然形成不同批次和缓存键，不复用边界不同的旧组合缓存。现有 `max_concurrency`、单元级重试、预扣费和结算逻辑无需另建执行通道。

自动分批必须使用纯输入决定边界，不能依赖请求 ID、消息索引、批次序号、goroutine 完成顺序或审计元数据。现有缓存键继续由辅助渠道、辅助模型、提示词、用户问题、多图模式和批次内有序图片内容哈希组成；不把 `combined_max_images` 直接写入缓存键。这样：

- 新一轮对话重复提交相同问题和相同有序图片时，只要确定性分批结果相同，就能在 Redis 或进程内 HybridCache 中逐批命中。
- 调整上限但未改变某个实际批次的图片组合时，该批次结果仍可安全复用。
- 调整上限导致联合图片组合变化时，图片序列哈希自然不同，不会错误复用旧组合的联合分析结果。
- 批次在执行完成后仍按现有 TTL 写入缓存；缓存命中不进入辅助 caller，因此不产生辅助审计、预扣费或上游请求。

回归测试使用两个独立 `gin.Context` 和不同请求 ID 模拟新会话：第一次完成所有分批并写缓存，第二次提交等价输入，断言 caller 零调用、结果顺序一致且缓存命中图片数覆盖全部输入。另覆盖消息索引变化、并发完成顺序变化和分批上限变化的安全命中边界。

渠道编辑页在选择 `combined` 后显示数字输入框“单批最大图片数”，范围 `1-64`，默认 `5`。表单解析旧配置时回填 `5`，保存时写入 `vision_assist.combined_max_images`；切换到 `separate` 不删除已配置值，便于再次切回。

普通请求日志增加以下诊断字段：

- `vision_assist_combined_max_images`
- `vision_assist_batch_count`
- `vision_assist_batch_image_counts`
- `vision_assist_split_applied`
- `vision_assist_split_reason`

`split_reason` 仅取稳定枚举值 `image_count`、`payload_size` 或 `image_count_and_payload_size`，未切割时省略。

## 文件与上游冲突面

### 新建文件

- `model/channel_model_option.go`：启用渠道及模型的精简稳定查询。
- `controller/channel_model_option.go`：渠道模型选项管理 API 薄控制器。
- `relay/vision_assist_audit.go`：视觉辅助独立审计生命周期。
- `web/src/features/channels/components/drawers/sections/vision-assist-model-fields.tsx`：联动选择器。
- 对应的新测试文件：分别覆盖 API、审计生命周期和选择器交互。

### 必须修改的现有文件

- `router/channel-router.go`：只注册一条渠道模型选项路由。
- `model/message_audit_review_options.go`：改为复用通用启用渠道模型查询，保持原 API 输出。
- `model/message_audit.go`：增加审计元数据字段和查询列。
- `service/message_audit.go`：扩展 capture 输入、standalone 会话边界和详情关联结果。
- `relay/vision_assist.go`：在最终 DTO 准备后启动审计，并通过统一 defer finalize。
- `relaykit/dto/channel_settings.go`：增加 `combined_max_images` 可选配置字段，保持模块独立。
- `service/vision_assist.go`：按图片数与固定请求体安全上限切割 combined 单元，并输出分批诊断字段。
- `web/src/features/channels/components/drawers/sections/build-channel-settings.tsx`：用新组件替换原有两个手填字段。
- `web/src/features/channels/lib/build-channel-settings.ts`：解析、校验并保存单批最大图片数。
- `web/src/features/channels/api.ts`、`types.ts`：增加渠道模型选项 API 契约。
- `web/src/features/message-audits/types.ts`、列表和详情组件：展示类型及双向关联。
- `web/src/i18n/locales/*.json`：通过项目 i18n 脚本同步新增文案。

原有核心文件只承担字段、路由、调用或渲染入口；选择器和视觉辅助审计主体位于新文件。

## 风险与回滚

- 风险：辅助重试较多时消息审计记录数量增加。该行为与真实上游尝试一一对应，并受现有视觉辅助重试上限约束。
- 风险：主请求 capture 与辅助 capture 都是异步非阻塞写入，极端队列压力下可能只保存一侧。关联不设外键，单侧缺失不会阻塞另一侧。
- 风险：受控 Combobox 若遗漏历史值合成选项会导致显示或键盘异常，必须有交互回归。
- 风险：capture 后错误分支未 finalize 会产生 pending 记录，必须通过统一 defer 和失败测试保护。
- 风险：分批边界变化会改变 combined 缓存键；这是避免错误复用旧组合结果所必需的预期行为。
- 风险：联合识别结果取决于同批图片组合和顺序，因此不能拆成单图缓存后跨任意批次拼接；本设计只复用完全相同的有序批次。
- 风险：远程图片 URL 无法在调用前获知真实媒体大小，请求体安全上限只能精确约束内联 data URL；图片数量上限仍对所有来源生效。
- 延后风险：`strip_image=true` 会阻断需要原始像素的图片操作 Agent；`strip_image=false` 可能让目标模型重复识图，且不支持视觉的上游可能拒绝请求。当前没有可供 Agent 跨请求访问的稳定本地图片路径。该问题涉及模型能力判断、对象存储权限与图片工具协议，不在本轮解决。
- 分批实现必须保持现有原图转发语义：`strip_image=false` 时不修改原始媒体块的内容、顺序或出现次数，`strip_image=true` 时维持现有移除行为。
- 回滚时删除新增组件、选项 API 和审计生命周期接入，并撤销新元数据字段的代码读取即可；数据库新增空闲列可保留，不需要破坏性回退迁移。
- 上游同步后重点复核渠道编辑 Drawer、`controller/relay.go` 的审计入口、`relay/vision_assist.go` 的准备顺序和消息审计列表查询列。
