# 重构 Responses Compact 透传与基础模型计费

## 目标

将 new-api 的 Responses Compact 职责收敛为鉴权、限流、现有渠道选择、基础模型计费和协议透传：V1、历史 body bridge、V2 HTTP/SSE 与 V2 WebSocket 均使用下游请求中的基础模型，渠道明确开启透传能力后将原始模型、请求体和响应协议转发给 sub2api，不再生成或依赖 `-openai-compact` 虚拟模型。

## 背景与已确认事实

- 下游 Compact 请求携带基础模型，例如 `gpt-5.6-sol`，不会携带 `gpt-5.6-sol-openai-compact`。
- 当前 V1 分发会在 [middleware/distributor.go](/root/project/new-api/middleware/distributor.go:470) 为选择模型追加 `-openai-compact`。
- 当前所有 Compact 模式都会在 [relay/helper/model_mapped.go](/root/project/new-api/relay/helper/model_mapped.go:73) 将本地计费模型改为带后缀的虚拟模型。
- 当前主链路在 [controller/relay.go](/root/project/new-api/controller/relay.go:179) 完成模型映射后，于真正发送上游请求前执行价格检查和预扣；未配置后缀价格时会在 new-api 内返回 `model_price_error`，sub2api 收不到请求。
- 现有亲和性先尝试 `service.GetPreferredChannelByAffinity`，未命中时才按基础模型、分组、优先级和权重选择渠道；Compact 必须服从这一结果。
- sub2api 已支持 V1 `/responses/compact`、原生 V2 `remote_compaction_v2`、历史 body bridge 和 Responses WebSocket，并负责内部账号能力、账号调度、真实上游模型映射与 OpenAI Compact 协议处理。
- 历史 body bridge 必须让 sub2api 收到裸 `/responses`；sub2api 会在内部提升至 `/responses/compact`，并在客户端请求流式响应时合成 SSE。
- 旧任务 `07-16-openai-compact-v1-v2-alignment` 已归档；该任务完成协议对齐，但保留了 Compact 虚拟模型选择和计费后缀，且未完成真实 new-api 到 sub2api 联调。

## 实现约束

- Compact 是请求协议和渠道能力，不是独立模型。
- new-api 不通过渠道名称、类型或 Base URL 猜测 sub2api；管理员通过渠道 JSON 设置显式开启 `responses_compact_passthrough_enabled`。
- 所有 Compact 模式均以请求基础模型完成 Token 模型权限、分组、亲和性、渠道选择、预扣与结算。
- Compact 透传不执行 new-api 渠道模型映射、参数覆盖、禁用字段过滤或请求 DTO 重组；发送给上游的 `model` 与下游请求一致。
- 不增加 Compact 工具价格或固定调用费；new-api 继续按基础模型现有价格和合法 usage 计费。
- sub2api 原则上不修改业务逻辑；仅当真实联调证明其现有协议不兼容时，另行评估独立变更。
- 本任务必须遵守 [Build 分支上游同步友好定制指南](/root/project/new-api/.trellis/spec/guides/build-upstream-friendly-customization.md)：核心逻辑优先放入新文件，原有 Router、Middleware、Controller、Relay、WebSocket 和前端大表单只做最薄分派、字段接入或组件挂载。
- 可以复用 `common.BodyStorage`、Adaptor 的 URL/Header/DoRequest 边界、`BillingSession`、价格与 quota helper、日志和 Responses 元数据 header allowlist；不得为复用而重构上游实现。
- 不做无关格式化、重命名、移动、清理或抽象。若一个原有文件的修改无法用一句话说明必要性，不得进入实施范围。

## 需求

### R1. Compact 模式识别

- 保留现有检测器并覆盖：
  - V1：`POST /v1/responses/compact`。
  - 历史 body bridge：裸 `POST /v1/responses` 携带 `compaction_trigger`，但不满足原生 V2 信号。
  - V2 HTTP/SSE：裸 `POST /v1/responses` 同时携带 `stream:true`、`compaction_trigger` 和 `remote_compaction_v2` beta feature。
  - V2 WebSocket：Responses WebSocket `response.create` turn 满足原生 V2 信号。
- 普通 Responses 请求不得进入 Compact 透传分支。

### R2. 基础模型、亲和性与能力门禁

- 分发层不得生成、查询或要求渠道声明 `*-openai-compact` 模型。
- Compact 首次选择沿用普通请求的现有基础模型、分组、优先级、权重和亲和性语义；现有策略允许实际上游失败重试时，重试渠道选择也继续使用基础模型。
- 渠道选定并完成上下文初始化后，读取 `ChannelSettings.ResponsesCompactPassthroughEnabled`。
- 开关开启后才允许准备计费和发送请求；开关未开启时立即返回明确、可区分、不可重试的渠道配置错误。
- 能力门禁失败不得改选其他渠道、不得清除或绕过亲和性、不得自动禁用渠道、不得记为上游故障，也不得产生预扣。
- Compact 不执行第二轮能力筛选；同一分组存在多个渠道时，第一次选择结果仍完全服从现有亲和性和权重规则。
- 开关仅影响 Compact 请求，普通 Responses 与其他端点保持现有行为。

### R3. 请求透传与路径

