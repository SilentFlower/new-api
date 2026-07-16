# 上线操作

## 结论

存在可选的上线后配置操作。功能默认关闭，部署本身不要求修改配置。

## 已核对证据

- `task.json`
- `prd.md`
- `design.md`
- `implement.md`
- `implement.jsonl`
- `check.jsonl`
- 提交 `538dc0893` 与 `bc1fcfc60` 的变更文件

## 漂移检查

此前缺少 `release.md`；任务材料和 Git 证据一致。

## SQL 变更

无。

## 配置变更

- `general_setting.non_stream_keepalive_enabled` 默认关闭。需要启用非流式 JSON 空白心跳时，由管理员在系统设置中开启。
- 心跳间隔复用 `general_setting.ping_interval_seconds`；启用前应确认该值符合当前代理超时策略。

## 批处理、部署脚本与数据修复

无。

## 外部系统与依赖平台

无。

## 上线顺序

先部署代码，再按需开启非流式响应保活。首次启用建议小范围验证后再扩大使用范围。

## 回滚说明

优先关闭 `general_setting.non_stream_keepalive_enabled`，无需数据回滚。

## 上线后验证

- 开关关闭时确认现有非流式请求行为不变。
- 开关开启后确认长耗时 JSON 响应能收到空白心跳并最终解析成功。
- 确认音频、文件、实时流和其他非 JSON 响应未启用该能力。
