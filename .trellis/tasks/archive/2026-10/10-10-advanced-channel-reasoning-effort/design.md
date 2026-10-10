# 高级渠道思考强度日志设计

## 范围与数据流

客户端参数 → 模型映射及高级渠道转换 → 禁用字段/param_override → 明确强度同步到 RelayInfo → GenerateTextOtherInfo → 既有日志详情。

- 在 relay 层复用统一的明确强度读取行为，各请求 handler 在最终 JSON 已就绪时调用。高级渠道的 converter=None 与协议转换路径共同覆盖，不再用 GPT 模型名前缀限定可观测性。
- 使用已定义的 `RelayInfo.GetFinalRequestRelayFormat()` 识别最终协议，避免入站 OpenAI 转 Claude 后仍读取顶层字段。
- OpenAI Chat 使用 `reasoning_effort`；OpenRouter 使用其最终 `reasoning.effort`；Responses 使用 `reasoning.effort`；Claude 使用 `output_config.effort`；Gemini 使用 `generationConfig.thinkingConfig.thinkingLevel`。已解析 Gemini DTO 兼容蛇形字段，透传取值保留该兼容行为。
- 仅同步明确 JSON 字符串，原值写入日志，不新增供应商枚举或默认等级。没有对应字段时清空本次受支持路径的旧值；未知协议不根据其他字段猜测。

## 兼容边界

- GPT 后缀和各适配器已有参数规范化先执行，日志取值反映其最终结果。既有 Anthropic 专属 helper 的非 Anthropic 隔离合同保留；新的高级渠道处理不可直接放宽旧 helper 的类型条件而破坏回归。
- 透传路径复用已解析 DTO 的明确字段，不为了日志再次消费 body reader；参数覆盖原本不在透传路径执行，日志不能假称覆盖已发生。
- 同步/流式共享请求准备阶段；每次渠道尝试重新计算，避免前一尝试的强度污染后一次。
- `other.reasoning_effort` 仍是显式出站参数记录，不是上游返回的实际思考预算，也不证明模型一定遵从参数。
- UI 已按字段非空展示，预期无需前端、schema、i18n 或数据库迁移。

## 实现候选与验证

- 主要候选：relay 层强度同步 helper、`compatible_handler.go`、`responses_handler.go`、`claude_handler.go`、`gemini_handler.go`，以及必要的尝试状态初始化与测试。
- 使用真实高级渠道适配器和参数覆盖链、本机 HTTP mock 断言最终正文与消费元数据；覆盖转换、字段删除、透传、重试与流式，保持计费字段不变。
- 从当前已核实 `build-bak` 基线建立本任务独立分支；保留现有 Python 缓存及其他任务文件，不改共享任务树。
- 此阶段只规划和本地实现；未来发布按仓库流程单独核对范围，不能把测试通过写成生产已生效。

## 上游同步与回滚边界

- 新建 `relay/advanced_channel_reasoning_effort.go` 承载高级渠道的最终正文与透传 DTO 取值，测试独立放置；既有 Anthropic helper 保持原合同。
- 四个协议 handler 只在最终正文准备完成和透传分支增加同步调用；Chat 经 Responses 的桥接路径同样在最终正文处接入，不重构原请求准备流程。
- `controller/relay_attempt.go` 的每次尝试初始化清理思考强度与最终协议状态，避免上一次 Claude 响应或转换链污染下一次渠道尝试；不修改计费快照清理。
- 复用 gjson、已定义 DTO、最终协议接口和现有日志生成函数。原有文件冲突面限于上述调用点；撤销调用与新增文件即可回滚。同步上游后复核覆盖顺序、透传使用原入站 DTO、桥接路径及重试初始化。

## 最终交付分支

- 用户补充要求：提交标题带 `[build]`，交付到 `build-bak`。经更新后的精确计划确认，本次实现与规范已提交为 `cb68d9cab41b3894ab4c22e5b53342c32b6a1b5d`，并推送至 `origin/build-bak`；既有自动归档提交一并交付。
- 复用现有 `docker-image-build.yml` 的 `[build]` 条件触发镜像构建，不修改 CI；构建运行编号为 `38049320240`。用户随后明确要求构建完成后同步 ACR 并发布 ACK 的 new-api，发布与真实日志验收按运维流程另行执行，当前尚未完成。
