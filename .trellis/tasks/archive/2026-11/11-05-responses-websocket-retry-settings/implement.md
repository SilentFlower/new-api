# 实施计划

当前状态：已按确认的 Brief 完成实现、本地验证、Full Check-All 和规范同步，并按确认计划完成业务提交与推送；任务完成态和 Close 由 Trellis 交付流程同步，未部署或重启生产服务。

## 1. 开工前收敛

- [x] 重读三件套、JSONL、`research/` 和匹配的项目知识，确认工作区基线为 `9379c8080`，现有其他改动不属于本任务
- [x] 定稿首帧 `408 / responses_websocket_first_frame_timeout / 1013` 合同及底层错误分类，核对 Gorilla 源码、RFC 与 IANA
- [x] 定稿站点 key、OptionMap 配置 owner、读写锁和新 key 复用 `UpdateOptionsBulk` 的持久化路径
- [x] 明确只修改首帧恢复和动态设置，其他重试分支维持现状并回归
- [x] 更新并完整展示 Brief，取得首次开工评审确认后运行 `task.py start`
- [x] 按 `trellis-route(target=implement)` 决定执行位置，并加载所属层规范

## 2. 首帧恢复

- [x] 新建 `controller/responses_websocket_first_frame_test.go`，先补真实连接超时错误封套、关闭码及重新建连发送完整首帧成功的回归
- [x] 新建 `controller/responses_websocket_first_frame.go` 集中错误分类，原入口仅分派；超时机器码不要求修改 `relaykit/`
- [x] 为专属超时选择 `1013`；保持其他错误映射和任意事件交付后的原有重试边界
- [x] 覆盖非法 JSON、缺少模型、报文上限、正常关闭及其他读取失败；底层已经发出关闭帧时不再追加业务错误
- [x] 更新已有超时回归，保留及时首帧进入模型校验的行为
- [x] 核对请求关联和安全诊断，确认无完整请求时不选渠、不预扣、不请求上游

## 3. 动态配置

- [x] 新建 `common/responses_websocket_setting.go` 及对应测试，保护配置优先级、`1..9223372036` 秒范围及时间换算
- [x] 在常量文件声明站点 key 与上限，环境初始化复用解析规则；启动期环境值不热写
- [x] 在 `model/responses_websocket_setting_test.go` 先补持久化、重载、非法历史值和数据库保存失败不污染现值的回归
- [x] `model/option.go` 只接入新 key 的缺省注册、双入口校验和 `UpdateOptionsBulk` 分派，不重构其他设置保存路径
- [x] getter 使用现有 OptionMap 读写锁，首帧入口在建立时冻结该连接的 deadline
- [x] 验证新连接生效、存量连接期限不变、重启重载后回显，以及既有多实例同步入口
- [x] 使用有限且有同步屏障的并发回归验证更新与读取；不以随机循环或休眠构造压力测试

## 4. 管理端

- [x] 加载项目 UI、React 和 i18n 规范，在全局模型配置现有表单增加首帧等待字段，说明单位和新连接生效边界
- [x] 同步 `ModelSettings`、模型设置默认映射、section registry、schema、扁平化差异提交和服务端回显
- [x] 同步模型编辑抽屉中另一份 `ModelSettings` 默认值，修复新增必填字段导致的类型错误
- [x] 沿用现有 `node:test`、happy-dom 与 React 测试方式，验证保存、正整数/上限校验、失败反馈和读取环境缺省值
- [x] 同步七语言新增键；按 i18n skill 执行键完整性和源码缺键检查，不用整包覆盖语言文件
- [x] 在隔离数据环境实际操作并截图，核对加载、保存、刷新和错误反馈，不依赖生产模型或线上配置写入

## 5. 回归与自审

- [x] 回归首帧、上游首个错误前重试、任意事件交付后不重放、渠道变化、增量恢复及普通/Compact 多轮路径
- [x] 沿成功、失败、取消和既有重试出口追踪账务与并发租约，核对只处理一次
- [x] 对照 `design.md` 的新建文件、原有文件薄接入清单和回滚方式检查实际差异
- [x] 更新清单最终结论，区分已改、保留限制、静态核对与未验证，不声称所有客户端都会自动重试
- [x] 就地同步 `.env.example` 的配置优先级；WebSocket 规范在 Check-All 后按 Update-Spec 流程更新
- [x] 完成 focused validation 后按 workflow 进入 Check-All，记录通过、失败、跳过与未执行项

### 验证计划

实际执行结果见 `research/verification.md`；全仓格式检查的既有问题与本次涉及文件的格式结果分别记录。

```bash
go test ./common ./controller ./middleware ./model -run 'ResponsesWebSocket' -count=1
go test -race ./common ./controller ./middleware ./model -run 'ResponsesWebSocket' -count=1
go build ./...
go vet ./common ./controller ./middleware ./model
bun --cwd web run typecheck
bun --cwd web run lint
bun --cwd web run format:check
bun --cwd web run i18n:sync
bun --cwd web run build
git diff --check
```

- 前端行为测试按已存在的 `node:test` 方式执行：在 `web/` 使用 Bun 运行实际新增测试文件；实施时依据可用 runner 定稿命令，不凭空假设已有 `test` script。
- i18n 同步工具可能写入文件，实施时遵循 i18n skill 并复核差异；通过同步不能替代源码缺键扫描。
- 普通/Compact 多轮与账务、并发回归复用现有用例，新增测试只保护本任务改变的真实行为。
- 仅当实际改动 `relaykit/` 或其公开 API 时，额外执行 `cd relaykit && GOWORK=off go build ./...`；当前方案不需要该改动。
- 存在未解决验证或环境限制时如实报告，不把未执行的浏览器截图、多实例传播或真实上游计费标为通过。

## 6. 交付边界与回滚

- [x] 首帧入口与错误写出、Option key 注册/校验/事务分派、前端字段映射是主要回滚点，保持可分别核对和撤销
- [x] 撤销本任务新增文件及薄接入点即可回滚；不删除用户原有改动或修改历史已完成任务
- [x] Check-All 之后依所属流程完成首帧恢复及动态设置的规范同步
- [x] 确认精确提交计划后完成业务提交与推送，任务记录按 Phase 3.4 同步；部署和重启另按适用授权与流程执行
