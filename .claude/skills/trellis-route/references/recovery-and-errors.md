# 路由恢复、异常与只读预检

仅在 helper 异常、无法持久化、需要诊断、auto-loop 启动前选模或 prepare 时读取。正常恢复 hit 由入口直接消费，无须加载本文件。

## helper 结果

- 默认输出精简 JSON：hit/written 含 mode/source/task 等执行证据，miss 含 reason。需要完整 decision、session 路径、context key、pref/auto 路径或写入标记时，在原命令加 `--verbose`；不为诊断额外读 raw runtime。
- 原始合法来源只有 `trellis-route`、`numbered-fallback`、`route-prefs`、`auto-loop`；`runtime` 是 origin，不是 decision.source。scope、task、target、source 和 mode 都由 helper 校验。
- helper 只恢复当前 session 或唯一 session fallback 内当前任务的决定；不匹配视为 miss，不删文件、不借用其它任务/窗口。

| 状态或原因 | 动作 |
| --- | --- |
| 非 Trellis 项目 | 跳过 route，不阻断外层流程 |
| 普通 miss、缺少/不合法 prefs、旧单值偏好 | 读取 interaction 并等待当前选择；不擅自删除配置 |
| `no-current-task` / subject 两者均未命中 | 返回 Request Triage，不伪造任务 |
| `no-session-context` 且 workflow 已允许 task 执行 | 跳过 runtime，用 `read-pref` 读取个人默认或交互选择，说明不能保存 session；不当作 untracked |
| `session-runtime-corrupt/io_error` | 失败关闭：不继续读 prefs/auto-loop，不覆盖损坏文件；报告缺口，由用户决定修复或当前明确选择 |
| `session-auto-run-corrupt/io_error` | 不 fallback 到其它健康 run，不借用其授权，报告缺口 |
| helper 缺失、执行失败或输出不可解析 | 不生成虚构 hit，不猜模式；已有当前合法选择只用于本次，否则询问模式 |
| 写入失败或 skipped | 核对已保存的 pref 与未写入的 session 分别报告；明确当前选择可继续，不虚称持久化成功 |

`write --save-pref` 可能先写 pref 再发现 session 不能写入；因此出错后用 `read-pref` 核对明确请求保存的默认，不从 status=error 推断两者均未写。仅核对已请求写入的结果，不把它当作损坏 runtime 的默认 fallback。

当前明确选择但未持久化时，只说明“本次按你的选择执行，路由未保存，恢复时可能再次询问”；不要求重复选择，也不把自然语言摘要当下一次的合法恢复证据。损坏文件的修复不由 route 擅自执行。

## auto-loop 启动前选模

仅在用户明确要求启动自动循环，且 `trellis-auto-loop` 已解析显式任务列表后使用；普通规划、恢复 running action 或历史启动表述不进入此例外。此时 run 尚未创建，不要求任务已开工，也不创建 task/untracked subject。

1. 对显式队列任务的 implement/check 使用下方只读预检。匹配任务的 runtime、个人默认和本次候选仍由 helper 按既有顺序解析；不得把某一任务的 session 决定复制成整队列模式。已有合法模式的 target 不重复询问。
2. 普通 miss 才按 [interaction.md](interaction.md) 询问未决 target；当前明确选择直接采用，implement/check 保持独立，只有用户明确选择整个循环统一模式时才映射两者。菜单、紧邻编号和不代选规则不变；状态损坏或 helper 异常按上方失败关闭规则处理。
3. 本次临时选择只保留到调用 `auto_loop.py start --route-implement <mode> --route-check <mode>`，只传实际已选择的 target；不得调用 task 的 `write`、手写 runtime 或绑定/切换任务。只有明确要求保存个人默认时调用 `write-pref --target <target> --mode <mode>`，不使用 `write --save-pref`。
4. 选择确定后同轮返回 `trellis-auto-loop` 启动 runner，继续它返回的 prepare action。不得直接 `task.py start`、实施、运行 Check-All 或分发 subagent，也不等待用户回复“继续”。runner 完成 prepare 后，实际执行仍由相应 action 进入普通 route。

启动前询问遵守平台有效的用户输入工具规则；Codex 使用允许的同步 `request_user_input`，不可用时在主对话提问并等待，不调用异步询问工具。选择模式不替代 Open Questions、readiness、Brief 或 Git 安全校验。

## auto-loop prepare 预检

启动前选模和 runner prepare 共用同一 helper 的只读预检：

```bash
python3 <skill-dir>/scripts/route_state.py resolve --target <implement|check> --read-only --task <repository-relative-task-path> [--auto-mode <runner-candidate>]
```

顺序为匹配任务的 runtime → prefs → runner 显式候选；不写 session、不绑定任务、不扫描其它 run 借用授权。`--task` 只接受项目 `.trellis/tasks/` 内存在的非软链路径，`--task/--auto-mode` 仅用于只读模式。预检不是执行决定；实际执行仍由普通 resolve 校验和持久化。缺少 helper 或合法 mode 时 runner 保守要求相应 JSONL context。

正常 auto-loop 授权仅来自当前 session 绑定的 run 或唯一 running run，且当前任务必须属于 pending/running 队列。个人默认高于此临时授权；runtime 已选决定高于个人默认。run 已停止、队列项已完成、task 不属于 run、状态损坏或 run 不唯一时不得借用授权。
