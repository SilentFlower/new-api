# Responses 历史推理计数响应头契约

## 适用范围

渠道高级设置中的 `responses_reasoning_included_enabled` 声明：该渠道的原生 Responses 上游用量已经包含历史推理。默认关闭，只应对已验证计数口径的渠道开启。

## 转发行为

- 仅覆盖普通 Responses HTTP／SSE；不覆盖 Compact V1/V2、WebSocket、Chat Completions、Gemini 等其他协议或转换后的 Responses。
- OpenAI／Codex 适配器和 Advanced Custom 的原生 `converter=none` 路由可以生效；未知或转换路由不补充。
- 上游成功响应完全缺少 `X-Reasoning-Included` 时，补充 `true`。上游已有 `true`、`false`、空值或未知值均保留，不覆盖、不追加第二个值。
- 不修改请求或响应正文、加密推理、usage 和计费。错误响应不补充；响应头已经提交时不尝试修改。
- 开关存于已有渠道 `setting` JSON，无数据库迁移；旧渠道缺少字段时等价于关闭。

## 响应提交与保活

- `ResponsesHelper` 初始化渠道后调用 `PrepareResponsesReasoningHeader`，通过已有非流式 writer 的 `Header()` 停止后续空白保活，保留响应头决策机会。
- 符合条件的请求等待上游 HTTP 响应头时不启动 SSE TTFB ping，避免上游标识到达之前提交下游响应头。取得响应头后，流正文扫描继续按既有配置发送 SSE comment 保活。
- 未开启或不支持的请求保留原有保活行为。开关开启的请求在上游响应头等待阶段不发送保活，长时间等待可能受到代理空闲超时限制。
- 非流式保活若在进入 Responses handler 前已经提交了响应头，本次不补充标识；不能把修改已提交 Header map 误认为客户端收到新值。
- WebSocket 在读取首帧及选定渠道前完成客户端握手，不能按最终渠道补充握手响应头，本开关不承诺 WebSocket 生效。

## 客户端限制

Codex 0.162.1 会在新的用户回合开始时将服务端推理已计数标志重置为 false，再执行请求前的压缩检查。网关响应头只能影响客户端收到响应后的计数，不能保证新回合按实际 450K 才压缩，也不会扩大服务端上下文窗口。

## 验证与回滚

- 后端回归覆盖默认关闭、缺失时补充、上游 false/true/未知值优先、错误与已提交响应、协议转换和 Compact 排除。
- JSON／SSE 转发测试检查客户端收到的头、原始响应正文及 usage 一致。
- 保活测试覆盖非流式响应头仍可决定，以及未开启渠道的既有 SSE TTFB ping。
- 前端测试覆盖默认值、创建／编辑／关闭、未知配置保留、开关点击与可访问标签。
- 检查 root Go 构建、`relaykit` 独立构建、Go vet、相关测试、前端类型／lint／构建及七语言完整性。
- 回滚时关闭渠道开关即可恢复原行为；代码回滚删除独立 helper 和组件并撤销薄接入点。
