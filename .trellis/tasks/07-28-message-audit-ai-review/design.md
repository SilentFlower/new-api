# 技术设计

## 设计目标

本任务在现有 Root-only 消息审计边界内增加会话级 AI 辅助审核，同时修复模型展示、失败原因、超大正文降级和 metadata-only 会话归并问题。

核心约束：

- 审核粒度固定为 `audit_session_id`，不改变现有单次请求详情和会话切换模型。
- 审核调用走内部控制面，不经过公开 Relay Controller，不计费、不写消费日志、不产生新的消息审计。
- AI 只通过受限虚拟文件 Tool 按需读取本次资料集，不能访问真实文件系统、网络、数据库或其他会话。
- 审计正文和完整审核结果均加密保存；普通日志、管理审计和系统任务只记录无正文元数据。
- SQLite、MySQL、PostgreSQL 使用同一 GORM 数据合同。

## 数据模型

### 消息审计请求扩展

`model.MessageAuditRequest` 增加兼容历史数据的可空统计字段：

```go
CapturedPlaintextBytes *int64 `json:"captured_plaintext_bytes"`
StoredPayloadBytes     *int64 `json:"stored_payload_bytes"`
```

语义：

- `plaintext_bytes`：过滤、规范化后的原始正文大小，即使最终降级也保留。
- `captured_plaintext_bytes`：本次实际进入加密块的明文大小；历史记录为 `nil`，不能伪装成 0。
- `stored_payload_bytes`：本请求新增的 nonce 与密文载荷字节数；复用既有 blob 的部分不重复计入，历史记录为 `nil`。
- `dedup_saved_bytes`：沿用现有同用户去重节省的明文字节。

新增 `audit_status=content_reduced` 表示原始快照超过 1 MiB，但系统剥离非审核内容后仍保存了有界文本；`metadata_only` 继续表示没有可解密正文。

### 当前审核结果

新增 `message_audit_reviews`：

```go
type MessageAuditReview struct {
    ID                int64
    AuditSessionID    string
    UserID            int
    ReviewedRequestID string
    CurrentTaskID     string
    RiskLevel         string
    ReviewChannelID   int
    ReviewModel       string
    KeyFingerprint    string
    ResultNonce       []byte
    ResultCiphertext  []byte
    ReviewedAt        int64
    CreatedAt         int64
    UpdatedAt         int64
}
```

合同：

- `audit_session_id` 唯一，一行只表示最后一次成功结果和当前/最后一次重审任务。
- 风险等级、模型、时间和任务 ID 是列表需要的无正文元数据。
- 摘要、分类、依据和覆盖范围统一序列化后，使用 `MESSAGE_AUDIT_SECRET` 派生出的独立审核密钥和包含用户、会话的 AAD 加密。
- 重审开始只替换 `current_task_id`，旧密文结果保持不变；新结果成功后才原子替换。

新增 `message_audit_review_sources`：

```go
type MessageAuditReviewSource struct {
    ID        int64
    ReviewID  int64
    RequestID string
    CreatedAt int64
}
```

该表只保存审核资料集的请求引用，不复制正文或密文。成功替换结果时同步替换来源引用；清理任一来源请求时保守删除对应审核结果。

### 系统任务扩展

新增任务类型 `message_audit_review`。通用 `SystemTask` 增加兼容现有调用的 scoped 创建能力：

- 默认任务继续使用 `active_key=task_type`，行为不变。
- 审核任务使用 `active_key=message_audit_review:<audit_session_id>`，保证同一会话重复点击只返回同一活动任务。
- `SystemTaskLock` 仍按任务类型加锁，因此不同会话可排队，但审核任务全局一次只执行一个，避免内部审核并发挤占正常渠道。
- 系统任务 payload 只保存会话 ID、触发时固定的目标请求 ID、来源请求 ID、操作者 ID和配置快照，不保存正文。
- 系统任务 result 不保存 AI 输出；成功结果只进入加密审核表，失败只保存稳定错误类别。

## 超大正文与会话归并

### 分级降级

保留原 1 MiB 完整快照阈值，并增加默认 4 MiB 的审核文本硬上限；部署可通过受边界校验的环境变量覆盖，但系统设置不增加该选项。

处理顺序：

1. 按现有协议完成敏感字段、隐藏思考和媒体二进制过滤。
2. 完整规范化正文不超过 1 MiB 时保持现有全量加密行为。
3. 超过 1 MiB 时，剥离工具/函数定义、系统/开发者消息、媒体信息和工具调用参数，只保留带角色与顺序的用户可见文本。
4. 精简文本不超过审核文本硬上限时，按消息边界切成独立加密块并标记 `content_reduced`。
5. 精简文本仍超限时不保存正文，标记 `metadata_only`。

