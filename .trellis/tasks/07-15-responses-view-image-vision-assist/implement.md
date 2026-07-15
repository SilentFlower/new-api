# 实施计划：修复 Responses 工具输出识图辅助

## 实施步骤

1. 扩展 Responses 图片抽取：
   - 保留普通消息 `content` 扫描。
   - 为 `function_call_output`、`custom_tool_call_output` 增加 `output` 数组扫描。
   - 复用现有图片字段解析和顶层索引定位。
2. 扩展 Responses 请求改写：
   - 普通消息继续原位注入。
   - `function_call_output` 在原 `output` 中注入描述。
   - `custom_tool_call_output` 按配置处理原图片，并在其后追加普通用户描述消息。
   - 只有实际改写时才重新序列化 `Input`。
3. 增加确定性单元测试：
   - 直接 `view_image` 风格的 `function_call_output` 字符串图片 URL。
   - 统一 `exec` 风格的 `custom_tool_call_output` 对象图片 URL、detail 和 MIME 类型。
   - `strip_image=true` 与 `strip_image=false`。
   - 保留工具输出原有 `input_text` 及顺序。
   - 无图片工具输出不调用 caller、不改写请求。
   - 同一工具输出重复图片只调用一次 caller，并完整写回编号结果。
4. 运行格式与定向验证：
   - `gofmt -w service/vision_assist.go service/vision_assist_test.go`
   - `go test ./service -run 'VisionAssist|OpenAIResponses'`
   - `go test ./dto ./service ./relay`
5. 运行全仓回归与差异检查：
   - `go test ./...`
   - `git diff --check`
6. 按质量检查结果修复问题，并重新执行受影响范围和全仓验证。

## 风险文件

- `service/vision_assist.go`：共享 OpenAI Chat、Responses、Claude 的视觉辅助执行链，改写错误可能造成图片未移除、识图文本丢失或重复插入。
- `service/vision_assist_test.go`：测试需断言可观察的请求 JSON 和 caller 次数，避免只锁定私有实现细节。

## 实施约束

- 开始编码前重新读取 `dto.OpenAIResponsesRequest`、`VisionAssistImage`、抽取/改写函数的当前定义，禁止猜测字段和签名。
- JSON 编解码只能使用 `common.Marshal` / `common.Unmarshal`。
- 不新增仅为缩短调用方而存在的单次机械 helper；只有普通消息与两类工具输出确实共享稳定的内容块处理语义时才抽取复用逻辑。
- 不修改视觉辅助计费、缓存、并发、重试、日志、前端配置或数据库。
- 不修改受保护的项目标识与组织信息。

## 检查门槛

- 两种真实 Codex 工具输出形态都触发辅助 caller。
- `strip_image` 两种配置都符合 PRD。
- 不含图片的请求字节内容保持不变。
- 现有普通 Responses、OpenAI Chat、Claude 视觉辅助测试全部通过。
- 全仓测试通过；若存在与本任务无关的既有失败，必须记录完整命令、失败包和与本次差异无关的证据。

## 回滚点

- 本任务无数据迁移和外部状态变更。
- 若工具输出改写产生协议回归，可整体撤销 `service/vision_assist.go` 的新增分支，恢复到只处理普通消息图片的行为。
