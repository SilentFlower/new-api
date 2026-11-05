# 排查依据与实现入口

## 来源与覆盖范围

- 本地仓库 `/root/project/new-api`，分支 `build-bak`，排查时 HEAD 为 `9379c8080`
- 线上镜像 revision 为 `2ca8fd15ed38c7087c8634dd8e4c81c1d056faae`，服务版本 `build-2ca8fd1`
- 已用本地 Git 中的部署提交核对首帧入口与环境变量实现，相关代码与当前工作区一致
- 建档阶段仅进行源码、日志排查，未修改产品代码；本次开工后的实现和验证见 [验证记录](verification.md)，未部署或重启生产服务
- 不包含服务器密码、API Key、数据库连接串、请求正文或对话内容

## 已核实的请求

- 请求 ID：`202611050404003440720208268d9d6kmTLznzp`
- 路径：`GET /v1/responses`
- 服务器访问日志结束于 `2026-11-05T04:05:00.352128896Z`，持续时间 `1m0.007444323s`
- 根据请求 ID 时间及持续时间，约从 UTC `04:04:00` 等待至 `04:05:00`，北京时间为 `2026-11-05 12:04:00 → 12:05:00`
- 容器未设置专用首帧环境变量，使用 60 秒缺省；`STREAMING_TIMEOUT=600` 不控制这一阶段
- 精确请求 ID 在应用输出和文件日志中对应同一条访问结束记录；该读取失败分支没有记录底层 `err`

这些证据与首帧期限到达吻合，不能进一步证明客户端完全没有发送任何字节，也不能区分客户端延迟发送与链路未送达

## 首帧调用链

1. `controller/responses_websocket.go:66` 进入 handler，`:74` 完成 upgrade
2. `:80` 设置大小上限，`:81` 读取配置并设置 deadline
3. `:83` 调用 `ReadMessage`，返回后清除 deadline
4. `:85` 将任意读取失败构造为 `400 invalid_request + SkipRetry`，写错后直接返回
5. 读取成功后才进入 `:90` 解析、`:95` 选渠与 `:103` 建连及上游请求

`writeResponsesWebSocketError` 位于 `controller/responses_websocket.go:758`，附请求 ID；5xx 使用 internal error 关闭码，其余统一使用 policy violation，错误封套不发送内部 skipRetry 字段

`***.create` 来自 `relaykit/relayconvert/kitutil/mask.go` 将 `response.create` 按域名脱敏；本任务不扩展为通用脱敏重构

## 底层读取约束

当前依赖为 `github.com/gorilla/websocket v1.5.0`

- 对应模块 `conn.go:991` 附近的 `NextReader` 合同说明读取错误具有永久性，后续读取返回同一错误
- `conn.go:1101` 附近的 `SetReadDeadline` 合同说明读取超时后连接状态不可恢复
- 因而不能在原连接上清空期限并循环读取；应在出口表达可恢复失败，由调用方重新建连

这是对应模块源码依据，不是对具体客户端自动重试实现的推断

## 配置与前序工作

- `constant/responses_websocket_env.go`：缺省 60 秒与启动期全局整数
- `common/responses_websocket_env.go`：启动解析，非法、非正数与溢出回退 60 秒
- `common/responses_websocket_env_test.go`：环境变量及数值边界回归
- `controller/responses_websocket_timeout_test.go`：真实首帧超时和及时首帧回归，当前断言 400
- 已完成前序任务 `.trellis/tasks/11-04-responses-timeout-stream-status/`，不要重写其完成记录

## 动态配置复用入口

- `model/option.go` 的 `InitOptionMap` 导出注册配置并加载 Options；`SyncOptions` 周期重载数据库值
- 同文件的 `UpdateOption`、`updateOptionMap`、`handleConfigUpdate` 为持久化、运行时派发和注册型配置更新入口
- `controller/option.go` 的 `GetOptions`、`UpdateOption` 为现有管理端读写入口
- `setting/operation_setting/general_setting.go` 提供现有通用保活设置结构与注册示例
- 实施前核对解析错误、数据库保存错误和反射更新的锁边界，避免无效值先落库或保存失败却更新内存

## 前端复用入口

- `web/src/features/system-settings/models/global-settings-card.tsx`：全局设置、已有保活字段、schema 和提交差异计算
- `models/index.tsx`：模型设置初始值；`models/section-registry.tsx`：服务端值组装为表单 defaultValues
- `web/src/features/system-settings/types.ts`：`ModelSettings` 结构
- `hooks/use-settings-form.ts`：已有表单模式，按相邻实现选择复用，不为一项字段重构全部表单