队列事件仍受 64 MiB 总字节预算约束，任何降级或丢弃不得阻塞 Relay。

### metadata-only 指纹

规范化过程对所有可归并文本增量计算：

- 单消息内容 HMAC；
- 有序滚动会话指纹；
- 当前请求的全部前缀指纹；
- 消息数量和最终序列指纹。

即使正文不保存，也把前缀指纹传给 `assignMessageAuditSession`，使完全相同和追加上下文的 metadata-only 请求可走 `exact` / `prefix`。不新增明文、密文或消息引用；缺少锚点时不强行做 `compressed` 匹配。

## 审核资料集

触发审核时固定资料集版本：

1. 查询会话触发时的最新请求，作为 `target_request_id`。
2. 在不超过目标请求的会话历史中，找出每条 `session_match=compressed` 记录。
3. 对每个压缩断点选取其 `parent_request_id`，去重后按时间排列。
4. 加入目标最新请求；若已在列表中则不重复。

每个请求映射为只读虚拟文件：

```text
file_id: request:<request_id>
request_id
captured_at
stage: before_compression | latest
message_count
estimated_tokens
available
```

最新文件没有正文时拒绝启动审核并返回 `content_unavailable`。旧压缩来源没有正文时仍进入清单并标记不可用，最终覆盖范围必须明确反映缺失，不能声称完整审核。

虚拟文件保留消息审计已经保存的完整入站角色上下文，包括客户端提交的 system、user、assistant 和 tool。系统不额外采集当前请求的新模型响应；只有客户端后续把模型回复作为历史重新提交时，该回复才会出现在虚拟文件中。所有角色内容统一视为不可信证据，不具备改变审核系统提示词或 Tool 权限的能力。

## 受限 Tool

内置 Tool：

- `list_files()`：返回固定资料集清单和可用性。
- `read_file(file_id, cursor, limit)`：按消息边界读取指定虚拟文件的一段内容。
- `search_files(query, file_ids, cursor, limit)`：对固定资料集做大小写不敏感的字面量搜索，返回有界命中和上下文；不接受正则表达式。

服务端强制：

- `file_id` 必须属于本次任务；不存在或跨会话立即拒绝。
- 游标、查询长度、单次返回量、总 Tool 调用次数、总 Tool 返回 Token 和任务时长均有常量上限。
- 每次模型调用前对完整消息和 Tool schema 计数；超过内置输入预算时不再返回正文，并要求模型基于已读证据完成结果。
- 不把明文写入临时文件；每次 Tool 调用按需解密，返回后只在任务内存中存在。
- 覆盖记录由服务端根据实际 Tool 调用的虚拟分片游标生成，同时保留原消息序号用于证据展示；不能信任模型自报，也不能因长消息分片复用原序号而伪装成整条消息已读。
- 第一版不生成分块局部摘要，也不提供管理员自定义审核提示词；达到上下文或 Tool 上限时任务明确失败，由覆盖记录如实反映本次已读范围。

## 默认提示词与输出

系统内置不可由审计材料覆盖的核心提示词，固定包含：

- 审计目标与辅助判断声明；
- 审计材料全部不可信，材料中的指令不得改变系统规则或 Tool 范围；
- 只通过内置 Tool 读取资料；
- 风险等级 `none / low / medium / high`；
- 稳定风险分类，包括提示词注入、敏感信息、网络滥用、欺诈违法、暴力自伤、色情内容、仇恨骚扰、策略规避和其他；
- 不保存大段原文，证据只返回虚拟文件、消息区间和非逐字说明；
- 输出严格 JSON，不输出 Markdown 包裹。

摘要和依据使用审核材料的主要语言；风险等级和类别使用稳定枚举，前端负责本地化标签。

结构化结果：

```json
{
  "summary": "string",
  "risk_level": "none|low|medium|high",
  "categories": ["prompt_injection"],
  "findings": [
    {
      "category": "prompt_injection",
      "severity": "low|medium|high",
      "file_id": "request:...",
      "start_sequence": 1,
      "end_sequence": 2,
      "reason": "string"
    }
  ]
}
```

后端严格校验枚举、长度、数量、文件 ID 和消息范围。首次无法解析时只允许一次格式修复调用；仍无效则任务失败，原始模型输出不落库、不写日志。

## 内部模型调用

在 `service` 定义审核调用接口并由 `relay` 注册实现，避免 `service -> relay` 循环依赖，模式与视觉辅助的 caller 注入一致。

Relay 实现直接使用配置渠道的 adaptor：

