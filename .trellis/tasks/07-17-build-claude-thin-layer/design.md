# Claude 主链路 Build 薄层化设计

## 1. 目标结构

### 新建文件

| 文件 | 职责 |
| --- | --- |
| `relay/claude_websearch_emulation.go` | 本地模拟开关、Provider 调用、Claude JSON/SSE 响应和成功计费 |
| `relay/claude_reasoning_effort.go` | 从 DTO 或最终上游 JSON 同步 Anthropic Reasoning Effort 日志字段 |
| `controller/channel_websearch_setting.go` | Channel WebSearch setting 解析、回写、创建校验、更新密钥合并和响应脱敏 |
| `dto/channel_websearch_settings.go` | WebSearch 配置 DTO、常量、归一化、校验和 Provider 判断 |
| `dto/channel_websearch_settings_test.go` | WebSearch DTO 的归一化与校验契约 |

### 修改的原有文件

| 文件 | 只保留的接入点 |
| --- | --- |
| `relay/claude_handler.go` | WebSearch 条件分派；透传与非透传路径各一次 effort 同步调用 |
| `controller/channel.go` | 查询响应脱敏、创建归一化、更新密钥合并调用 |
| `dto/channel_settings.go` | `ChannelSettings.WebSearch` 字段 |
| `dto/channel_settings_test.go` | 移除已迁到独立测试文件的 WebSearch DTO 测试 |

## 2. 行为数据流

### WebSearch Relay

`ClaudeHelper` → 纯 WebSearch 检测 → 渠道开关检测 → 独立模拟函数 → Provider → Claude 响应 → 原有 `PostTextConsumeQuota`。

未启用、混合工具或非搜索请求继续进入原有 converter、disabled fields、参数覆盖和上游请求链路。

### Reasoning Effort

- 请求体透传：解析后的 `ClaudeRequest.OutputConfig` → 独立同步函数 → `RelayInfo.ReasoningEffort`。
- 普通路径：最终参数覆盖 JSON → 独立同步函数 → `RelayInfo.ReasoningEffort`。

### Channel setting

- 查询：原始 Channel → 浅拷贝 → 清理 MultiKey 临时信息 → 独立 WebSearch 脱敏 → API 响应。
- 创建：请求 setting → 归一化/校验 → 移除前端状态字段 → 保存真实 key。
- 更新：请求 setting + 原始 Channel → 显式清空或继承旧 key → 归一化/校验 → 保存。

## 3. 兼容性

- 函数签名、错误类型、错误消息、状态码和调用顺序不变。
- Channel setting 继续存储为原 JSON 文本，不增加迁移。
- 未配置 `web_search` 的旧 Channel 保持无操作。
- 所有 JSON marshal/unmarshal 继续使用 `common` 包装。
- 不改变前端字段或 Provider 协议。

## 4. 回滚

- 将独立文件中的函数体恢复到原文件原位置。
- 恢复 DTO 类型和测试到 `channel_settings.go` / `channel_settings_test.go`。
- 删除新增文件即可回到任务前结构，不涉及数据回滚。

## 5. 上游同步复核点

- 上游 `ClaudeHelper` 是否调整 converter、透传或参数覆盖顺序。
- 上游 Channel CRUD 是否调整响应副本、复制渠道或 setting 保存顺序。
- `ChannelSettings` JSON 解析方式是否变化。
- Claude 流式响应写入和计费入口是否变化。
