# 视觉辅助选择与消息审计现状调研

## 调研范围

- 渠道视觉辅助表单与可复用选择控件。
- 启用渠道及其模型列表的精简查询。
- 主请求消息审计捕获时机。
- 视觉辅助缓存、并发、重试和上游调用链路。
- 消息审计会话归并、详情展示和关联字段现状。

## 代码事实

### 渠道与模型配置

- `web/src/features/channels/components/drawers/sections/build-channel-settings.tsx:409` 使用数字 `Input` 保存 `vision_assist_channel_id`，使用文本 `Input` 保存 `vision_assist_model`。
- `web/src/components/ui/combobox.tsx` 的兼容封装支持搜索选项和 `allowCustomValue`，适合渠道搜索和模型自定义值。
- `.trellis/spec/frontend/base-ui-composition.md:24` 要求受控选择器始终包含当前值对应选项；历史失效渠道和模型必须通过合成选项保留。
- `model/message_audit_review_options.go:15` 已证明可以只查询启用渠道的 `id`、`name`、`models` 并通过 `Channel.GetModels()` 返回模型数组，不涉及密钥。

### 主请求消息审计

- `controller/relay.go:138` 在主请求 DTO 校验和 `RelayInfo` 创建后调用 `service.CaptureMessageAudit`。
- `controller/relay.go:151` 仅当 capture 入队成功时注册 finalize。
- `service/message_audit.go:306` 负责协议允许列表、正文规范化、媒体摘要、加密前事件构造和非阻塞入队。
- `service/message_audit.go:329` 当前只按协议判断是否跳过会话指纹，尚无调用级 standalone 标记。
- `model/message_audit.go:29` 已有 `ParentRequestID`，但该字段服务于同一推断会话的请求链，不能复用为视觉辅助关联字段。

### 视觉辅助调用

- `service/vision_assist.go:144` 先检查请求内复用与全局缓存，只有缺失识图单元进入调用。
- `service/vision_assist.go:309` 每次重试都会重新构造辅助请求、复制 Gin 上下文并调用 `VisionAssistCaller`。
- `relay/vision_assist.go:91` 的 `callVisionAssistModel` 直接完成辅助渠道切换、协议转换、token 估算、计费和适配器调用，不会再次经过 `controller.Relay`。
- `relay/vision_assist.go:110` 完成 `prepareVisionAssistRequest` 后才得到最终端点协议 DTO；这里是审计正文最早具备准确协议语义的位置。
- `relay/vision_assist.go:242` 为每个辅助尝试生成带 `vision_assist` 后缀的独立请求 ID，可直接作为消息审计记录主键。

## 结论

- 用户观察正确：当前视觉辅助请求不进入消息审计，原因是内部调用绕过唯一的 Controller 捕获点。
- 渠道和模型选择可使用精简的启用渠道模型选项接口，不需要返回完整渠道或敏感字段。
- 视觉辅助审计应在 `prepareVisionAssistRequest` 成功后开始，这时 DTO 已完成端点转换和模型映射，同时后续 token、计费及上游失败仍可被 finalize。
- 视觉辅助记录需要新的请求类型和关联请求字段；现有 `ParentRequestID` 不能复用。
- standalone 应是 capture 级语义，不应伪造新的 RelayFormat；跳过会话指纹即可让 Model 为每条辅助记录创建独立审计会话。
- 每次内部重试都是真实独立尝试，应各自产生记录；缓存命中不会进入 caller，因此自然不产生记录。
