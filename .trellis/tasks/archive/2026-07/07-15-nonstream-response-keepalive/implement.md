# 非流式响应空白心跳实施计划

## 1. 后端配置与适用范围

- [ ] 在 `setting/operation_setting/general_setting.go` 增加默认关闭的 `non_stream_keepalive_enabled` 配置字段。
- [ ] 在 relay 公共层实现显式 JSON 允许列表判断，表驱动测试覆盖文本、Responses、Claude、Gemini、Embeddings、Rerank、图片生成/编辑，以及音频、Realtime、SSE、未知 mode 等拒绝路径。
- [ ] 验证历史配置缺字段时保持 false，无需数据库迁移。

## 2. 请求级响应包装器

- [ ] 在 relay/helper 或相邻公共边界实现非流式 keepalive writer，完整代理 `gin.ResponseWriter` 合同并提供 `Unwrap`。
- [ ] 将 tick 循环与单次心跳写入拆成可控、可直接测试的复杂逻辑入口，测试使用可控 channel，不依赖随机输入或 sleep。
- [ ] 实现 JSON 空白写入、Flush、写 deadline、请求取消、幂等停止和退出等待。
- [ ] 通过真实 `httptest.Server`/HTTP client 行为测试验证：客户端收到心跳后响应体读取仍未 EOF，只有最终 JSON 写入并结束 handler 后才完成。
- [ ] 用同一 mutex 串行化心跳与普通 `WriteHeader`/`Write`/`WriteString`/`Flush`，验证最终 JSON 中间不会插入空白。

## 3. Relay 生命周期与错误语义

- [ ] 在 `controller.Relay` 生成 `RelayInfo` 后安装请求级包装器，使其跨越完整渠道重试循环，并在 controller 退出前停止、等待清理。
- [ ] 保持心跳后的现有渠道重试；增加“首个渠道失败、后续渠道成功”和“所有渠道失败”的行为测试。
- [ ] 调整 `service.IOCopyBytesGracefully`：writer 未提交时行为不变；已由心跳提交时跳过固定 `Content-Length`、迟到 header 和重复状态码写入。
- [ ] 验证 OpenAI、Claude、Gemini 错误体在前导空白后仍是单个合法 JSON 值，且原始错误信息进入日志。
- [ ] 验证 New API request ID 始终下发，上游 request ID 始终进入 context/日志。

## 4. 图片与跨协议回归

- [ ] 为 `/v1/images/generations` 和 `/v1/images/edits` 非流式 JSON 增加核心回归测试，覆盖 URL 与 `b64_json` 语义不变。
- [ ] 验证上游在首次 tick 前完成时响应原始字节、状态码、header、`Content-Length` 与当前实现一致。
- [ ] 验证流式图片继续使用 SSE ping，不启动非流式空白心跳。
- [ ] 验证音频和其他二进制响应不会安装包装器、不会出现前导空白。

## 5. web/default 设置项

- [ ] 按现有 React Hook Form + Zod 模式补齐 schema、类型、默认值、section 注入、flatten/save 字段。
- [ ] 在现有心跳区域加入独立 Switch；共享间隔在任一开关启用时可编辑。
- [ ] 使用 `Alert` 展示状态码与 provider header 风险，保持现有设置页组合和移动端布局，不新增依赖。
- [ ] 遵循 React 性能规范，直接派生共享间隔启用状态，不增加冗余 state 或简单 `useMemo`。

## 6. i18n

- [ ] 按 `i18n-translate` 流程先运行同步报告，确认新增英文键来自实际 `t(...)` 调用。
- [ ] 只通过临时 `add-missing-keys.mjs` 写入 en、zh、fr、ja、ru、vi、zh-TW 文案，禁止直接编辑 locale JSON。
- [ ] 运行缺失键检查和 `bun run i18n:sync`，确认七种运行时资源均完整，然后删除临时脚本。

## 7. 验证命令

- [ ] `gofmt` 格式化所有修改的 Go 文件。
- [ ] 先运行相关包测试：`go test ./setting/operation_setting ./relay/common ./relay/helper ./service ./controller ./relay/channel/openai`。
- [ ] 对并发核心运行 race 检查：`go test -race ./relay/helper ./controller ./service`。
- [ ] 运行后端全量测试：`go test ./...`。
- [ ] 在 `web/default` 运行 `bun run i18n:sync`、`bun run typecheck`、`bun run lint`、`bun run format:check`、`bun run build`。
- [ ] 检查实际 diff，确认没有数据库迁移、受保护项目信息变更、classic 前端误改或无关格式化。

## 8. 审查门槛与回滚点

- [ ] 实现审查必须重点检查 Gin writer 包装合同、锁顺序、goroutine 退出、WriteHeader 后状态、HTTP/2 Flush 和代理缓冲假设。
- [ ] 若无法证明某 relay mode 在发起上游请求前必定返回 JSON，从允许列表移除，不使用运行时猜测兜底。
- [ ] 若心跳后发现会产生两个 JSON 顶层值、错误 JSON 被截断或最终业务 body 交错，停止交付并回滚 writer 集成。
- [ ] 运行时紧急回滚优先关闭 `general_setting.non_stream_keepalive_enabled`；无数据回滚步骤。
