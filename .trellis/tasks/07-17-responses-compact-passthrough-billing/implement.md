# 实施计划 — Responses Compact 透传与基础模型计费

## 1. 开发前确认

- [ ] 重述并确认 `brief.md` 后运行 `task.py start`；planning 状态不得修改业务代码。
- [ ] 进入实现前执行 `trellis-route(implement)`，按路由结果派发实现。
- [ ] 读取 `implement.jsonl` 中的 build 定制指南、Compact 契约和任务研究。
- [ ] 记录当前 `git status --short`，只处理本任务文件，不覆盖用户已有改动。

## 2. 后端实施顺序

### 2.1 渠道设置字段

- [ ] 在 `dto/channel_settings.go` 的 `ChannelSettings` 增加 `ResponsesCompactPassthroughEnabled bool`，JSON 键为 `responses_compact_passthrough_enabled`。
- [ ] 增加 DTO/Model JSON round-trip 测试，验证旧设置默认关闭、新字段可保存且无数据库迁移。

### 2.2 分发层基础模型

- [ ] 在 `middleware/distributor.go` 保留 Compact mode 检测和上下文写入，仅删除 `WithCompactModelSuffix` 改写。
- [ ] 更新或新增独立测试文件，断言 V1 path、历史 bridge 和 V2 HTTP 的 Token 权限、`original_model`、亲和性 key 与渠道选择模型均为基础模型。
- [ ] 保留普通 Responses 和非 Compact 请求既有断言。

### 2.3 新建 HTTP Compact 透传模块

- [ ] 新建 `relay/responses_compact_passthrough.go`，定义带中文 Doc 注释的公开分派、准备和处理函数。
- [ ] 实现 `ShouldHandle` 判断，仅匹配现有 Compact mode。
- [ ] 在准备阶段调用 `InitChannelMeta`，检查渠道开关；关闭时返回专用 `503`、`skipRetry`、`noRecordErrorLog` 错误，不调用模型映射。
- [ ] 将基础模型写入 `OriginModelName`、`UpstreamModelName` 与请求 DTO，清理映射和旧计费快照状态。
- [ ] 使用 `common.BodyStorage` 获取原始 body，不做 DTO marshal、Param Override 或 disabled fields 处理。
- [ ] 构造局部出站 `RelayInfo` 视图：V1 使用 Compact 路径，V2 与历史 bridge 使用 `/responses`；复用 adaptor `DoRequest`、认证与安全 header allowlist。
- [ ] 非 2xx 在响应提交前走现有 `RelayErrorHandler` 和状态码映射。
- [ ] JSON 响应原样写回并旁路解析合法 usage。
- [ ] SSE 响应按原始字节流式写回，旁路解析终态与 usage，不重组事件或 payload。
- [ ] 合法 usage 调用 `PostTextConsumeQuota`；缺失/非法 usage、失败、取消、断连和不完整流退款并写安全审计。
- [ ] 新建 `relay/responses_compact_passthrough_test.go` 覆盖路径矩阵、原始 body、未知字段、header 安全、JSON/SSE 原样响应、合法结算、异常退款和普通 Responses 隔离。

### 2.4 HTTP 主链路薄接入

- [ ] 在 `controller/relay.go` 仅增加三个薄接入点：Compact 跳过旧本地 SSE bridge、选定渠道后调用新准备函数、Responses handler 分派到新模块。
- [ ] 能力门禁错误必须在 `prepareMainRelayBilling` 前结束；不得调用 `processChannelError` 或继续 retry。
- [ ] 真实上游错误继续走现有错误处理、亲和性和 retry；每次重试选定新渠道后重新执行开关门禁。
- [ ] 增加控制器回归测试，证明初选门禁关闭时渠道调用次数为零、预扣为零、未换渠、未自动禁用；真实上游可重试错误仍使用基础模型。

### 2.5 新建 WebSocket Compact turn 模块

- [ ] 新建 `controller/responses_compact_passthrough_websocket.go`，实现 Compact turn 的独立准备函数。
- [ ] 复用现有请求校验，但返回原始 frame 副本；不调用 `ModelMappedHelper`、adaptor request conversion、disabled fields 或 Param Override。
- [ ] 初始化/重置 `RelayInfo`，固定基础模型，检查当前已选渠道开关后再调用 `prepareMainRelayBilling`。
- [ ] 在 `controller/responses_websocket.go` 的 `prepareResponsesWebSocketTurnAttempt` 顶部增加一次 Compact 分派；普通 turn 原逻辑不动。
- [ ] 新建 `controller/responses_compact_passthrough_websocket_test.go`，覆盖首轮、后续 turn、普通/Compact 交替、开关关闭不重选、原始未知字段、基础模型计费、合法 usage 结算和缺失 usage 退款。

## 3. 前端实施顺序

### 3.1 Default

