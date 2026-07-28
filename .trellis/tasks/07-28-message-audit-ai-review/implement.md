# 实施计划

## 实施顺序

1. 修正消息审计载荷与会话元数据
   - 扩展 `MessageAuditRequest` 的可空载荷统计字段并加入三库迁移。
   - 重构规范化流程，完整快照超限时生成有界 `content_reduced` 文本。
   - 对最终 metadata-only 请求继续计算滚动 HMAC 和前缀指纹，支持 exact/prefix 会话归并。
   - 在事务中记录新增密文载荷与去重节省字节，兼容历史 `nil` 字段。

2. 增加审核结果和来源模型
   - 新建 `MessageAuditReview`、`MessageAuditReviewSource` 及查询/加密结果持久化方法。
   - 将新表注册到 `model/main.go` 的迁移和缺失表检查。
   - 增加按会话批量读取审核元数据，避免列表 N+1。

3. 扩展 scoped 系统任务
   - 为 `SystemTask` 增加自定义 `active_key` 创建和按 key 查询能力，旧调用保持原行为。
   - 新增 `message_audit_review` 任务类型和按会话幂等创建事务。
   - 保持任务类型级锁，实现不同会话排队、审核任务全局串行。
   - 增加结果、来源和任务成功状态的原子收口方法。

4. 实现固定资料集与受限 Tool
   - 根据每个 compressed 断点的 `parent_request_id` 和目标最新请求生成虚拟文件清单。
   - 实现只读 `list_files`、`read_file`、`search_files`，限制文件范围、游标、查询长度和返回量。
   - 由服务端记录实际读取覆盖范围，不信任模型自报。
   - 增加输入 Token、Tool 总返回 Token、轮数和超时硬上限。

5. 实现默认提示词与结构化结果
   - 内置审核系统提示词、风险枚举、类别、注入防护和 JSON schema。
   - 实现 Tool 循环、最终 JSON 严格解析、范围校验和一次格式修复。
   - 完整结果使用独立派生审核密钥加密，列表元数据保持无正文。

6. 实现内部无计费模型调用
   - 在 service 定义 caller 接口，relay 注册具体 adaptor 调用实现。
   - 复用渠道上下文、模型映射、请求转换和上游请求能力。
   - 解析 OpenAI Chat/Responses、Claude、Gemini 非流式文本和 Tool Call。
   - 明确绕过公开 Relay、预扣/结算、消费日志、Token/成本记录、渠道 System Prompt、Param Override 和消息审计 capture/finalize。

7. 增加固定审核配置与管理 API
   - 新增 `message_audit_review.config` 默认配置和后端完整校验。
   - 增加精简的启用渠道/模型选项接口。
   - 增加会话审核查询和手动触发接口，全部使用 RootAuth。
   - 管理审计只记录会话、任务、操作者、状态和错误类别。

8. 接入清理与状态派生
   - 删除消息审计请求前同步删除引用该请求的审核结果。
   - 任务成功提交前复核来源仍存在，阻止清理后回写。
   - 列表和详情计算任务状态、旧风险、新鲜度并保持互相独立。

9. 修复失败原因和模型展示
   - Default 列表/详情展示 HTTP 状态、错误码和本地化安全说明。
   - metadata-only/content-reduced 明确展示原始大小、实际保留明文和新增密文载荷。
   - 列表和状态统计只通过页面刷新按钮按需更新，避免后台刷新触发表格加载态。

10. 完成 Default 前端
    - 系统设置消息审计区域增加固定渠道、模型联动 Select。
    - 列表增加审核状态和风险列，移动端同步展示，列表不放触发按钮。
    - 详情增加审核区、手动审核/重审、任务轮询、旧结果和覆盖信息。
    - 按 Base UI 组合规则维护 SelectGroup、受控 value 和 Sheet/Drawer 生命周期。

11. 完成 i18n 与测试
    - 使用 i18n 同步流程补全全部前端语言。
    - 增加 model/service/relay/controller 和前端行为回归测试。
    - 验证 SQLite；MySQL/PostgreSQL 使用隔离 DSN，有 DSN 才声明实测。

12. 优化高频采集与查询放大
    - 请求内按 HMAC 去重，分批查询已有 blob、插入缺失 blob、回查最终 ID。
    - 按原序列批量写入 message item，使用兼容 SQLite 参数上限的固定批次。
    - compressed 匹配先解析 blob ID，再通过 item 的 `blob_id` 索引缩小候选范围。
    - 保持 exact/prefix/compressed/new 语义、去重载荷统计和清理并发合同不变。

