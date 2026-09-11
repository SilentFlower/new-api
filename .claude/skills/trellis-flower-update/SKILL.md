---
name: trellis-flower-update
description: "手动检查和执行已安装 Flower/Trellis 强化包升级。用于用户明确要求更新或升级 flower-trellis、Flower、Trellis 强化包、skill-garden 快照、项目 flower 版本追平，或自动更新提示被稍后/跳过后仍想在对话里升级时。不要用于用户说想发版、发布版本、release、打 tag、npm publish 或修改 package 版本号；这些属于项目发版流程。"
---

# Trellis Flower Update

用于用户主动要求升级已安装的 Flower/Trellis 强化层时。自动 SessionStart 提示的 snooze、skip 和 cooldown 只是不主动打扰，不能阻止用户显式要求升级。

本 skill 不是发版入口。用户说“我想发版了”、release、打 tag、npm publish、更新 package 版本号或准备发布包时，不使用本 skill；按当前项目的 release SOP 或发布规范处理。

## Workflow

1. 确认目标项目。用户给出路径时使用该路径，否则使用当前工作目录。
2. 检查 `flower-trellis` 是否可执行。缺失或入口异常且目标项目存在 `.trellis/scripts/flower_update_hook.py` 时，先运行目标项目的检测脚本：`python3 <target>/.trellis/scripts/flower_update_hook.py --bootstrap-only --target <target>`（路径按当前 shell 引用，Python 命令沿用项目配置）。遵循返回的 `<flower-cli-bootstrap>`：先征得当前成员确认，再安装锁定版本并验证；当前对话已授权这次安装时不重复确认。脚本不存在时说明项目缺少安装引导入口，不猜版本安装。未安装成功则停止依赖 CLI 的步骤，不循环追问或安装。
3. CLI 可用后运行人工检查，默认强制刷新远端版本证据：

```bash
flower-trellis self-check --json --manual --force-remote --target <target>
```

4. 解析 JSON：
   - `update_available`：展示当前版本、推荐版本、release notes 摘要和 `commands.recommended`。
   - `project_out_of_sync`：展示当前 Flower/Trellis 与项目记录的差异，并展示 `commands.recommended`。
   - `up_to_date`：说明 CLI 与项目版本记录一致；版本记录不等于受管内容完整性验证。
   - `project_unknown`：说明 CLI 未发现新版，但项目 Flower 版本无法确认。linked worktree 进入 `trellis-worktree` 的 Flower Preparation，通过 `prepare --inherit-flower --source <source>` 补齐可验证记录后重新检查；来源缺失或冲突时如实报告，不推定需要重装。
   - `disabled` / `offline` / `skipped`：说明原因；不要靠重置缓存伪造可执行状态。
5. 写入前遵守确认和安全门槛：
   - 用户当前消息已经明确要求执行升级时，可以执行 `commands.recommended`。
   - 用户只是询问、查看或比较版本时，只展示结果并等待确认。
   - `safety.reasons` 非空时，先说明风险，再等待用户明确确认。
6. 执行推荐命令后读取 `<flower-update-result>`。如果结果要求 `run_trellis_push_confirmation`，进入 `trellis-push`，展示文件和 commit message 后等待确认。

## Rules

- 不直接读写 `.flower/update-check.tmp`。
- 无论远端查询结果如何，都单独说明 `project.flowerVersionStatus=unknown`；不得用本机 CLI 版本替代项目安装证据。
- 不使用 `update-check reset`、`snooze` 或 `skip` 作为升级绕过手段。
- 不运行 `npm run release`、不打 tag、不 publish，也不修改 `package.json` 版本号。
- 不把 `self-check --manual` 用在 SessionStart 自动 hook；自动路径必须继续尊重提示节流。
- 目标项目有脏文件、活动任务或命令缺失时，按 `safety.reasons` 报告并等待用户确认。