- [ ] 新建 `responses-compact-passthrough-field.tsx`，使用现有 `FormField`、`FormItem`、`FormLabel`、`FormDescription` 和 `Switch`，组件内部调用 `useTranslation()`。
- [ ] 在 `types.ts` 和 `channel-form.ts` 最薄接入字段类型、Zod schema、默认值、旧值解析和 JSON 序列化；保留未知 JSON 字段。
- [ ] 在 `channel-mutate-drawer.tsx` 只导入并挂载新组件，并将字段纳入高级设置是否展开的判断。
- [ ] 通过临时 `scripts/add-missing-keys.mjs` 写入六种语言文案，执行 `node scripts/add-missing-keys.mjs`、`bun run i18n:sync`，验证报告后删除临时脚本。
- [ ] 为 `transformChannelToFormDefaults`/提交转换补充字段 round-trip 测试；组件测试验证开关交互和可访问名称。

### 3.2 Classic

- [ ] 新建 `ResponsesCompactPassthroughSetting.jsx`，仅封装 `Form.Switch` 展示与 `onChange`。
- [ ] 在 `EditChannelModal.jsx` 的默认值、`channelSettings`、JSON 构造、旧值解析、状态同步、清理字段和组件挂载处加入同一字段，不改其他设置逻辑。
- [ ] 按 Classic 现有 i18n 方式补齐标签和说明文案，验证编辑旧渠道时字段不会丢失。

## 4. Spec 更新

- [ ] 实现和验证完成后使用 `trellis-update-spec` 修订 `.trellis/spec/backend/relay-alpha-search-compact.md` 中已过时的后缀模型、历史 bridge 和 Compact 计费契约。
- [ ] 保留 `.trellis/spec/guides/build-upstream-friendly-customization.md`，并在检查中逐文件核对薄接入原则。

## 5. 验证命令

### 5.1 定向后端

```bash
go test ./dto ./middleware ./relay ./relay/channel/openai ./relay/channel/codex ./controller ./service -count=1
go test -race ./relay -run 'ResponsesCompactPassthrough' -count=1
go test -race ./controller -run 'ResponsesCompactPassthrough|ResponsesWebSocket' -count=1
go vet ./dto ./middleware ./relay ./controller ./service
```

### 5.2 前端

```bash
cd web/default
bun run i18n:sync
bun run typecheck
bun run lint
bun run build
```

Classic 按其 `package.json` 现有脚本执行定向 lint/build，不新增包管理器或依赖。

### 5.3 全仓

```bash
go test ./... -count=1
go vet ./...
git diff --check
git diff --stat
```

### 5.4 真实联调

- [ ] 启动本地 sub2api，准备一个基础模型可用且渠道开关开启的 new-api 渠道。
- [ ] 在不配置任何 `*-openai-compact` 模型或价格的情况下执行 V1 path。
- [ ] 执行历史 body bridge，确认 sub2api 入站仍为 `/responses` 且客户端收到合法 SSE。
- [ ] 执行原生 V2 HTTP/SSE，确认路径、beta header、原始 payload 和 completed usage。
- [ ] 执行 V2 WebSocket，确认原始 frame、completed usage 和多 turn 行为。
- [ ] 关闭所选亲和渠道开关，确认立即返回专用错误且不换渠、不预扣、不自动禁用。
- [ ] 对照 new-api 与 sub2api 的请求 ID/安全审计字段，日志中不得出现正文、密文、完整 query 或凭证。

## 6. 风险与回滚点

| 风险 | 防护 | 回滚点 |
| --- | --- | --- |
| 历史 bridge 被错误改为 `/responses/compact` | 路径矩阵单测与真实 SSE 联调 | 删除出站视图 bridge 分支并撤销 controller 分派 |
| 能力失败进入 retry/auto-ban | 专用非 `channel:` ErrorCode + `skipRetry` + 调用次数断言 | 撤销门禁接入 |
| Compact 仍被模型映射或后缀计费 | 准备函数不调用 ModelMappedHelper；断言四个模型位置 | 恢复旧 handler 分派 |
| SSE observer 改写 payload | 原始字节 golden 测试，observer 只旁路解析 | 关闭 observer并退款，不影响透传 |
| 缺失 usage 被估算收费 | 仅完整 usage 结算，异常退款测试 | 回滚新结算分支 |
| 前端编辑丢未知设置 | 现有 JSON spread 合并 + round-trip 测试 | 删除字段接入，不影响后端默认关闭 |
| 上游同步冲突 | 新逻辑独立文件，热点文件仅单点分派 | 删除新文件并撤销薄接入 |

## 7. 完成标准

- [ ] PRD 全部验收项有测试、命令输出或真实联调证据。
- [ ] `git diff --stat` 显示业务逻辑主要位于新文件，原有文件改动保持最小。
- [ ] 每个原有文件改动均能用 `design.md` 中的一句话解释必要性。
- [ ] 最终执行 `trellis-check-all` 或当前 route 指定的完整检查，修复后重新检查。
- [ ] 更新 task progress、release 说明和相关 spec 后，才进入提交与归档流程。
