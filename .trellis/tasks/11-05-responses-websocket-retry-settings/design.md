# 技术设计

## 责任与状态边界

- 首帧读取、错误分类和下游关闭由 `controller` 的 Responses WebSocket 入口负责。
- 首帧超时前没有完整业务请求，恢复单位是新连接；不在原连接重读、不伪造 `response.create`，也不在此阶段选渠、预扣或调用上游。
- 首帧之后继续使用现有 `connectResponsesWebSocketTurn`、`shouldRetry`、BillingSession 和并发租约，不新增重试框架或修改重试次数、状态码规则。
- 动态设置复用现有 Options 表、管理 API、`common.OptionMap` 和 `common.OptionMapRWMutex`；不新增 API、数据库结构或配置中心。

## 首帧恢复合同

### 错误分类

| 触发条件 | 对外结果 | 处理方式 |
| --- | --- | --- |
| 首帧读取超时 | 原有 `type=error` 封套，`status=408`、`error.code=responses_websocket_first_frame_timeout`，随后关闭码 `1013` | 告知调用方重新建连并重发完整请求；不在服务端透明重放 |
| 收到首帧但 JSON、type 或 model 非法 | 保留现有 `400 invalid_request` 和输入拒绝关闭语义 | 继续由现有解析器处理 |
| `websocket.ErrReadLimit` | 保留 Gorilla 已发送的 `1009` 报文过大关闭信号 | 不在底层已关闭后追加 JSON 或第二个关闭帧 |
| 对端正常关闭或离开 | 保留关闭握手 | 不再构造首帧参数错误 |
| 其他传输或协议读取失败 | 终止连接，不返回可恢复超时信号 | 保留底层已发送的协议关闭信号；不推测请求可重放 |

- 用 `errors.As` 检查网络错误的 `Timeout()`，用 `errors.Is` 检查读取上限，用 WebSocket 关闭错误区分正常关闭；不靠原始错误文本判断可重试性。
- 超时机器码作为 `controller` 本地的 `types.ErrorCode` 常量定义；沿用现有 `NewAPIError` 和 JSON 包装，不需要修改 `relaykit/` 的公开 API。
- `writeResponsesWebSocketError` 仅针对该超时机器码选择 `websocket.CloseTryAgainLater`；其他调用方继续使用原有关闭码映射。
- 错误继续关联请求 ID。诊断只记录首帧阶段、分类及必要的有效期限，不记录原始报文、对话、凭据或任意原始错误字符串。
- 错误中的 `408` 是升级后 JSON 封套的状态字段，不是再次发送 HTTP 响应；成功 upgrade 的 HTTP 状态仍为 `101`。

### 技术依据与限制

