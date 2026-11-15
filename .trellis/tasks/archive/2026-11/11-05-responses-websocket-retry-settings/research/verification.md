# 本地验证记录

## 验证范围

- 代码基线为 `9379c8080`，本任务修改尚未提交。
- Go `1.25.1`、Bun `1.3.14`；浏览器使用本机 Playwright Chromium。
- 页面测试使用临时 SQLite、随机本地端口、独立进程和测试管理员；未访问生产数据库、线上配置或真实模型上游。
- 本地实例测试结束后已停止；未提交、推送、部署或重启生产服务。

## 失败回归与修复

- 实现前运行 `go test ./controller -run '^TestResponsesWebSocketFirstFrameTimeout$' -count=1`，复现原有 `400 invalid_request / 1008`，与本任务要求的超时合同不符。
- 实现前运行 `go test ./model -run '^TestResponsesWebSocketOption' -count=1`，复现非法值落库、环境缺省未导出、历史非法值污染运行时，以及只读数据库保存失败却返回成功并更新内存。
- 前端实现前运行新增行为测试，三项均因缺少首帧输入框失败；接入后全部通过。

## 已通过的代码验证

| 验证 | 结果与保护行为 |
| --- | --- |
| `go test ./common ./model ./controller -run 'ResponsesWebSocketFirstFrame\|ResponsesWebSocketOption' -count=1` | 通过；正整数/溢出、优先级、保存失败及首帧超时合同 |
| `go test ./controller -run 'ResponsesWebSocketFirstFrame' -count=1` | 通过；实际关闭码、协议错误、报文上限、正常关闭、期限冻结、重连请求与单次记账 |
| `go test -race ./common ./controller ./middleware ./model -run 'ResponsesWebSocket' -count=1` | 四包通过；覆盖新配置并发读取和既有普通/Compact 多轮、重试与租约路径 |
| `go test ./... -count=1` | 全量通过；复用现有上游失败重试、已交付事件不重放、增量恢复与账务回归 |
| `go build ./...` | 通过；另构建包含最新前端产物的本地验证二进制 |
| `go vet ./common ./controller ./middleware ./model` | 通过 |
| `go vet ./...` | 通过；最终核对时覆盖全部根模块包 |
| `bun run typecheck` | 通过；已同步模型编辑抽屉中复用的 `ModelSettings` 默认值 |
| `bun test src/features/system-settings/models/__tests__/responses-websocket-settings.test.tsx` | 三项通过：服务端值回显、非法值拒绝、只提交目标键与整数秒数 |
| 同时运行首帧设置、tool-price-validation 和 group-auto-limit-validation 三个前端测试文件 | 六项通过；核对同一测试进程中的相关表单行为 |
| `bun run lint` | 无错误；存在其他文件的既有 warning，本次涉及文件定向 lint 无问题 |
| `bun run build` | 通过 |
| `gofmt -l` 对本任务 Go 文件 | 无输出 |
| `git diff --check` | 通过 |

## 格式检查边界

- 本次涉及的七个前端源码/测试文件均已通过保留许可头的正文格式检查，原许可头逐字节保留。
- `bun run format:check` 全仓检查未通过，报告以下四个文件；已核对四者均与 `HEAD` 一致，未因本任务产生变更：
  - `web/src/features/channels/components/drawers/sections/__tests__/responses-reasoning-included-field.test.tsx`
  - `web/src/features/keys/lib/__tests__/name-limit-form.test.ts`
  - `web/src/features/users/lib/__tests__/name-limit-form.test.ts`
  - `web/src/features/users/lib/user-form.ts`
- 未为本任务格式化这些无关文件。

## 国际化

- 新增三个键，均通过 `add-missing-keys.mjs` 写入七语言，再执行 `bun run i18n:sync`。
- 源码缺键扫描为 `0`；七语言均为 `5986` 个键，sync 报告的 missing/extras/untranslated 均为 `0`。
- 新增键在六种非英文语言均为本地化值，动态校验文案已登记到 `static-keys.ts`。
- 原始英文同值启发式扫描仍命中既有品牌、同形词和其他功能条目；本次语言包差异仅为每包新增三个键，没有覆盖既有翻译。
- 三个临时 i18n 脚本已删除。

## 浏览器、API 与数据库

详细结果见 [浏览器验证](browser-verification.json)。以下七项均通过：

1. 加载期间不展示错误的 60 秒占位值；没有站点记录时实际显示环境缺省 120 秒。
2. 页面保存 180 秒，真实 API 只提交目标 key，SQLite 保存字符串 `180`；刷新仍显示 180。
3. `0`、`1.5` 和 `9223372037` 显示字段错误，不发送更新请求，也不改变数据库。
4. 网络边界注入 API 业务失败，页面显示失败提示；刷新后仍为 180。真实数据库写入失败另由只读 SQLite Go 回归覆盖。
5. 法语页面实际显示新标签、说明及 180 秒有效值，布局可用。
6. 停止并重启隔离应用，同时把环境缺省改为 240 秒，页面及 SQLite 仍为站点值 180。
7. 浏览器 `pageerror` 为 `0`。

截图：

- [中文设置](settings-zh.png)
- [中文输入校验](settings-validation-zh.png)
- [保存失败反馈](settings-save-failure-zh.png)
- [法语设置](settings-fr.png)

## 未执行与边界

- 未验证任意具体客户端是否自动重连；本任务保证网关恢复信号和调用方主动重连的可用性。
- 多实例周期传播通过现有 `SyncOptions → loadOptionsFromDatabase → updateOptionMap` 调用链与重载回归核对，未启动第二个应用实例或操作线上节点。
- 未进行 MySQL/PostgreSQL 独立实例测试；持久化复用已有通用 GORM 事务路径，没有新增 SQL 或数据库结构。
- `relaykit/` 和公开 API 未修改，因此未额外执行独立模块构建。
