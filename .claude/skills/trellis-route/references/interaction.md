# 路由选择与默认管理

仅在当前 target 仍有未决选择、用户要求菜单/重选/清除，或处理紧邻编号答复时读取。目标、模式和保存意图已明确的当前指令直接按入口应用，不要求重复选择。

## 选项与等待

使用可用的用户输入工具，或在聊天中展示相同编号选项并等待；工具不可用不是自动代选理由。implement 与 check 各自独立，选择一个不授权另一个。

| 编号 | implement 选项 | check 选项 |
| --- | --- | --- |
| 1 | 本次 Inline | 本次 Check-all inline |
| 2 | 本次 Subagent | 本次 Check-all subagent |
| 3 | 保存默认：Inline | 保存默认：Check-all inline |
| 4 | 保存默认：Subagent | 保存默认：Check-all subagent |

重选时标题说明当前默认或无默认，1/2 仅覆盖本次，3/4 更新对应默认，并增加 `5. 清除默认`。无默认时主动菜单标明内置 inline；target 含糊只先问目标，不擅自同时修改两种默认。light/full 交给 Check-All，不扩充 mode 选项。

裸数字回复（如 `1` / `2` / `3` / `4`）只有在当前可见的上一条 assistant 消息刚展示同一个 target 的选项，并明确停下等待回答时才有效。compact summary、ordinary summary、SessionStart 摘要、replacement history、历史消息、旧 target 或非紧邻裸数字都不能触发 `write --source numbered-fallback`；没有有效紧邻选项时重新展示并等待。

普通聊天编号选择用 `source=numbered-fallback`；用户输入工具返回的当前选择用 `source=trellis-route`。不从历史数字、默认预选但未提交的选项或无答复推断选择。

## 应用选择

`<skill-dir>` 按入口确定。task 的本次选择：

来自 auto-loop 启动前选模时，先按 [recovery-and-errors.md](recovery-and-errors.md) 的例外应用选择并返回 runner，不执行下方 task `write` 或末尾同轮实施；本次选择由 runner 的 start 参数保存，明确要求个人默认才使用 `write-pref`。

```bash
python3 <skill-dir>/scripts/route_state.py write --target <implement|check> --mode <mode> --source <trellis-route|numbered-fallback>
```

选项 3/4 或明确保存意图在同一命令加 `--save-pref`；只有写入成功才报告已保存。untracked 的本次选择只用于当前调用，不写 runtime；保存默认：

```bash
python3 <skill-dir>/scripts/route_state.py write-pref --target <implement|check> --mode <mode>
```

明确清除指定默认或选项 5 时只清该 target；成功后返回入口，以内置 inline 继续，不再弹菜单；已有本任务临时选择仍遵守入口优先级：

```bash
python3 <skill-dir>/scripts/route_state.py clear-pref --target <implement|check>
```

helper 保留另一个 target 的个人偏好；task 写入还保留当前 session 的其它字段，并清理属于旧任务的 route 决策。不要手写 JSON 或直接删文件。失败先按入口的异常 reference 核对实际持久化结果；决定已明确但未持久化时简短提示可能在恢复时再次询问。

选择确定后回到入口，按入口的展示规则在同一回合实际执行。