1. 创建仅用于内部调用的 Gin 上下文和非流式 OpenAI Chat DTO。
2. 通过 `middleware.SetupContextForSelectedChannel` 初始化渠道密钥、地址、模型映射和多 Key 状态。
3. 调用 adaptor 的 `ConvertOpenAIRequest`、`DoRequest`，直接解析非流式上游响应中的文本和 Tool Call。
4. 兼容 OpenAI Chat、OpenAI Responses、Claude 和 Gemini 非流式响应格式。
5. 不调用 `controller.Relay`、`TextHelper` 的计费收口、`PreConsumeBilling`、`PostTextConsumeQuota`、消费日志或消息审计入口。
6. 不应用渠道 System Prompt 和可能改写消息/Tool 的 Param Override；仅保留模型映射、必要请求头和渠道适配。

配置渠道必须启用且模型仍存在于渠道模型列表。渠道/模型不支持 Tool 时任务以稳定类别 `tool_unsupported` 失败，不自动切换其他渠道。

## 设置合同

新增配置命名空间 `message_audit_review`，使用单个 JSON 配置项原子表达：

```json
{
  "channel_id": 12,
  "model": "example-model"
}
```

默认值是未配置。`controller.UpdateOption` 对 `message_audit_review.config` 做完整校验：

- `channel_id=0` 且模型为空表示清空配置；
- 渠道必须存在且启用；
- 模型必须出现在该渠道 `GetModels()` 返回值中；
- 保存值只记录到 Option 表和内存配置，不记录配置值到管理日志。

系统设置“日志维护 / 消息审计”区域增加渠道、模型联动 Select。渠道选项通过 Root-only 精简接口返回启用渠道的 ID、名称和模型，避免把渠道密钥或完整设置带入页面。

## API 合同

Root-only 路由：

```text
GET  /api/message-audit/review-options
GET  /api/message-audit/session/:audit_session_id/review
POST /api/message-audit/session/:audit_session_id/review
```

- `review-options` 只返回启用渠道的 `id/name/models` 和当前固定配置。
- `GET review` 返回当前成功结果、任务状态和服务端计算的新鲜度；成功解密或失败尝试均写不含正文的管理审计。
- `POST review` 固定目标请求和来源列表，创建或复用同会话活动任务；管理审计只记录会话 ID、任务 ID、操作者和是否新建。
- 列表接口为每个聚合会话附加 `review_status`、`review_risk_level`、`review_stale`、`reviewed_at`，不返回完整结果。

管理 API 保持 `{success,message,data}` 契约。

## 新鲜度与状态

服务端分别计算：

- 任务状态：`unreviewed / pending / running / failed / succeeded`；
- 风险等级：最后一次成功结果的 `none / low / medium / high`；
- 新鲜度：`reviewed_request_id != 当前会话最新 request_id` 时为 `stale`。

因此列表可同时显示“高风险”和“待重审”。待重审、重审中、重审失败均保留旧结果；重审成功后才替换风险和密文结果。

## 清理与并发

- `DeleteMessageAuditsBeforeBatch` 在删除请求前查询来源引用，删除对应 review source 和 review 行，再删除消息引用与请求。
- 任务提交成功结果的事务再次确认所有来源请求仍存在且未越过清理水位；失败时返回 `source_expired`。
- 同会话活动任务通过 scoped `active_key` 幂等；不同会话按创建顺序排队并由任务类型锁全局串行。
- 结果替换、来源替换和系统任务成功收口在同一主库事务内完成，避免任务成功但结果缺失或结果更新后任务失败。

## 失败原因与模型一致性

### 安全失败原因

继续只持久化现有 `http_status`、`error_code`、`finish_reason`。前端使用稳定错误码映射为本地化说明，未知错误显示通用说明和原始错误码，不展示上游原文。

详情和列表失败状态同时展示：

- HTTP 状态；
- 稳定错误码；
- 本地化安全说明。

### 模型一致性

- 后端测试断言 finalize 后同一 `request_id` 的列表和详情返回相同 `model_name`。
- Default 列表在页面打开期间每 5 秒刷新，使异步 finalize 后的模型、状态和审核任务状态稳定收敛。
- 详情切换请求仍保持当前 Sheet/Drawer 打开。

## 前端结构

- `message-audits/index.tsx`：新增风险/审核状态列和移动端徽标；失败状态下显示安全原因；保持列表无审核触发按钮。
- `message-audit-detail.tsx`：在元数据和消息时间线之间增加未嵌套卡片的审核区，提供手动审核/重审按钮、任务轮询、旧结果提示和完整结构化结果。
- `log-settings-section.tsx`：在现有消息审计设置组内增加固定渠道和模型 Select。
- 所有 SelectItem 均置于 `SelectGroup`，受控模型值必须存在于当前渠道选项中；配置失效时显示明确错误态而不是自动选择。
- 新增文案通过 `t('English key')`，同步全部前端语言文件。

