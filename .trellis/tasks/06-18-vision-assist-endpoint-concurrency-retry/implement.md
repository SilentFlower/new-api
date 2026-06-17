# 实施计划

## 实施顺序

1. 扩展配置 DTO 与归一化 helper
   - 在 `dto.ChannelVisionAssistSettings` 增加端点模式、并发、重试字段。
   - 在 `service/vision_assist.go` 增加模式常量与归一化函数。
   - 补默认兼容测试。

2. 抽象单图辅助执行结果
   - 为未命中图片建立任务结构和结果结构。
   - 保留请求内重复图片复用逻辑。
   - 保证最终结果按图片 `Index` 稳定排序。

3. 增加有限并发 worker
   - 使用归一化后的 `max_concurrency` 控制 worker 数量。
   - 每个 worker 独立执行单图辅助请求。
   - 避免多个 goroutine 共享可变 Gin context。

4. 增加重试逻辑
   - 增加可恢复错误判断 helper。
   - 对每张图按 `retry_count` 和 `retry_backoff_ms` 重试。
   - 尊重请求取消。
   - 覆盖 429/5xx 可重试、400 不重试。

5. 增加端点模式解析与执行
   - `auto` 根据辅助渠道类型解析实际模式。
   - 保留 `openai_chat` 兼容现有路径。
   - 实现或接入 `gemini_native`。
   - 评估并实现 `anthropic_messages` 与 `openai_responses` 的最小可用路径。

6. 补充日志字段
   - 成功和失败都写入端点模式、并发、重试、失败统计。
   - 错误摘要截断，不包含图片内容。

7. 更新默认 UI
   - 类型、表单默认值、校验、setting JSON 读写、抽屉控件。
   - 补 i18n。

8. 更新经典 UI
   - `EditChannelModal.jsx` 的默认值、读取、保存、重置、清理、UI 控件。
   - 补 classic locale。

9. 测试与验证
   - 后端单元测试覆盖默认兼容、重复图片缓存、并发限制、重试分类、skip/error 策略。
   - 前端至少运行 i18n 同步/静态检查；环境可用时运行构建。

## 验证命令

```bash
go test ./service ./relay
```

如果改动 Gemini / Claude / Responses 适配器：

```bash
go test ./relay/channel/gemini ./relay/channel/claude ./service ./relay
```

默认 UI：

```bash
cd web/default && bun run i18n:sync
cd web/default && bun run build
```

经典 UI：

```bash
cd web/classic && bun run build
```

当前环境如果缺少 `bun` 或 `node_modules`，需要在检查结果中明确说明未运行原因。

## 风险文件

- `service/vision_assist.go`
- `relay/vision_assist.go`
- `relay/channel/gemini/*`
- `relay/channel/claude/*`
- `dto/channel_settings.go`
- `web/default/src/features/channels/types.ts`
- `web/default/src/features/channels/lib/channel-form.ts`
- `web/default/src/features/channels/components/drawers/channel-mutate-drawer.tsx`
- `web/classic/src/components/table/channels/modals/EditChannelModal.jsx`

## 回归重点

- 历史渠道未配置新增字段时，视觉辅助仍按旧逻辑可用。
- `max_concurrency=1` 时行为接近原串行实现。
- `max_concurrency>1` 时结果顺序不乱，缓存不污染。
- Gemini 选择 `gemini_native` 后不再走 OpenAI Chat Completions 语义。
- 400 `INVALID_ARGUMENT` 不被无意义重试。
- 429/5xx 可按配置重试。
- 两套 UI 保存后不丢 `setting` 和 `vision_assist` 的未知字段。