- [Gorilla v1.5.0 `SetReadDeadline`](https://github.com/gorilla/websocket/blob/v1.5.0/conn.go#L1101-L1106) 明确读取超时会使连接后续读取失败，因此必须重连。
- `408` 的未及时收到完整请求语义参考 [RFC 9110 §15.5.9](https://www.rfc-editor.org/rfc/rfc9110.html#section-15.5.9)；`1013` 的 Try Again Later 名称见 [IANA WebSocket 注册表](https://www.iana.org/assignments/websocket/websocket.xhtml#close-code-number)。二者用于本任务的网关恢复合同，不表示任意客户端必然自动恢复。
- new-api 的服务端 `shouldRetry` 默认不包含 `408`，本任务不修改该规则；首帧未形成业务轮次，不能通过删除内部 `SkipRetry` 标记实现恢复。

## 动态设置

### 配置 owner 与数据流

- 站点设置 key 定为 `ResponsesWebSocketFirstFrameTimeoutSeconds`，采用现有传统 Option 路径；数值语义与现有环境变量一致。
- 新建 `common/responses_websocket_setting.go`，集中正整数秒数解析、有效值读取及安全时长换算；常量包保留 key、默认值和时长上限。
- `constant.ResponsesWebSocketFirstFrameTimeoutSeconds` 仅保留启动期环境缺省职责，运行期间不热写该裸变量。
- `model.InitOptionMap` 注册环境缺省或 60 秒默认值；数据库加载后，有效站点值覆盖该缺省。`GetOptions` 已从 OptionMap 返回字符串值，页面和新连接读取同一事实源，无需新增返回字段或修改 `controller/option.go`。
- 首帧入口经公共 getter 在 `OptionMapRWMutex.RLock` 下取得值并规范化，在本连接开始读取首帧时冻结 deadline；配置更新不追溯修改存量连接。
- 优先级：有效站点值 > 已规范化环境变量 > 60 秒。非法历史站点值在载入边界拒绝，保留当前有效值或启动缺省，并记录不含配置正文的诊断。

### 校验与持久化

- 统一使用有符号 64 位整数解析，接受范围为 `1..9223372036` 秒，即现有 `time.Duration` 可表示的完整正整数秒上限；前端 Number 在该范围内可精确表示整数。
- 环境变量初始化复用同一安全范围，继续保持缺失、空值、非法、非正数及溢出回退 60 秒的行为。
- `model.validateOptionValue` 和数据库同步进入 `updateOptionMap` 前的专属分支均调用同一解析规则，避免写入非法值后再校验。
- 现有 `model.UpdateOption` 忽略 `FirstOrCreate`、`Save` 错误。本任务只为新 key 增加窄分派，复用已检查数据库错误的 `UpdateOptionsBulk`：先事务持久化成功，再更新 OptionMap。其他设置的保存路径不重构。
- 保存失败不改变内存，既有管理 API 反馈错误；成功后由 `useUpdateOption` 失效 `system-options` 查询并回显服务端结果。
- 其他实例通过现有 `SyncOptions` 周期加载站点值，不承诺即时广播。配置读取与写入共同复用 OptionMap 的读写锁，不为本字段引入反射热写或第二份原子缓存。

## 前端接入

- 入口为“系统设置 → 模型设置 → 全局模型配置”，在既有保活配置附近增加独立首帧等待字段，单位秒。
- `ModelSettings`、模型设置初始映射、section registry、全局表单 schema、扁平化差异提交和输入框完整贯通。
- `defaultModelSettings` 仅作加载占位；设置读取成功后必须使用后端有效值，不能在环境变量为 120 秒时仍显示 60 秒。
- 沿用同一表单和保存动作，避免新建第二个表单抢占页面统一保存入口。整数、正数和上限校验提供字段错误，保存失败提供既有反馈。
- 说明保存影响之后新建的连接；同步 `en/zh/zh-TW/fr/ru/ja/vi`，不重排无关设置或重构公共表单。

## Build 分支薄接入计划

| 新建文件 | 完整职责 |
| --- | --- |
| `controller/responses_websocket_first_frame.go` | 首帧读取错误分类、恢复错误构造和安全诊断 |
| `controller/responses_websocket_first_frame_test.go` | 真实 WebSocket 错误、关闭信号与重连恢复合同 |
| `common/responses_websocket_setting.go`、对应测试文件 | 配置解析、并发安全有效值读取与时长换算 |
| `model/responses_websocket_setting_test.go` | Option 持久化、同步、非法历史值和数据库错误不污染有效值 |
| 前端全局设置行为测试文件 | 字段保存、校验、失败反馈与服务端回显 |

| 必须修改的原有文件或区域 | 最薄接入点与必要性 |
| --- | --- |
| `controller/responses_websocket.go` | getter 设置 deadline、首帧读取失败分派、专属超时关闭码映射；保留现有解析和主转发循环 |
| `constant/responses_websocket_env.go` | 声明站点 key 和统一安全上限，保留环境缺省常量与启动值 |
| `common/responses_websocket_env.go` 及已有回归 | 环境初始化复用解析规则，保持前序配置兼容 |
| `model/option.go` | 缺省注册、校验、数据库加载前保护及新 key 事务保存分派；不重构其他选项 |
| `controller/responses_websocket_timeout_test.go` | 更新原有超时对外合同，保留及时首帧验证 |
| `web/src/features/system-settings/models/global-settings-card.tsx` | 加入同一表单的 schema、扁平化键和输入字段；保持统一保存动作 |
| `models/index.tsx`、`models/section-registry.tsx`、`types.ts` | 默认映射、服务端值传递和类型声明 |
| `web/src/features/models/components/drawers/model-mutate-drawer.tsx` | 补齐该处复用的 `ModelSettings` 默认值，不改变模型编辑行为；由类型检查发现这一使用方 |
| 七语言文件、`.env.example`、所属 WebSocket 规范 | 就地同步文案、配置优先级和错误合同 |

复用 `common` JSON、OptionMap 锁、Options、`UpdateOptionsBulk`、表单组件、查询失效、BillingSession 和并发租约；不修改 `setting/config`、`GeneralSetting`、中间件、Relay 适配器或 `relaykit/`。

## 兼容、回滚与同步

- 其他重试分支保持 [重试边界清单](research/retry-inventory.md) 中的现有边界：任意事件已交付、输入或权限错误、取消、额度或并发限制，以及缺少完整历史的增量请求均不得通过放宽重试绕过。
- 普通 Responses 和 Compact V2 共用首帧链路共同回归；Realtime 仅记录入口差异，不修改其实时会话和计费行为。
- 冲突面集中在 WebSocket 首帧入口及关闭码映射、Option 接入点和全局表单字段映射。上游同步后逐项复核这些位置及七语言键并集。
- 回滚撤销新增模块和薄接入点，不涉及 schema 迁移；旧版本忽略新站点 key，并恢复仅环境变量与 60 秒默认路径。
- 对成功、失败、取消和既有重试路径核对预扣、退款、结算及并发租约释放，验证只发生一次。
