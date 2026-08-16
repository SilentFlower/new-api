# 优化视觉辅助模型选择与消息审计实施计划

## 实施步骤

- [x] 1. 新增启用渠道模型选项契约
  - 在新文件 `model/channel_model_option.go` 定义精简 DTO 和 GORM 查询，只选择启用渠道的 `id`、`name`、`models`。
  - 让现有消息审计重审选项查询复用该能力，保持既有 JSON 返回不变。
  - 在新控制器文件提供 `GET /api/channel/model-options`，使用 `common.ApiSuccess/ApiError`。
  - 在 `router/channel-router.go` 以 `ChannelRead` 权限注册单一薄路由。
  - 增加 Model/Controller 契约测试，验证只返回启用渠道、模型解析正确且不包含敏感字段。

- [x] 2. 实现视觉辅助渠道与模型联动选择器
  - 在渠道 feature 的 `api.ts` 和 `types.ts` 增加精简选项类型和查询函数。
  - 新建 `vision-assist-model-fields.tsx`，使用 React Query 加载选项并绑定现有 React Hook Form 字段。
  - 渠道 Combobox 支持名称/ID 搜索，不允许自定义 ID；模型 Combobox 随渠道联动并允许自定义值。
  - 为历史失效渠道和模型合成当前选项，保证受控值始终存在；切换渠道时清空旧模型。
  - 在加载、失败、无渠道、无模型和历史值失效状态下保持表单值与 Drawer 可用。
  - 在 `build-channel-settings.tsx` 仅保留新组件调用，移除原两个手填输入块。
  - 使用 `i18n-translate` 工作流一次性写入七语言文案，不直接编辑 locale JSON。
  - 增加 RTL/Vitest 交互测试，覆盖搜索选择、渠道切换清模、历史值保留、自定义模型、禁用和错误状态。

- [x] 3. 扩展消息审计元数据与 standalone 捕获契约
  - 在 `model.MessageAuditRequest` 增加 `request_kind` 和 `related_request_id` 索引字段，不设置数据库默认值和外键。
  - 更新列表、详情及会话查询的显式选择列，历史空类型按普通客户端请求返回。
  - 扩展 `MessageAuditCaptureInput`，支持请求类型、关联请求 ID 和非持久化 standalone 标记。
  - standalone capture 跳过会话指纹，使每条视觉辅助记录获得独立审计会话，不改变 `ParentRequestID` 语义。
  - 详情响应增加关联视觉辅助记录元数据查询，避免返回密文或正文。
  - 增加 Service/Model 回归，覆盖字段持久化、独立会话、不归并主会话、历史空类型、关联查询和 finalize。

- [x] 4. 接入视觉辅助独立审计生命周期
  - 新建 `relay/vision_assist_audit.go`，集中构造 capture 输入并映射成功/失败 finalize。
  - 在 `prepareVisionAssistRequest` 成功后、token 估算前启动审计，正文使用 `prepared.req`。
  - 在 `callVisionAssistModel` 使用统一 defer，确保 token、定价、并发、预扣费、上游和响应错误都能 finalize。
  - 每次内部重试沿用独立辅助请求 ID 并关联同一主请求；缓存命中保持不进入 caller。
  - 增加四种端点 DTO、成功、失败、capture 不可用、多次重试和缓存命中边界测试。

- [x] 5. 在消息审计界面展示独立记录与关联
  - 扩展前端消息审计类型，历史缺失 `request_kind` 时按普通请求处理。
  - 列表为视觉辅助记录显示紧凑 Badge，不新增低价值独立宽列。
  - 详情显示请求类型；视觉辅助记录提供主请求跳转。
  - 主请求详情展示关联视觉辅助记录列表，允许打开每条独立记录。
  - 关联目标缺失时保留 ID，并沿用现有加载错误处理。
  - 增加 UI 逻辑和交互测试，覆盖普通历史记录、视觉辅助标识、双向跳转和空关联。

- [x] 6. 实现 Combined 模式有界分批
  - 在 `relaykit/dto.ChannelVisionAssistSettings` 增加 `combined_max_images`，后端归一化默认值 `5` 和范围 `1-64`。
  - 调整视觉辅助单元构造：按用户消息、图片原始顺序、单批图片数和固定 `8 MiB` 请求体安全上限稳定分批。
  - 保持 `separate` 模式、全局图片索引、缓存、重试、并发、计费和失败策略语义不变。
  - 保持现有 `strip_image` 契约：关闭移除时原始媒体块内容和顺序不变，开启移除时仍只保留识图文字；不实现持久图片句柄。
  - 保持分批和缓存键确定性，不引入请求 ID、会话 ID、消息索引、批次序号或 worker 顺序；相同有序批次跨新请求继续命中现有 HybridCache。
  - 在普通日志中记录批次数、各批图片数、是否切割、切割原因和生效图片上限。
  - 扩展渠道表单 schema、默认值、旧配置解析和保存逻辑，仅在 `combined` 模式显示范围 `1-64` 的数字输入，默认 `5`。
  - 使用 `i18n-translate` 工作流同步七语言文案。
  - 增加后端表驱动测试和前端表单/UI 测试，覆盖 `39 -> 5+5+5+5+5+5+5+4`、跨消息隔离、字节上限提前切割、单张超限、旧配置默认值和 separate 不回归。
  - 增加跨独立请求缓存回归：首次分批写缓存，第二个请求 ID/会话/消息位置不同但输入等价时 caller 零调用；批次组合变化不误命中，组合不变仍可复用。
  - 执行 `cd relaykit && GOWORK=off go build ./...`，验证独立模块仍可构建。

- [x] 7. 完成规范、兼容性和全量验证
  - 核对新增字段由 GORM AutoMigrate 在 SQLite、MySQL、PostgreSQL 上安全添加，不引入数据库专用 SQL。
  - 核对消息审计密文、媒体摘要、保留清理、整库清空和 AI 重审边界未变化。
  - 核对视觉辅助缓存、重试、计费、并发和失败策略未变化。
  - 更新视觉辅助与消息审计可执行规范，记录独立审计和关联契约。
  - 执行定向测试、全仓测试、前端门禁和 diff 检查。

## 验证命令

```bash
go test ./model ./service ./relay ./controller ./router
go test ./...
go vet ./...

cd relaykit
GOWORK=off go build ./...

cd web
bun test src/features/channels src/features/message-audits
bun run typecheck
bun run lint
bun run format:check
bun run i18n:sync
bun run build

cd ..
git diff --check
```

i18n 还必须按 `i18n-translate` 工作流执行源码缺键扫描、七语言键集合和未翻译值检查；不能只以 `i18n:sync` 作为通过依据。

## 重点检查

- [x] 选项 API 不返回渠道密钥和敏感配置。
- [x] 受控 Combobox 始终包含历史当前值。
- [x] 切换渠道只清空辅助模型，不影响其他视觉辅助字段。
- [x] 最终协议 DTO 才进入视觉辅助审计正文。
- [x] 每个真实重试独立记录，缓存命中无记录。
- [x] 所有 capture 后返回路径都 finalize。
- [x] 视觉辅助记录不参与主请求会话归并或主会话 AI 重审。
- [x] 普通历史审计和历史渠道配置无回归。
- [x] 原有上游文件只有必要的字段、路由、调用和渲染接入。

## 回滚点

- 完成步骤 2 后可独立回滚选择器与选项 API，不影响现有保存格式。
- 完成步骤 4 后可撤销视觉辅助审计调用和 UI 展示，保留数据库新增列不会影响旧代码。
- 如全量验证发现消息审计会话或保留清理回归，先撤销 standalone capture 接入，不调整现有会话算法。
