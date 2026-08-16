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
- 不改变 `relaykit/`，视觉辅助渠道设置 JSON 和外部 Relay API 不变。
- 历史审计记录的空类型按普通请求处理。
- 不修改消息审计加密格式或 Blob schema；新字段只属于明文元数据。
- 不改变缓存键、视觉辅助重试次数、并发、计费和失败策略。
- 消息审计不可用时 `CaptureMessageAudit` 返回 false，视觉辅助继续执行。

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
- `web/src/features/channels/components/drawers/sections/build-channel-settings.tsx`：用新组件替换原有两个手填字段。
- `web/src/features/channels/api.ts`、`types.ts`：增加渠道模型选项 API 契约。
- `web/src/features/message-audits/types.ts`、列表和详情组件：展示类型及双向关联。
- `web/src/i18n/locales/*.json`：通过项目 i18n 脚本同步新增文案。

原有核心文件只承担字段、路由、调用或渲染入口；选择器和视觉辅助审计主体位于新文件。

## 风险与回滚

- 风险：辅助重试较多时消息审计记录数量增加。该行为与真实上游尝试一一对应，并受现有视觉辅助重试上限约束。
- 风险：主请求 capture 与辅助 capture 都是异步非阻塞写入，极端队列压力下可能只保存一侧。关联不设外键，单侧缺失不会阻塞另一侧。
- 风险：受控 Combobox 若遗漏历史值合成选项会导致显示或键盘异常，必须有交互回归。
- 风险：capture 后错误分支未 finalize 会产生 pending 记录，必须通过统一 defer 和失败测试保护。
- 回滚时删除新增组件、选项 API 和审计生命周期接入，并撤销新元数据字段的代码读取即可；数据库新增空闲列可保留，不需要破坏性回退迁移。
- 上游同步后重点复核渠道编辑 Drawer、`controller/relay.go` 的审计入口、`relay/vision_assist.go` 的准备顺序和消息审计列表查询列。
