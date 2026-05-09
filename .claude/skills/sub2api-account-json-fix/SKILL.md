---
name: sub2api-account-json-fix
description: 修正当前项目里的 sub2api 账号导出 JSON，并把这些账号直接推送到 sub2api admin 接口。用于批量推送 `sub2api-account-*.json` 到远端 sub2api，尤其适合先同步模板里的 `model_mapping`、`allow_overages`，再按指定 group_ids 创建账号。
argument-hint: <template-json> [target-json...]
disable-model-invocation: true
---

# Sub2API 账号推送

先确认模板账号，再运行项目内的推送脚本。

## 规则

- 先用模板账号补齐目标账号的 `credentials.model_mapping`。
- 确保目标账号包含 `extra.allow_overages`。
- 把 `name` 改成 `credentials.email`。
- 然后把代理推送到 sub2api，再逐个创建账号。
- 创建账号后默认立刻关闭调度。
- 如果账号带有 `refresh_token`，创建后默认立刻调用一次 refresh，确保远端拿到最新 access token。
- 如果 `.claude/skills/sub2api-account-json-fix/env/push.env` 不存在，继续检查系统环境变量。
- 如果地址或 token 仍未配置，或者 token 还是 `admin-xxxx` 这种示例值，先向用户索要真实值，不要直接推送。

## 技能目录配置

优先读取：

- `.claude/skills/sub2api-account-json-fix/env/push.env`

支持这些键：

- `SUB2API_ACCOUNT_JSON_FIX_PUSH_ADMIN_BASE_URL`
- `SUB2API_ACCOUNT_JSON_FIX_PUSH_TOKEN`
- `SUB2API_ACCOUNT_JSON_FIX_PUSH_GROUP_IDS`
- `SUB2API_ACCOUNT_JSON_FIX_PUSH_SCHEDULABLE`
- `SUB2API_ACCOUNT_JSON_FIX_PUSH_REFRESH_AFTER_CREATE`

## 执行方式

如果用户给了参数，按“第一个参数是模板文件，后续参数是目标文件”的规则执行；否则先向用户确认模板文件。

运行：

```bash
bash .claude/skills/sub2api-account-json-fix/scripts/run.sh $ARGUMENTS
```

如果用户没有显式要求写回本地文件，就不要自动加 `--write`；先预览或直接推送内存中的规范化结果即可。
