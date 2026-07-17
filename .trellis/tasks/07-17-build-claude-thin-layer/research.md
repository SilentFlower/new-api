# Claude 主链路薄层化研究

## 当前证据

- `relay/websearch/` 已独立承载 Tavily、AnySearch、纯 WebSearch 识别和 Claude 响应构造。
- `relay/claude_handler.go` 仍直接定义 Reasoning Effort 同步、WebSearch 模拟执行和流式响应写入；相对 `origin/main` 当前约 `+127/-4`。
- `controller/channel.go` 仍直接定义 Channel setting JSON 解析、WebSearch API Key 继承/清空、响应脱敏和批量响应副本；相对 `origin/main` 当前约 `+185/-14`。
- `dto/channel_settings.go` 直接包含约 120 行 WebSearch 类型、常量和归一化逻辑；`ChannelSettings` 本身只需要保留一个 `WebSearch` 字段。
- `controller/channel_websearch_test.go` 已保护密钥脱敏、继承和显式清空。
- `relay/claude_handler_test.go` 已保护本地模拟开关、官渠原生透传和 Reasoning Effort 参数覆盖结果。
- `relay/websearch/*_test.go` 已保护请求识别、不修改原始 Claude 请求、Provider 鉴权和响应构造。

## 精确职责清单

### Claude handler 中的 build 职责

- `syncAnthropicReasoningEffort`
- `syncAnthropicReasoningEffortFromRequestBody`
- `shouldHandleClaudeWebSearchEmulation`
- `handleClaudeWebSearchEmulation`
- `writeClaudeWebSearchStream`

### Channel controller 中的 build 职责

- `sanitizeChannelForResponse` / `sanitizeChannelsForResponse`
- Channel setting JSON record 的解析与回写
- WebSearch setting 的提取、归一化和写回
- 响应 API Key 脱敏
- 创建时校验与更新时真实 API Key 继承/显式清空

## 结构结论

- 所有上述函数均可在不改变 package、签名和调用顺序的前提下迁移到同 package 独立文件。
- `relay/claude_handler.go` 只需保留三个窄接入：纯 WebSearch 条件分派、透传 body effort 同步、最终 JSON effort 同步。
- `controller/channel.go` 只需保留列表/详情响应脱敏调用、创建归一化调用和更新密钥合并调用。
- `dto/channel_settings.go` 只需保留 `ChannelSettings.WebSearch` 字段；WebSearch 类型和方法可迁入独立 DTO 文件。
- 不需要新接口、配置、数据库、路由或前端修改。

## 风险

- 脱敏必须操作结构体副本，不能把空 API Key 写回数据库模型。
- 更新空 API Key 必须继承旧值，只有 `clear_api_key=true` 才能清空。
- WebSearch 本地短路必须继续发生在 converter、参数覆盖和上游 body 构造之前。
- Reasoning Effort 非透传路径必须读取参数覆盖后的最终 JSON；透传路径只能读取已解析 DTO。
- 迁移不得改变未启用 WebSearch 时的原生上游透传。

## 治理前差异

```text
controller/channel.go        +185/-14
relay/claude_handler.go      +127/-4
dto/channel_settings.go      +157/-6（包含其他 build 配置）
```

实施后以这三个原有文件的净减少量和剩余调用可解释性作为冲突面验收依据。

## 治理前基线

2026-07-17 已执行：

```bash
go test ./dto ./controller ./relay ./relay/websearch ./service -count=1
go test -race ./relay -run 'ClaudeWebSearch|AnthropicReasoningEffort|SyncAnthropic' -count=1
```

五个相关包完整回归和 Relay 定向 race 全部通过，可作为结构迁移前行为快照。

## 实施结果

- 函数体等价性复核：WebSearch 迁移块逐字一致；DTO 类型与测试逐字一致；Controller 与 effort 迁移块仅减少文件边界处空行。
- 三个原有热点文件相对 `origin/main` 的当前差异：

```text
controller/channel.go        +25/-14
relay/claude_handler.go      +16/-4
dto/channel_settings.go      +26/-6
```

- 相关五包完整回归、定向 race、20 次重复行为测试、定向 vet、gofmt 与 `git diff --check` 通过。
- `go test ./... -count=1` 除根包缺少 ignored 的 `web/classic/dist/index.html` 外，其余包全部通过；该阻断与治理前基线一致。
