# Design — Claude 消息审计会话归并

## 方案概述

消息审计继续保存请求校验后、渠道覆盖前的安全过滤入站内容。仅为 Claude 会话推断派生一个不落库的语义指纹副本，消除 Claude Code 连续请求中不代表会话身份的瞬态差异。

修复不移动 `CaptureMessageAudit`，不读取最终上游请求，不改变审计正文、Relay、渠道覆盖、计费或数据库结构。

## 数据流

1. `controller/relay.go` 继续在现有位置调用 `CaptureMessageAudit`。
2. `service/message_audit.go` 的 `normalizeRequest` 继续生成安全过滤后的实际审计条目，并沿用现有大小限制与 `content_reduced` / `metadata_only` 降级。
3. Claude 请求从最终选定的指纹源条目派生语义规范化副本；实际保存条目保持不变。
4. Claude 的滚动前缀指纹使用语义副本，压缩匹配锚点继续使用实际保存条目的 HMAC。
5. `model/message_audit.go` 继续按现有 exact、prefix、compressed、new 规则分配会话，无需修改。

## Claude 语义规范化契约

- 只处理 `types.RelayFormatClaude` 对应的指纹副本；其他协议直接沿用现有条目。
- 递归移除指纹副本中的 `cache_control` 字段，不修改实际保存内容。
- 仅在 `system` 审计条目中识别以 `x-anthropic-billing-header:` 开头的文本，删除分号分隔参数中的 `cch=` 项，保留其余稳定字段与其他 system 指令。
- 在 Claude system 内容和 Claude message 的 `content` 字段中，把字符串与仅含单个 `{type: "text", text: ...}` 内容块的数组规范化为同一表示。
- 单文本块只有在移除 `cache_control` 后不含其他语义字段时才允许折叠；包含 Tool、图片、引用或其他字段时保持结构。
- Tool `input` 内部属于用户可见语义载荷；其中名为 `cache_control` 的业务字段必须保留，只有 Claude 协议内容块上的缓存控制元数据可以从指纹副本移除。
- 用户、助手、工具调用 ID、工具名称、工具输入、工具结果和稳定 system 文本保持参与指纹计算，真实变化必须产生不同指纹。
- 规范化过程必须构造独立副本，禁止原地修改 `normalizeRequest` 返回的实际保存条目。

## 前缀与压缩锚点

现有 prefix/exact 匹配依赖滚动 `SequenceFingerprint`，适合使用 Claude 语义副本。现有 compressed 匹配会用 `SessionAnchorHMACs` 查询真实 `message_audit_blobs`，因此锚点不能改为只存在于内存中的规范化 HMAC。

Claude 专用构建逻辑按同一条目顺序执行：

- 使用规范化条目生成 `ConversationPrefixFingerprints` 和最终 `SequenceFingerprint`。
- 使用对应的实际保存条目生成 `SessionAnchorHMACs`。
- 沿用 `isMessageAuditConversationBlob` 和 `isMessageAuditSessionAnchor` 的现有角色与内容类型边界。
- `metadata_only` 没有实际保存条目时不伪造压缩锚点；其前缀指纹仍沿用现有降级后的指纹源语义。

这样可以修复正常连续请求的 exact/prefix 归并，同时保持 compressed 候选查询和 Blob 去重合同不变。

## 文件边界

### 新建文件

- `service/message_audit_claude_session.go`
  - 承载 Claude 指纹副本规范化。
  - 承载 Claude 语义前缀与实际存储锚点的联合构建。
- `service/message_audit_claude_session_test.go`
  - 承载生产形态 Claude 连续请求、真实变化和存储内容不变的回归测试。

### 原有文件薄接入

- `service/message_audit.go`
  - 在 `normalizeRequest` 的现有三个返回边界调用 Claude 指纹条目派生函数。
  - 在 `CaptureMessageAudit` 中仅为 Claude 分派到专用会话指纹构建逻辑，其他协议继续调用现有函数。

不修改 `controller/relay.go`、`model/message_audit.go`、DTO、数据库迁移或前端文件。

## 公共能力复用

- JSON 序列化与副本转换使用 `common.Marshal` / `common.Unmarshal`。
- HMAC、滚动指纹、会话角色判断继续复用 `messageAuditManager` 及现有私有函数。
- 不抽取通用插件或重构消息审计主流程，避免扩大 build 分支与上游的冲突面。

## 兼容与上线边界

- 不回填历史记录，也不提供修复脚本。
- 部署后的第一条 Claude 请求可能无法与部署前使用旧指纹算法的最后一条记录匹配，因此可能新建一次会话；后续连续请求应稳定归并。
- 审计详情、Blob HMAC、去重字节、消息计数、工具计数和加密正文继续反映该次请求实际捕获的安全过滤内容。
- 没有新增持久化版本字段；回滚只需删除 Claude 专用文件并撤销 `service/message_audit.go` 的薄接入。

## 风险与控制

- 风险：规范化范围过宽导致真实内容被错误合并。
  - 控制：只处理 Claude 指纹副本，只删除明确字段，并为稳定 system、消息、Tool 输入和 Tool 结果变化添加反例测试。
- 风险：规范化 HMAC 被误用于 compressed Blob 查询。
  - 控制：前缀与实际存储锚点分开计算，并断言锚点等于实际加密记录的 Blob HMAC。
- 风险：指纹副本修改实际审计正文。
  - 控制：独立深拷贝并对原始 `cache_control`、billing header 和内容表示做持久化断言。
- 风险：上游同步冲突。
  - 控制：完整逻辑集中到新文件，原有文件只增加窄调用和 Claude 分派。

## 上游同步复核点

上游修改 `service/message_audit.go` 的以下边界时需要复核：

- `normalizeRequest` 的大小降级与返回值语义；
- `CaptureMessageAudit` 的指纹构建调用；
- `buildMessageAuditSessionFingerprints`、`isMessageAuditConversationBlob` 或 `isMessageAuditSessionAnchor` 的会话规则。

只需确认 Claude 专用逻辑仍接收最终指纹源条目和实际保存条目，不需要把专用逻辑合回上游主文件。
