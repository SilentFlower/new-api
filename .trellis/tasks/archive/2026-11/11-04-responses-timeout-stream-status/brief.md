# Brief — 修复 Responses 首帧超时配置与异常流终态记录

## Goal

- 让 Responses 首帧等待可配置，并准确记录 SSE 业务失败与不完整响应。

## Scope

- 首帧超时环境变量、配置说明及读取期限回归。
- Responses SSE 业务终态观察、日志状态与对应回归。

## Non-Goals

- 不调整计费规则、日志类型、请求 ID 关联或历史数据。
- 不修改前端、Codex Proxy、Nginx、其他协议或执行线上部署。

## Technical Overview

- 新增 RESPONSES_WEBSOCKET_FIRST_FRAME_TIMEOUT_SECONDS，启动读取，默认 60 秒；无效配置回退 60 秒。
- 观察 failed/incomplete/error 等异常事件，以及缺少完成事件的正常传输收尾，通过现有 RecordError 使日志 status=error 并展示原因。
- end_reason 保留传输结束原因；[DONE] 不清除业务错误。沿用原始事件转发和既有结算路径。

## Context & Decisions

- 用户指定首帧默认 60 秒；线上已证实固定 30 秒触发超时。
- 已查到 Proxy 记录 incomplete、new-api 却记录 done/ok 的同一实际执行。
- 零 Token 不能作为失败依据；预热、搜索和工具调用按协议终态判断。
- 现有前端已经能展示错误状态，无需新增页面或翻译。

## Risks

- 扫描器可能先于业务回调读到 [DONE]，需用真实扫描链路及 race 检查验证错误不会丢失。

## Acceptance

- 默认 60 秒、有效配置及无效输入回退均验证，真实 WebSocket 首帧读取期限生效。
- 业务失败后跟 [DONE]/EOF 仍记异常；缺少完成事件被识别，已有取消和超时原因保留。
- 正常完成、合法零 usage、预热和工具调用不误判；事件内容与计费结果保持。
- 相关测试、race、构建、vet 和 diff 检查通过。

## Next Step

- 确认本摘要后，执行 task.py start 激活本任务。
