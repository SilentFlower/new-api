# Claude 主链路 Build 薄层化实施计划

## 1. 固化治理前基线

- [x] 运行 DTO、Controller、Relay、WebSearch 与 Service 完整回归。
- [x] 运行 Claude WebSearch 与 Reasoning Effort 定向 race。
- [x] 记录三个上游热点文件的治理前差异。

验证：

```bash
go test ./dto ./controller ./relay ./relay/websearch ./service -count=1
go test -race ./relay -run 'ClaudeWebSearch|AnthropicReasoningEffort|SyncAnthropic' -count=1
```

## 2. 迁移 DTO WebSearch 定义

- [x] 新建 `dto/channel_websearch_settings.go`，原样迁移 WebSearch 常量、类型和方法。
- [x] 新建 `dto/channel_websearch_settings_test.go`，迁移现有三组 DTO 测试。
- [x] `dto/channel_settings.go` 只保留 `ChannelSettings.WebSearch` 字段。
- [x] 不改变任何 JSON tag、默认值、校验或错误消息。

## 3. 迁移 Channel setting 管理

- [x] 新建 `controller/channel_websearch_setting.go`，原样迁移响应副本、setting record、脱敏、创建归一化和更新密钥合并函数。
- [x] `controller/channel.go` 保留现有调用位置和返回行为。
- [x] 保持复制渠道读取真实 key、API 响应不泄漏 key、未知 setting 字段不丢失。

验证：

```bash
go test ./dto ./controller -run 'ChannelWebSearch|SanitizeChannel' -count=1
```

## 4. 迁移 Claude WebSearch 与 Effort

- [x] 新建 `relay/claude_websearch_emulation.go`，迁移三个 WebSearch 模拟函数。
- [x] 新建 `relay/claude_reasoning_effort.go`，迁移两个 effort 同步函数。
- [x] 清理 `relay/claude_handler.go` 不再需要的 import，不移动其余代码。
- [x] 保持 WebSearch 短路位置和 effort 同步位置完全不变。

验证：

```bash
go test ./relay ./relay/websearch ./service -run 'ClaudeWebSearch|AnthropicReasoningEffort|SyncAnthropic' -count=1
```

## 5. 完整回归与冲突面检查

- [x] 运行相关包完整回归和定向 race。
- [x] 运行 `go vet ./dto ./controller ./relay ./relay/websearch ./service`。
- [x] 执行 `gofmt` 和 `git diff --check`。
- [x] 对比治理前后 `controller/channel.go`、`relay/claude_handler.go`、`dto/channel_settings.go` 差异。
- [x] 逐项复核 WebSearch 原生透传/模拟、密钥脱敏/继承/清空、effort 参数覆盖和日志行为。

最终验证：

```bash
go test ./dto ./controller ./relay ./relay/websearch ./service -count=1
go test -race ./relay -run 'ClaudeWebSearch|AnthropicReasoningEffort|SyncAnthropic' -count=1
go vet ./dto ./controller ./relay ./relay/websearch ./service
git diff --check
```

## 6. Review Gates

- [x] Gate A：迁移前基线全部通过。
- [x] Gate B：DTO 与 Channel setting 迁移后密钥和未知字段契约不变。
- [x] Gate C：Claude 迁移后模拟/透传/effort 行为不变。
- [x] Gate D：原上游文件只保留窄调用，无无关重排或业务修复。
