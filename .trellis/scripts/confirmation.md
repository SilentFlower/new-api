# 原生后台确认

本文件由 Brief、Check-All、Push 三个 owner 按需读取。Codex / Claude 的 Linux/WSL 或原生 Windows 会话须通过宿主能力核验；项目 `confirmation_timeout` 缺省开启。其它平台、禁用、能力或展示无法核验时维持原人工流程，不从沉默推断人工确认。

## 适用边界

- Brief：已收敛且完整展示的实际 Brief，默认动作 `implement`。产品取舍、范围或 Spec 歧义仍未解决时先问用户，不准备计时器。
- Check：完整 interactive 报告仍有待处置 CHK/FBK，默认动作 `repair_all_and_recheck`；先结束报告回合，到期领取后再进入原 repair-and-recheck。DOC、已解决、有效已接受项不计数，不自动接受风险。
- Push：完成原预检与完整精确计划，默认动作 `execute_git_plan`；Check-All/Spec 证据不完整、存在尚未接受的实质风险或 Git 停止条件时保持人工。普通 push 或用户指定 commit-only 模式保持不变。
- 已校验 auto-loop 沿用 runner 授权，不叠加本计时器。新产品选择、风险接受、Spec 歧义、宿主权限审批，以及正在开发/验证本机制自身的任务均保持人工。

## 入口决策

三个 owner 必须在最终回复前执行本分支，而不是只在收到提醒后才读取协议。已有当前范围的人工/runner 授权直接续接；显式停止、未决产品决定/授权、auto-loop、本机制自身开发保持原人工或 runner 流程。其余合资格交互实际尝试 prepare，不要求用户额外申请超时，也不要求已有领取回执。仅准备成功不允许执行业务。

人工回退使用实际 `reason`、工具错误或明确排除条件，不能未尝试就推测不支持。prepare 成功而 wait 失败时，先 cancel 本请求，再展示正文、具体失败原因和人工提示；未实际启动后台等待不得展示自动执行倒计时。展示后不继续工具轮询。

## 准备与完整展示

Codex 保持普通 `codex` / `codex resume` 与默认共享服务。通过当前根会话内置 codex_tui 的本机监听者绑定原前台；需要新输入 Hook 已实际加载。Windows 复用当前 Codex 映像的本机代理接入已有控制端点。缺少服务、出生身份或可靠退出能力时保持人工。Claude 保持原来的普通 TUI；Windows 同时核验原生 Hook 的前台进程。

先从项目根执行 `python3 .trellis/scripts/flower_confirmation_hook.py --entry`，读取实际返回的 `entry` argv，后续在其后追加 `prepare`、`wait` 等子命令。入口固定为项目内 `.trellis/scripts/flower_confirmation.py`，内部复用当前 Python 解释器，仅依赖标准库和项目脚本。`entry=null` 或工具失败才报告实际原因；不能用无 stdout 的 `test -f` 推断入口不存在。确认阶段无需 Node、npm、全局 Flower 或额外 pip 安装。

1. 在 gitignored `.trellis/.runtime/` 为当前平台和会话写一个 JSON 材料文件，路径不得复用于并行会话。不要把它放入提交集合，不生成公开报告附件。使用结构化文件写入，用户正文和 message 不拼入 shell。
2. JSON 的公共字段为 `kind: brief|check|push`、`task`。Brief 必须指定目标活动任务的精确相对路径，正文读取该任务 `brief.md`；当前会话允许尚未绑定任务，已有绑定须与目标一致。会话绑定另行冻结，等待期间变化使旧确认失效。Check/Push 使用当前精确任务相对路径，无任务为 null，另提供完整标准报告/计划的 `body`。
3. Check 必填 `reportVersion`、`dispositionVersion`（非空版本字符串）及 `findings: [{id:"CHK-001",status:"pending"}]`；status 只接受 pending/accepted/resolved。必须覆盖当前全部条目，禁止只传准备修复的子集来改变默认动作。
4. Push 必填 `mode: push|commit-only`、`evidence: {check:passed|accepted|focused,spec:no-op|written,unresolvedRisks:0}` 和有序 `repositories`。每仓包含 `root`、实际 `branch`、完整 `head`（初始空仓为 null）、实际 `upstream`（没有为 null）、精确 `message`、完整 `files` 路径列表。把已展示的生成命令、任务记录 exact files 与顺序一并保存在输入中，不在领取后扩大范围。
5. 从项目根执行 `python3 -X utf8 .trellis/scripts/flower_confirmation.py prepare --platform codex|claude --input <相对路径>`；用户明确指定本次时长时加 `--seconds <正整数秒>`。仅 `prepared=true` 可继续，任何错误或 false 保持原人工提示。
6. 两端运行同一个 `python3 -X utf8 .trellis/scripts/flower_confirmation.py wait --platform <平台> --session <prepare.sessionId> --request <UUID>`。Claude 用 Bash `run_in_background:true`。Codex 用原生 `exec_command` 短 `yield_time_ms`（例如 1000）启动，取得持续运行的 terminal session 后结束当前回合；不等待命令完成、不轮询、不启动子代理、不用 clock.sleep。后台 worker 到期会经原生同会话队列唤醒。
7. 最终回复完整展示 prepare 返回的 `display`；它包含标准正文和本次时长/默认动作。可在前后补必要说明，但不能省略、改写 display。后台会核验原回合最终正文，展示完成才开始计时；工具启动和生成时间不扣减等待时间。

