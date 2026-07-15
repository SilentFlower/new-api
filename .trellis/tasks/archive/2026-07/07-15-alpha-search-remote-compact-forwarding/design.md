# Alpha Search 与远程压缩字段补全设计

## 1. 变更边界

两项改动保持独立：

```text
Alpha Search：新增完整 Relay 入口、透明请求转发、错误重试和纯工具计费
Responses Compact：在现有请求转换中补复制 tools / reasoning / text
```

Compact 不复用 Alpha Search 的原始 JSON 透传，不扩大渠道范围，也不改变既有响应和计费。

## 2. Alpha Search 请求与 Relay 模式

- 新增独立 Alpha Search RelayFormat 与 RelayMode，避免伪装成普通 Responses。
- 最小 DTO 只声明 `model`、`id`、`max_output_tokens` 等入口字段并实现 `dto.Request`；实际上游 body 始终来自 `BodyStorage`。
- `max_output_tokens` 使用指针类型并复用 `relay/helper/valid_request.go` 的统一上限，保留缺失与显式 `0` 的差异。
- `PrepareRequestForSelectedChannel` 每次尝试初始化渠道元信息并执行链式模型映射；使用 `sjson` 将最终 `UpstreamModelName` 写入原始 body，再应用现有 Param Override。

## 3. Alpha Search 上游转发

| 所选渠道 | 上游路径 |
|---|---|
| Codex | `<base>/backend-api/codex/alpha/search` |
| Advanced Custom | 匹配 route 的 `upstream_path` |
| 其它渠道 | 沿用渠道 Base URL 约定的 `/v1/alpha/search` |

- 入站 query 逐值合并到上游 URL；Advanced Custom 自带 query 鉴权不得被覆盖。
- 先调用 adaptor `SetupRequestHeader` 注入渠道认证，再应用 Header Override。
- 客户端 Authorization、Cookie、Host、Content-Length 和 hop-by-hop header 不做通配透传。
- Content-Type 使用 JSON，Accept 默认为 `application/json`。
- 非 `2xx` 在写客户端前转成 Relay 错误，以便状态码映射、自动禁用和重试；最终错误保持统一格式。
- `2xx` 响应不绑定固定 DTO，复制原始 body、状态码和安全响应头。

## 4. Alpha Search 纯工具计费

1. 通过 `helper.HandleGroupRatio` 获取当前尝试的分组倍率。
2. `ComputeToolCallQuota` 按 `web_search` 调用 1 次计算费用，并使用 Checked quota 换算记录饱和标记。
3. 上游调用前通过 `PreConsumeBilling` 或现有 BillingSession `Reserve` 预扣。
4. 所有尝试失败时由主 Relay 生命周期退款。
5. 最终 `2xx` 使用固定额度 `SettleBilling`，更新用户/渠道用量和请求次数，记录零 token 消费日志。
6. 日志 `other` 包含 WebSearch 次数、单价、分组倍率、模型映射、计费来源和请求路径，不包含查询或响应内容。

不修改 `PostTextConsumeQuota` 的历史语义，避免普通上游缺失 usage 时被误收费。

## 5. Responses Compact 最小修复

现有 `OpenAIResponsesCompactionRequest` 已解析以下三个官方字段：

- `Tools json.RawMessage`
- `Reasoning *Reasoning`
- `Text json.RawMessage`

只修改 `ResponsesHelper` 的 Compact 请求对象构造，将它们复制到 `OpenAIResponsesRequest`：

```text
req.Tools     -> responsesReq.Tools
req.Reasoning -> responsesReq.Reasoning
req.Text      -> responsesReq.Text
```

其余分支保持原样，包括 API 类型检查、模型映射、 disabled fields、Param Override、adaptor URL、Compact handler 和 `PostTextConsumeQuota`。

## 6. 兼容性与回滚

- 无数据库迁移、前端改动或配置变更。
- Alpha Search 可独立回滚新增 Relay 格式、路由、转发和工具计费。
- Compact 可独立回滚三个字段赋值；旧路由和计费不受影响。