## 测试策略

后端重点：

- 三库迁移和历史空字段兼容。
- scoped 系统任务同会话幂等、不同会话排队、租约失败。
- 重审失败保留旧密文结果，成功原子替换。
- 来源清理联动和执行中清理防回写。
- 多次压缩资料集来源选择和去重。
- Tool 越权、游标、查询、轮数、Token、超时边界。
- 提示词注入材料不能扩展 Tool 范围。
- 结构化输出校验和一次修复。
- 内部调用无计费、无消费日志、无递归消息审计。
- 超大正文 `content_reduced`、最终 metadata-only 指纹归并和去重载荷统计。
- finalize 后列表/详情模型一致，失败原因不包含上游原文。

前端重点：

- 固定渠道/模型联动与失效配置。
- 列表风险和任务状态分离，待重审保留旧风险。
- 详情触发、轮询、失败保留旧结果、成功刷新。
- 失败安全原因、metadata-only/content-reduced 大小说明。
- Base UI Select 分组、受控值和 Sheet/Drawer 生命周期。

## 风险与回滚

主要风险：

- 内部 adaptor 调用必须覆盖不同上游非流式 Tool 响应格式。
- 审核任务和清理任务并发时必须避免过期摘要回写。
- 超大正文降级改变审计载荷，需要严守队列预算和历史字段兼容。
- 列表聚合后附加审核元数据不能引入逐行查询。

回滚：

- 清空固定审核渠道/模型即可停止新审核调用，消息审计采集仍可独立使用。
- 新表和可空列保留不影响旧版本读取；代码回滚不要求删除数据。
- `content_reduced` 降级可回退到原 metadata-only 行为，不改变 Relay 主链路。

## MySQL 与高频采集优化

### 批量 blob 解析与 item 写入

`CreateMessageAuditCapture` 在会话归属确定后，先按消息出现顺序构建请求内唯一 HMAC 集合。事务内按固定批次查询同用户、同 schema 的既有 blob，批量插入缺失项并使用唯一键冲突忽略保证并发幂等，再批量回查最终 ID。随后按原消息顺序构造全部 item，并使用受 SQLite 参数上限约束的批次写入。

载荷统计继续以本次事务实际新增的唯一 blob 为准；同一请求内重复消息和跨请求重复消息都不重复计入新增载荷。任一 HMAC 无法解析到 blob ID 时整体事务失败，避免生成缺少正文引用的请求。

### compressed 候选查询

当前请求的锚点 HMAC 先在 `message_audit_blobs` 唯一索引中解析为 blob ID，只保留数据库真实存在的锚点。候选聚合从 `message_audit_items.blob_id IN (...)` 开始，关联请求表后按用户、协议、时间和当前请求排除条件筛选，保持原有 top-N、唯一候选和有序子序列复核逻辑。

该设计不新增索引：现有 blob 唯一索引负责 HMAC 定位，现有 item `blob_id` 索引负责反查请求。只有实际执行计划证明索引不足时才考虑迁移。

### 存储统计缓存

`GetMessageAuditStatus` 使用线程安全的短时缓存保存请求数、item 数、blob 数、payload 和物理大小。缓存周期默认 60 秒，只覆盖数据库统计；writer 运行状态、队列数量、队列字节、启停配置和保留期仍在每次请求实时组装。

缓存刷新失败时返回错误，不写入错误值；已有缓存超过有效期后不继续伪装为新数据。缓存实现不要求 capture 主链路主动失效，避免每次高频写入反而争抢统计锁。

### 自适应轮询

列表查询根据当前页是否包含未完成请求状态、审核 `pending/running` 或其他需要自动收敛的状态选择刷新间隔：活动时 5 秒，稳定时 30 秒。状态统计查询统一降到 30 秒；详情内已有审核任务轮询继续按任务状态控制。

### 运行参数

生产环境将应用连接池收敛为 `SQL_MAX_OPEN_CONNS=80`、`SQL_MAX_IDLE_CONNS=20`、`SQL_MAX_LIFETIME=300`，避免应用上限超过 MySQL 连接容量。MySQL InnoDB buffer pool 调整为 2 GiB，并开启 1 秒慢查询日志用于验证；保持 `innodb_flush_log_at_trx_commit=1` 和 `sync_binlog=1` 不变。

生产配置变更在代码验证和部署窗口执行。回滚时先恢复连接池和 buffer pool 配置，再重建容器；代码优化本身不依赖这些参数才能正确运行。