Git 整仓快照、仅用于哈希的文件以及两端会话记录不按总字节数拒绝。需要解析或展示的单条记录、材料和状态各有读取预算。Git 命令、Push 快照和策略检查分别使用超时预算，失败时展示实际阶段及原因。等待原回合完成展示与唤醒后等待领取分别过期；两者与用户设定的倒计时独立。

## 到期领取与原流程续接

原生提醒仅唤醒：先执行 `python3 -X utf8 .trellis/scripts/flower_confirmation.py claim --platform <平台> --request <UUID>`。必须实际取得 `claimed=true`、`source=policy_timeout`、预期 action 和当前冻结 context，才可续接。false、错误、材料或会话漂移时停止旧动作，不重复领取旧请求、不假称用户已确认；在同一回合进入下述恢复分支。

### 失效提醒的自动恢复

领取失败表示本次旧许可无效，不是当前工作流的终止条件。旧版提醒中的“结束本回合”按本节恢复规则处理。先读取当前用户消息及已有工作流上下文，再执行一次 `python3 -X utf8 .trellis/scripts/flower_confirmation.py status --platform <平台>`。只查询当前项目和当前原生会话，不搜索其它会话的许可，也不改用 status 返回的新请求 ID 领取旧通知。

- 当前有 `prepared`、`waiting`、`dispatching` 或 `queued` 请求：保留当前请求及其后台等待，不重新准备、不重置计时、不重复启动 worker。迟到提醒到此处理完毕；继续当前用户消息中已授权的工作。当前请求由其自己的原生提醒领取。
- 当前为 `consumed`：读取 `receipt` 和 `execution`，由原 owner 核对实际进度。已 `started` 的动作先据实恢复和记账，已完成的动作进入下一步；禁止重新执行旧默认动作。缺少 started 记录时也不能借本次旧通知补领或补启动。
- 当前为 `absent`、`cancelled` 或 `failed`：从当前任务、最新报告或精确 Git 计划恢复原 owner。当前工作仍需要确认且范围已明确时，重建最新材料，自动执行原 prepare/wait/display 并重新计时。新请求必须完整展示并独立取得 claim 与 started；不沿用旧倒计时、旧回执或旧材料。没有待续工作时仅忽略旧提醒。
- 当前为 `delivery_unknown`：先由原 owner 核对队列及实际执行结果，再按实际进度恢复。投递结果未知本身不授权重发或重执行业务。
- 用户已明确暂停、取消或改变方向时，优先执行该指令。新的范围或产品取舍、风险接受、Spec 歧义、宿主权限及能力缺失仍按入口决策处理；只有这些实际条件才回到相应人工流程。单纯 `not_current` 或旧许可失效不要求用户再手动确认。

一次提醒最多自动重新准备一次。准备或等待实际失败时报告具体原因并沿原人工分支交接，不在本回合循环领取、轮询或反复重建计时器。

### 有效领取后的原流程续接

领取后、首个业务动作前，执行 `python3 -X utf8 .trellis/scripts/flower_confirmation.py result --platform <平台> --request <UUID> --receipt <回执 UUID> --state started`。只有 `recorded=true` 才进入对应 owner；重复 started 不能再次执行。Brief 继续原 `task.py start` 与 route；Check 继续原“修复全部”及重检；Push 继续原精确范围和逐仓预检、提交、推送、进度/Close 及失败恢复。不得把 receipt 当成新产品决策、原生审批或任意 shell 的授权。

按实际结果用同一个回执记录 completed / interrupted / needs-verification；已开始的副作用不能伪称撤销。不确定时记录 needs-verification 并按原 owner 恢复，不重跑旧默认动作。新人工消息优先按现有流程处理，不因已有 receipt 忽略用户。

进入下一确认点前先记录本次动作的终态，不能用新 prepare 覆盖 started 回执。Brief 的动作完成指激活任务并进入原路由；Check 指本轮修复与重检结束，须在后续 Spec/Push 确认前记账；Push 指精确计划的实际执行结果。业务任务本身是否完成仍由原任务流程判断。

## 自定义与取消

默认 Brief 为 1000 个规范化 Unicode 字符内 100 秒，之后每增加 500 字加 30 秒；Check 0 项不等待、1 项 100 秒、每多一项加 30 秒；Push 200 秒。可在 `.trellis/config.yaml` 配置：

```yaml
confirmation_timeout:
  enabled: true
  brief:
    enabled: true
    base_seconds: 100
    threshold_chars: 1000
    increment_chars: 500
    increment_seconds: 30
  check:
    enabled: true
    base_seconds: 100
    increment_seconds: 30
  push:
    enabled: true
    seconds: 200
```

支持正整数秒数 1～86400，超界或坏配置保持人工，不缩短为更早执行。单次 `--seconds` 优先，但不绕过关闭开关。用户任何新消息都会使旧等待失效；人工确认沿用原流程。改时或改材料先使旧请求失效，更新 JSON，再重新 prepare/wait/display。显式取消使用 `python3 -X utf8 .trellis/scripts/flower_confirmation.py cancel --platform <平台> [--request <UUID>]`；status 离线只读。

Codex 原窗口退出或其原生服务失效会取消旧等待；恢复同一会话不得把旧请求迁移到新窗口。同会话双开会产生未对应独立会话的额外前台，检测到后保持人工；不得借用第一窗口证明第二窗口存活。原窗口失效后旧请求不能转移到后来打开的窗口。Claude 使用原生退出事件取消。重复或迟到通知可能增加模型回合，但不能重复有效领取；已经开始的业务副作用如实记账，不宣称自动回滚。
