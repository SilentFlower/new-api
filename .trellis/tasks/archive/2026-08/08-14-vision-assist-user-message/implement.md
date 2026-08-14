# 视觉辅助引入用户原始问题实现计划

## Step 1：解析当前用户意图

- 在 `service/vision_assist.go` 内增加视觉辅助领域内的用户文本解析逻辑。
- 覆盖 OpenAI Chat、Claude Messages 和 OpenAI Responses。
- 仅提取最新非空用户文本，忽略 system、assistant、tool 和 Responses 工具输出内容。
- 复用现有 DTO 内容解析方法和 `common.Unmarshal`，不修改 `relaykit/dto`。

## Step 2：构造意图驱动的辅助请求

- 更新内置默认视觉辅助提示词，使其优先围绕用户问题提取相关信息，并保留人物身份判断的不确定性。
- 将解析到的用户文本以明确分隔的独立文本块加入现有单条 `role = "user"` 多模态消息。
- 用户文本为空时不生成该区块，保持现有退化行为。
- 保持 `Message.SetMediaContent()`、图片编号、图片 detail/MIME 和各辅助端点转换契约不变。
- 增加 `separate` / `combined` 模式归一化；逐张模式每个单元一张图片，合并模式按原始消息分组。
- 合并模式在同一辅助请求中按顺序发送当前消息的全部图片，并增加联合分析说明。

## Step 3：隔离问题相关缓存

- 将规范化后的用户文本哈希、多图模式和当前识图单元的有序图片列表加入 `buildVisionAssistCacheKey` 输入。
- 同步线程化执行、重试和请求级去重调用中的用户文本参数。
- 保持缓存值结构、TTL、Redis/内存实现和日志字段不变。

## Step 4：增加渠道级模式配置

- 在 `relaykit/dto.ChannelVisionAssistSettings` 增加 `MultiImageMode`，JSON 字段为 `multi_image_mode`，空值保持兼容。
- 前端表单增加 `vision_assist_multi_image_mode`，新建渠道默认 `combined`；历史渠道缺失或配置非法时继续解析为 `separate`，并支持保存回渠道 JSON。
- 使用双选模式控件展示“逐张识别”和“合并识别”，并通过 i18n 脚本补齐七语言文案。
- 更新渠道表单类型、字段错误归类和 round-trip 测试。

## Step 5：调整写回语义

- 将注入标题调整为“图片相关信息”。
- 将说明调整为“与当前用户问题相关”，并要求目标模型保留不确定性。
- 保持原始用户文本、非图片内容顺序和 `strip_image` 行为不变。
- 合并模式将综合结果只写回一次，并按实际图片数统计日志。

## Step 6：测试

- 新建 `service/vision_assist_user_message_test.go`，使用 `testify/require` 和 `testify/assert` 覆盖：
  - OpenAI Chat 字符串和多模态文本解析。
  - Claude 字符串和多模态文本解析。
  - Responses 普通用户消息解析，以及工具输出文本排除。
  - 最新图片消息无文本时回溯上一条用户问题。
  - 完全无用户文本时保持原辅助请求形态。
  - 多图配置缺失或为 `separate` 时继续逐张调用。
  - `combined` 模式下同一消息多图只调用一次、不同消息不合并、综合结果只写回一次。
  - 多图模式和有序图片组参与缓存隔离。
  - 新建渠道表单默认使用 `combined`，历史渠道缺省或非法配置仍使用 `separate`。
  - 辅助请求仍只有一条 user 消息，内容顺序正确。
  - 同图同问题缓存命中、同图不同问题缓存隔离。
  - 写回保留原始问题并使用新的中性说明。
- 对 `service/vision_assist_test.go` 只做现有请求构造契约所需的最小适配。
- 更新 Relay 调用测试和渠道表单 round-trip 测试。
- 运行既有视觉辅助与端点转换回归。

## Validation

```bash
gofmt -w service/vision_assist.go service/vision_assist_test.go service/vision_assist_user_message_test.go
go test ./service -run 'VisionAssist' -count=1
go test ./relay -run 'VisionAssist' -count=1
go test ./service ./relay -count=1
cd relaykit && GOWORK=off go build ./...
cd ../web && bun run typecheck
cd ../web && bun run build
go test ./... -count=1
git diff --check
```

前端 i18n 必须通过脚本写入七个 locale，并执行源码缺键扫描和 `bun run i18n:sync`。

## Risk And Rollback Points

- Responses `Input` 是原始 JSON，解析必须保留现有结构并避免把工具输出当成用户问题。
- 用户问题遗漏在缓存键中会造成跨问题错误复用，缓存测试是阻断性验收项。
- 默认提示词与写回文案变化可能改变已有模型输出，测试应断言语义契约，不锁定整段生成文本。
- 回滚多图模式时同时移除 DTO 字段、前端表单字段和七语言文案；用户意图相关逻辑保持不变。

## Upstream Sync Review

- 新增逻辑继续收敛在现有 build 定制文件 `service/vision_assist.go`。
- 不重构或移动上游请求解析、协议转换和主 Relay 流程。
- 上游同步后复核 Chat/Claude/Responses 内容类型定义即可，不新增分散接入点。
