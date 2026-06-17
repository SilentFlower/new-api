# 渠道级视觉辅助识别实现计划

## 实现步骤

1. 补充配置 DTO
   - 在 `dto/channel_settings.go` 增加 `ChannelVisionAssistSettings`。
   - 在前端渠道类型定义中增加对应字段。
   - 如本轮实现包含前端配置入口，则在渠道编辑抽屉中增加配置项；否则先允许 JSON 配置生效。

2. 抽出渠道预处理入口
   - 新增 `relay.PrepareRequestForSelectedChannel(c, info)`。
   - 将 `info.InitChannelMeta(c)` 和 `helper.ModelMappedHelper` 前移到该入口。
   - 在 `TextHelper` / `ClaudeHelper` 内避免重复执行同一预处理逻辑。
   - 确保 `info.UpstreamModelName` 是 `model_mapping` 链式映射后的最终模型。

3. 调整 controller 主流程
   - 在首个渠道选中后、token meta 构造前调用预处理。
   - retry 换渠道后重新调用预处理。
   - 确保预处理后的 `relayInfo.Request` 用于敏感词检查、token 估算、预扣费和实际转发。

4. 实现图片提取和请求改写
   - OpenAI：支持 `image_url` 字符串和对象形式。
   - Claude：支持 `type=image` 且 `source.url` / `source.data`。
   - 实现 `StripImage` 行为。
   - 保持非图片内容顺序和文本内容不丢失。

5. 实现缓存
   - 构造稳定 key。
   - 优先 Redis，退化内存缓存。
   - 同一请求内使用 map 去重，避免同图并发重复调用。
   - 缓存值只保存描述和元信息。

6. 实现辅助视觉调用
   - 使用 `AssistChannelId` 强绑定辅助渠道。
   - 构造非流式 OpenAI 兼容 chat completions 请求。
   - 设置防循环 context 标记。
   - 保存并恢复主请求渠道相关 context key，避免污染后续转发。
   - 解析辅助模型文本响应。

7. 实现计费和日志
   - 辅助视觉调用按实际使用的 `AssistChannelId` 和 `AssistModel` 单独向用户扣费。
   - 辅助日志标记为 `vision_assist`。
   - 缓存命中不重复扣辅助费用。
   - 辅助调用失败时按 `FailurePolicy` 处理，并确保已预扣辅助费用可退款。
   - 记录辅助渠道、辅助模型、目标渠道、重定向后模型、缓存命中、耗时和错误。

8. 测试
   - OpenAI 请求：图片提取、文本回写、移除图片、保留图片。
   - Claude 请求：图片提取、文本回写、移除图片、保留图片。
   - `model_mapping`：按链尾 `UpstreamModelName` 匹配触发。
   - 缓存：同图、同 prompt 命中；不同 prompt 或不同辅助模型不命中。
   - 防循环：内部辅助请求不再次触发。
   - 透传：全局或渠道透传开启时不改写。

## 验证命令

```bash
go test ./dto ./relay/... ./service/... ./controller/...
```

如果涉及前端配置 UI：

```bash
cd web/default
bun run build
```

## 风险文件

- `controller/relay.go`
- `relay/compatible_handler.go`
- `relay/claude_handler.go`
- `relay/helper/model_mapped.go`
- `relay/common/relay_info.go`
- `dto/channel_settings.go`
- `dto/openai_request.go`
- `dto/claude.go`
- `service/vision_assist.go`

## 回滚点

- 配置字段新增应保持 `omitempty`，关闭或未配置时不改变现有行为。
- 预处理入口必须以“未开启视觉辅助时行为完全一致”为第一回滚判断。
- 若辅助调用计费实现风险过高，可先保留功能开关关闭并只合入解析/改写/缓存基础能力。

## 实现前阻塞问题

- 无。