以上缩写路径均相对 `web/src/features/system-settings/`

## 长文规范的读取要求

以下两份规范超过自动上下文注入上限，因此不直接放入 JSONL；实施或检查时必须从原文件读取所涉完整章节，不能用本研究摘要代替规范

- `.trellis/spec/backend/relay-alpha-search-compact.md`：完整读取“场景：Responses WebSocket 首帧等待配置”，以及 Compact 场景中的“Responses WebSocket 生命周期”、相应错误矩阵和测试要求
- `.trellis/spec/backend/logging-guidelines.md`：完整读取“日志级别”“日志格式”“请求 ID”“应该记录的内容”“禁止记录的内容”“敏感数据处理”及“使用指南”
- 使用 `rg -n '^#{1,4} ' <文件>` 定位最新章节再读取，避免依赖已漂移的行号或被截断的注入结果

## 建档工具状态

- 建档前 `task.py current --source` 无当前任务，`untracked_flow.py status` 为 miss，`task.py list --json` 可正常列出任务
- `get_context.py` 全量概览因既有任务树父子循环触发 `RecursionError`，本轮不修改旧任务或工作流脚本
- 该问题不属于首帧恢复功能范围；新任务创建和专属材料验证单独执行

## 2026-11-05 开工前复核与技术定稿

- 当前 HEAD 仍为 `9379c8080`；产品源码没有本任务未提交变更。工作区另有历史任务文档和缓存目录变动，均不纳入本任务。
- 用户本次要求开始已有任务；没有有效 auto-loop run、当前绑定任务或可恢复的未跟踪事项。本次只完成技术收敛与首次 Brief 准备，尚未执行产品验证。
- `common.OptionMapRWMutex` 在 `common/constants.go:57` 已定义，`controller/option.go:80` 的读取 API 与 `model/option.go:278` 的运行时更新共用该锁；新配置可以直接复用，避免反射热写与裸全局热读。
- `model/option.go:221` 的单项更新忽略 `FirstOrCreate` 和 `Save` 错误；`:245` 的 `UpdateOptionsBulk` 使用已检查错误的事务，成功后才派发到内存。新 key 定为 `ResponsesWebSocketFirstFrameTimeoutSeconds`，仅对该 key 分派到现有事务路径。
- `model/option.go:193` 的数据库重载直接进入 `updateOptionMap`，并不经过写入 API 的 `validateOptionValue`，因此新 key 必须在同步入口同样校验，避免历史非法值覆盖缺省。
- `common/responses_websocket_env.go:15` 的现有安全上限为完整正整数秒 `9223372036`。站点设置使用相同上限，统一解析并校验，不引入额外产品限制。
- `web/src/features/system-settings/models/global-settings-card.tsx` 已有 schema、差异计算和页面统一保存入口；新字段接入同一表单，`models/index.tsx`、`models/section-registry.tsx` 和 `types.ts` 负责映射。现有 `useUpdateOption` 在成功后失效 `system-options` 查询。
- 前端已有 `node:test` 和 happy-dom 用例，没有 `test` package script；实施时复用真实 runner，不假设 Vitest 已安装。

### 协议与底层库依据

- [Gorilla v1.5.0 `conn.go`](https://github.com/gorilla/websocket/blob/v1.5.0/conn.go#L990-L1004)：`NextReader` 读取失败具有永久性；[首帧读取期限合同](https://github.com/gorilla/websocket/blob/v1.5.0/conn.go#L1101-L1113) 明确超时后不能继续读取原连接。
- 同版本 `conn.go:924` 在超过读取上限时已发送 `1009`，再追加 JSON 不能作为报文过大的可靠对外合同。
- [RFC 9110 §15.5.9](https://www.rfc-editor.org/rfc/rfc9110.html#section-15.5.9) 将 `408` 用于等待期间没有收到完整请求，允许请求通过新连接重复；本任务将该语义用于 WebSocket JSON 错误封套的状态字段。
- [IANA WebSocket 注册表](https://www.iana.org/assignments/websocket/websocket.xhtml#close-code-number) 将 `1013` 登记为 Try Again Later；本地 Gorilla 同版本已有 `CloseTryAgainLater` 常量。
- 最终合同定为 `408 / responses_websocket_first_frame_timeout / 1013`。它定义网关恢复信号，不证明任意客户端都已实现自动重连，测试须以调用方实际重连并重发完整首帧验证。
