# Brief — 修复 Responses WebSocket 首字时间漏记

## Goal

- 让普通 Responses WebSocket 的消费日志记录并展示每轮真实首响应时间。

## Scope

- 补齐普通 WS 上游事件入口的首响应记录，增加首帧、后续帧、连续两轮和未收到响应的正式回归。

## Non-Goals

- 不修改 codex-proxy-rs、前端显示规则，不新增计时字段或数据库迁移，不推算和回填历史首字时间。
- 不扩展为 WS 选渠、重连、重放、计费或页面重构。

## Technical Overview

- 首个上游响应事件更新本轮 RelayInfo，复用现有首次记录能力和日志 `other.frt` 毫秒字段；后续事件不覆盖，同一连接的下一轮独立计时。

## Context & Decisions

- 已复现普通 WS 收到业务事件后 `frt` 仍为 `-1000`；`response.created` 即可作为首响应，无需等待正文。
- 保持现有透传、usage、预扣与结算、重试、SSE 和 Compact 行为；截图对应请求的实际传输尚未核实。

## Acceptance

- AC1：新增回归先在旧实现失败；请求起点设为两秒前，处理 `response.created` 后日志首响应时间为合理正值。
- AC2–AC3：后续帧不覆盖首次时间，连续两轮分别使用各自的起点和首响应。
- AC4：未收到上游响应时仍为未知，Ping/Pong 不作为模型响应。
- AC5：成功完成后的日志记录首响应时间，Token、总耗时、速率和费用保持原合同。
- AC6：相关普通 WS、SSE、Compact 回归通过，记录命令及未实测的集成范围。

## Next Step

- 摘要确认后启动任务，先补充并运行能复现缺陷的正式回归。
