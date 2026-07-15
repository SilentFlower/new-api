# 实施计划

## 阶段 1：Alpha Search 入口与校验

1. 增加 Alpha Search RelayFormat、RelayMode、路径识别和 `/v1/alpha/search` 路由。
2. 定义最小 Alpha Search DTO，实现 Relay 生命周期所需接口。
3. 校验 JSON、必填 `model` 和 `max_output_tokens` 上限；其它协议字段保持透传。
4. 测试路由模式、模型必填、显式零值和超大 max token。

## 阶段 2：Alpha Search 上游转发

1. 从 `BodyStorage` 获取原始 body，写入最终模型映射并应用 Param Override。
2. 构造 Codex、Advanced Custom 和普通渠道上游 URL，合并 query。
3. 复用 adaptor 请求头、Header Override、渠道代理客户端和安全响应头复制。
4. 非 `2xx` 在提交响应前返回 Relay 错误；`2xx` 复制原始 JSON 响应。
5. 测试模型映射、未知字段、显式零值、query、请求头替换、响应头和失败重试。

## 阶段 3：Alpha Search 纯工具计费

1. 扩展 `ComputeToolCallQuota` 使用 Checked quota 并返回饱和标记。
2. 增加纯工具预扣、结算和消费日志方法，复用 BillingSession。
3. 请求上游前按 1 次 `web_search` 预扣；最终成功结算，失败退款。
4. 测试分组倍率、成功收费、非 `2xx`/网络失败不收费、日志字段和饱和保护。

## 阶段 4：Compact 官方字段补全

1. 在 `ResponsesHelper` 的 Compact 请求构造中复制 `tools`、`reasoning`、`text`。
2. 增加表驱动测试，验证三个字段及现有字段均进入转换后的上游请求。
3. 回归验证 Compact API 类型限制、URL、usage 和计费未改变。

## 阶段 5：质量验证

1. 运行定向测试：
   ```bash
   go test ./router ./middleware ./controller ./relay/... ./service ./dto
   ```
2. 运行完整检查：
   ```bash
   go test ./...
   go vet ./...
   git diff --check
   ```
3. 核对 Alpha Search 响应提交与重试、工具预扣/退款，以及 Compact 最小 diff。
4. 提交标题携带 `[build]` 触发构建。

## 风险与回滚点

- `controller/relay.go`：Alpha Search 预扣、重试和失败退款不能重复执行。
- Alpha Search 成功响应写入前必须完成所有可失败处理，避免已提交后重试。
- `relay/responses_handler.go`：Compact 只增加三个字段赋值，禁止顺带改变 API 类型、URL、响应和计费。
- `service/tool_billing.go`：新计费路径必须使用 Checked quota，不能改变现有工具价格。