13. 降低管理端统计与刷新压力
    - 为昂贵的存储 COUNT/SUM/物理大小查询增加 60 秒线程安全缓存。
    - 状态接口实时返回 writer/queue 字段，缓存仅覆盖持久化统计。
    - Default 列表和状态统计移除自动轮询，保留页面手动刷新；详情审核任务仅在活动状态轮询自身结果。
    - 审核渠道忽略原生 Tool 时由 relay 返回降级信号，service 构造严格 JSON 文本工具协议并对实际发送载荷重新执行工具范围和预算校验。
    - 清理批次按实际引用的 blob ID 定向回收孤儿记录，任务末尾只保留一次历史孤儿兜底扫描。
    - 人工清空改走整表快速清理并暂停本节点审计落库，定时保留期清理继续走分批路径。

14. 生产参数调优与验证
    - 代码验证通过后设置应用 SQL 最大连接 80、空闲连接 20、生命周期 300 秒。
    - MySQL buffer pool 调整为 2 GiB，开启 1 秒慢查询日志，保持事务与 binlog 耐久性配置不变。
    - 记录变更前后连接、buffer pool、慢查询和消息审计表读写指标；生产重启前确认配置文件和回滚方式。

## 主要文件

后端：

- `model/message_audit.go`
- `model/message_audit_review.go`
- `model/system_task.go`
- `model/main.go`
- `service/message_audit.go`
- `service/message_audit_review.go`
- `service/message_audit_cleanup.go`
- `relay/message_audit_review.go`
- `controller/message_audit.go`
- `controller/option.go`
- `router/api-router.go`
- `setting/operation_setting/message_audit_review_setting.go`

前端：

- `web/default/src/features/message-audits/types.ts`
- `web/default/src/features/message-audits/api.ts`
- `web/default/src/features/message-audits/lib/message-audit-ui.ts`
- `web/default/src/features/message-audits/components/message-audit-detail.tsx`
- `web/default/src/features/message-audits/index.tsx`
- `web/default/src/features/system-settings/maintenance/log-settings-section.tsx`
- `web/default/src/features/system-settings/operations/index.tsx`
- `web/default/src/features/system-settings/operations/section-registry.tsx`
- `web/default/src/features/system-settings/types.ts`
- `web/default/src/i18n/locales/*.json`

## 验证命令

后端定向：

```bash
go test ./model ./service ./relay ./controller -run 'MessageAudit|SystemTask' -count=1
go test -race ./model ./service -run 'MessageAudit' -count=1
go vet ./model ./service ./relay ./controller ./router
```

后端全量：

```bash
go test ./... -count=1
```

前端：

```bash
cd web/default
bun test src/features/message-audits
bun run i18n:sync
bun run typecheck
bun run lint
bun run build
```

通用：

```bash
git diff --check
```

## 回归检查重点

- 未配置审核渠道/模型时只能提示前往设置，不自动猜测渠道。
- 同会话重复触发复用活动任务，不覆盖并发结果；不同会话按序排队。
- 待重审、重审中和重审失败仍显示旧风险与旧结果。
- 新结果成功后风险、密文结果、来源引用和任务状态一致提交。
- 多次压缩资料集包含每个压缩前最后一版和触发时最新一版，且不复制正文。
- 普通审计记录按完整入站角色上下文提供虚拟文件；模型回复仅在客户端作为历史重新提交时出现，界面不再声称“仅分析用户输入”。
- Tool 不能访问其他会话、真实路径、网络或任意数据库数据。
- 审计材料中的提示词不能改变系统规则或 Tool 参数边界。
- 管理端只配置固定渠道和模型，不提供自定义审核提示词；正文通过虚拟分片按需读取，达到上下文或 Tool 上限时明确失败。
- 内部调用不产生消费日志、扣费、Token/成本记录或递归消息审计。
- 原 1 MiB 附近的大请求优先保存精简文本；真正 metadata-only 仍可 exact/prefix 归并。
- 失败列表显示安全原因但不泄露上游原文。
- finalize 后列表和详情的同一请求模型最终一致。
- 清理来源正文后不存在残留审核摘要，也不会被运行中任务重新写回。
- 大上下文 capture 使用批量 SQL，单请求重复 HMAC 不重复插入 blob，item 顺序与原请求一致。
- compressed 候选从 blob ID 索引反查，仍通过完整有序子序列确认，避免把共享少量消息的无关请求误归并。
- 存储统计缓存命中时不重复执行全表统计，过期刷新失败不会写入错误值。
- 列表和状态统计不会自动刷新；管理员点击刷新后能收敛 finalize 和审核结果。
- 原生 Tool 被忽略时，文本工具协议仍能完成受限工具循环并生成通过结构校验的结果。
- 文本 Tool 协议附加的系统指令和工具定义计入每次模型调用的上下文预算；接近预算边界时必须在调用前返回 `context_limit`。
- MySQL 人工清空通过单条原子换表切换全部审计表，其他实例的写入不能落入逐表清空间隙；外部 DSN 测试同时覆盖快速清空。
- 清空任务不再以小批次反复扫描整张 blob 表作为主要回收方式。