- Compact 透传不调用现有 Compact 模型映射和请求重组链路；`OriginModelName`、`UpstreamModelName`、计费模型和出站 body 中的 `model` 均保持请求基础模型。
- V1 path 使用既有 Compact 上游路径：Codex 渠道为 `/backend-api/codex/responses/compact`，其他支持渠道沿用其 Compact URL 规则。
- V2 HTTP/SSE 与 WebSocket 保持普通 `/responses` 上游路径。
- 历史 body bridge 必须保持裸 `/responses` 和原始 body，由 sub2api 完成内部提升与 SSE bridge；new-api 不启动旧的本地 bridge。
- 原始 body 或 WebSocket frame 中的未知字段、显式 `0`、`false`、密文和未来字段必须保留。
- 安全元数据请求头使用既有 allowlist；客户端 `Authorization`、Cookie、Host、`Content-Length` 和连接级凭证不得透传，上游认证必须使用所选渠道配置。

### R4. 响应透传与 usage 观测

- V1 JSON 响应按原始状态、允许响应头和 body 返回，不重新序列化。
- V2 SSE、历史 bridge SSE 和 WebSocket payload 原样返回，不重组 `compaction` item、`encrypted_content` 或未知字段。
- new-api 只旁路观测 usage、终态、Compact item 和审计字段；观测不得改变客户端收到的 payload。
- 成功终态存在合法 usage 时才进入结算；成功终态缺失或包含非法 usage、失败、取消、断连或不完整流必须按现有 `BillingSession` 安全语义退款并记录异常，不猜测 token 数量。

### R5. 基础模型计费

- new-api 使用请求基础模型查询现有 `ModelPrice`、`ModelRatio`、补全倍率和缓存倍率。
- 不再查询或要求配置 `*-openai-compact` 精确价格或通配价格。
- 渠道能力门禁通过后才执行预扣；门禁失败不得触发预扣。
- 合法 usage 按基础模型价格结算；sub2api 内部模型映射不得改变 new-api 的计费模型。
- 不增加 Compact 工具价格、固定调用费或独立计费配置。

### R6. 配置与管理界面

- 在现有渠道 JSON `setting` 中新增布尔字段 `responses_compact_passthrough_enabled`，默认关闭，不新增数据库字段或迁移。
- Default 与 Classic 渠道编辑界面均可读取、编辑并保存该字段；编辑旧渠道时不得丢失该设置或其他未知 JSON 字段。
- Default 新 UI 文案使用 `useTranslation()`，并通过规定脚本补齐 `en`、`zh`、`fr`、`ja`、`ru`、`vi` 六种语言。
- 旧 `*-openai-compact` 渠道模型和价格配置不再是运行前提，但本任务不自动删除管理员已有配置。

### R7. 审计、兼容和错误

- Compact 管理员审计继续记录模式、入站路径、上游路径、渠道、请求 ID、结局和耗时。
- 日志不得记录请求正文、对话内容、压缩密文、完整 URL query 或凭证。
- 能力门禁错误必须与 `model_price_error`、模型不可用和真实上游错误区分。
- 普通 Responses、Chat Completions via Responses、Alpha Search、视觉辅助和其他 Relay 模式的模型映射、参数覆盖、计费和重试语义不得改变。

## 验收标准

- [ ] 下游请求 `model=gpt-5.6-sol` 时，V1、历史 body bridge、V2 HTTP/SSE 和 V2 WebSocket 的 Token 权限、渠道选择、亲和性、预扣与结算均使用 `gpt-5.6-sol`。
- [ ] 不配置任何 `gpt-5.6-sol-openai-compact` 渠道模型或价格时，四种 Compact 模式可通过开启透传的渠道到达 sub2api。
- [ ] 所选渠道未开启透传时返回专用配置错误，不改选其他渠道、不进入能力失败重试、不清除亲和性、不自动禁用渠道且不产生预扣。
- [ ] 现有策略允许真实上游失败重试时，每次重试仍使用基础模型，并在新渠道上重新执行能力门禁。
- [ ] V1 上游路径为 Compact 路径；V2、WebSocket 和历史 body bridge 上游路径保持 `/responses`。
- [ ] HTTP 原始 body 与 WebSocket frame 中的 `model` 保持请求值，且不出现 `-openai-compact`；未知字段和显式零值不丢失。
- [ ] V1 JSON、V2 SSE、历史 bridge SSE 和 WebSocket 响应内容原样到达客户端，usage 旁路观测不改变 payload。
- [ ] 合法 usage 按基础模型价格结算；缺失或非法 usage、失败、取消、断连和不完整流退款，不增加 Compact 工具费用。
- [ ] Default 与 Classic 均能保存渠道开关；Default 六种语言完整且 i18n 同步报告无本任务新增缺失项。
- [ ] 普通 Responses 继续执行现有模型映射、Param Override、disabled fields 和计费路径。
- [ ] Compact 透传核心实现位于独立新文件；原有文件仅包含必要的薄接入点，无无关格式或结构调整。
- [ ] 删除新模块并撤销薄接入点即可完整回滚；逐文件审查能够用一句话解释每个原有文件的修改必要性。
- [ ] 完成 new-api 到本地 sub2api 的 V1、V2 HTTP/SSE、历史 bridge 和 WebSocket 真实联调，双方日志可通过请求 ID或安全审计字段关联。
- [ ] 定向测试、相关 `-race`、前端 typecheck/lint/build、`go test ./...`、`go vet ./...` 和 `git diff --check` 通过；既有基线问题单独说明。

## 不在范围内

- 修改 sub2api 的账号数据库、账号调度、Compact 探测或真实 OpenAI 模型映射策略。
- 为 Compact 新增按调用次数计费的工具价格。
- 自动识别某个 Base URL、渠道名称或渠道类型是否为 sub2api。
- 自动删除旧 `*-openai-compact` 模型或价格配置。
- 改变普通 Responses 请求的计费模型、请求转换或上游模型映射规则。
