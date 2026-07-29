import type {
  MessageAuditCleanupTask,
  MessageAuditMessage,
  MessageAuditReview,
  MessageAuditReviewStatus,
  MessageAuditRiskLevel,
} from '../types'

export const MESSAGE_AUDIT_CLEAR_CONFIRMATION = 'CLEAR'

/**
 * 判断消息审计清理任务是否仍在执行。
 *
 * @param task 当前清理任务。
 * @returns pending 或 running 时返回 true。
 */
export function isMessageAuditCleanupActive(
  task: MessageAuditCleanupTask | null
): boolean {
  return task?.status === 'pending' || task?.status === 'running'
}

/**
 * 判断危险清空操作的确认文本是否完全匹配。
 *
 * @param value 管理员输入的确认文本。
 * @returns 仅精确输入 CLEAR 时返回 true。
 */
export function isMessageAuditClearConfirmed(value: string): boolean {
  return value === MESSAGE_AUDIT_CLEAR_CONFIRMATION
}

/**
 * 返回可安全传给进度条的清理进度。
 *
 * @param task 当前清理任务。
 * @returns 0 到 100 之间的整数或小数。
 */
export function getMessageAuditCleanupProgress(
  task: MessageAuditCleanupTask | null
): number {
  const progress = task?.state?.progress
  if (typeof progress !== 'number' || !Number.isFinite(progress)) return 0
  return Math.min(100, Math.max(0, progress))
}

/**
 * 返回清理任务状态对应的 i18n 文案键。
 *
 * @param task 当前清理任务。
 * @returns 进行中、完成或失败状态对应的英文源键。
 */
export function getMessageAuditCleanupTitleKey(
  task: MessageAuditCleanupTask
): string {
  if (isMessageAuditCleanupActive(task)) return 'Clearing message audits...'
  if (task.status === 'succeeded') return 'Completed'
  return 'Cleanup failed'
}

/**
 * 从未知错误中提取可展示信息。
 *
 * @param error React Query 或操作回调捕获的未知错误。
 * @param fallback 无明确错误信息时使用的回退文案。
 * @returns 可安全展示的错误文本。
 */
export function getMessageAuditErrorMessage(
  error: unknown,
  fallback: string
): string {
  return error instanceof Error && error.message ? error.message : fallback
}

/**
 * 返回会话续接方式对应的 i18n 文案键。
 *
 * @param match 服务端记录的会话续接方式。
 * @returns 精确、前缀、压缩或新会话对应的英文源键。
 */
export function getMessageAuditSessionMatchLabelKey(match: string): string {
  switch (match) {
    case 'exact':
      return 'Exact history match'
    case 'prefix':
      return 'History continuation'
    case 'compressed':
      return 'Compressed continuation'
    default:
      return 'New inferred session'
  }
}

/**
 * 返回 AI 审核状态对应的 i18n 文案键。
 *
 * @param status 服务端审核状态。
 * @returns 未审核、等待、执行、失败或成功对应的英文源键。
 */
export function getMessageAuditReviewStatusLabelKey(
  status: MessageAuditReviewStatus
): string {
  switch (status) {
    case 'pending':
      return 'Waiting for AI review'
    case 'running':
      return 'AI review in progress'
    case 'failed':
      return 'AI review failed'
    case 'succeeded':
      return 'AI review completed'
    default:
      return 'Not reviewed'
  }
}

/**
 * 返回风险等级对应的 i18n 文案键。
 *
 * @param riskLevel 审核风险等级。
 * @returns 风险等级英文源键。
 */
export function getMessageAuditRiskLabelKey(
  riskLevel: MessageAuditRiskLevel | ''
): string {
  switch (riskLevel) {
    case 'high':
      return 'High risk'
    case 'medium':
      return 'Medium risk'
    case 'low':
      return 'Low risk'
    case 'none':
      return 'No risk found'
    default:
      return 'Risk unknown'
  }
}

/**
 * 返回稳定 AI 审核失败码对应的安全说明文案键。
 *
 * @param code 服务端保存的稳定失败码。
 * @returns 不包含上游响应正文的英文源键。
 */
export function getMessageAuditReviewFailureLabelKey(code: string): string {
  switch (code) {
    case 'config_invalid':
      return 'The configured review channel or model is no longer available.'
    case 'caller_unavailable':
      return 'The internal review caller is unavailable on this node.'
    case 'tool_unsupported':
      return 'The selected model did not use the required audit tools.'
    case 'tool_call_limit':
      return 'The review stopped after reaching the configured Tool call limit.'
    case 'tool_token_limit':
    case 'context_limit':
      return 'The review stopped after reaching its protected context limit.'
    case 'content_unavailable':
    case 'source_expired':
      return 'Some fixed audit source content is no longer available.'
    case 'invalid_output':
      return 'The selected model returned an invalid review result.'
    case 'upstream_failed':
      return 'The selected review channel request failed.'
    default:
      return 'The AI review could not be completed.'
  }
}

/**
 * 返回审核模型调用阶段对应的 i18n 文案键。
 *
 * @param phase 服务端记录的调用阶段。
 * @returns 常规审核或格式修复对应的英文源键。
 */
export function getMessageAuditReviewCallPhaseLabelKey(phase: string): string {
  return phase === 'format_repair' ? 'Format repair' : 'Review pass'
}

/**
 * 返回审核模型调用协议对应的 i18n 文案键。
 *
 * @param protocol 服务端记录的工具协议。
 * @returns 原生 Tool 或文本回退对应的英文源键。
 */
export function getMessageAuditReviewProtocolLabelKey(
  protocol: string
): string {
  switch (protocol) {
    case 'merged_context':
      return 'Merged context'
    case 'text_tool_fallback':
      return 'Text Tool fallback'
    default:
      return 'Native Tool calls'
  }
}

/**
 * 返回审核上下文模式对应的 i18n 文案键。
 *
 * @param mode 服务端记录的审核上下文模式。
 * @returns 合并上下文或 Tool 读取对应的英文源键。
 */
export function getMessageAuditReviewContextModeLabelKey(mode: string): string {
  return mode === 'tool' ? 'Model Tool reading' : 'Merged context'
}

/**
 * 返回审核模型调用结果对应的 i18n 文案键。
 *
 * @param outcome 服务端记录的调用结果。
 * @returns 调用结果对应的英文源键。
 */
export function getMessageAuditReviewCallOutcomeLabelKey(
  outcome: string
): string {
  switch (outcome) {
    case 'tool_calls':
      return 'Tools requested'
    case 'final':
      return 'Final result returned'
    case 'fallback':
      return 'Switched to text Tool fallback'
    case 'failed':
      return 'Model call failed'
    default:
      return 'Model call completed'
  }
}

/**
 * 返回脱敏失败阶段对应的 i18n 文案键。
 *
 * @param stage relay 返回的稳定失败阶段。
 * @returns 可供管理员理解的阶段英文源键。
 */
export function getMessageAuditReviewErrorStageLabelKey(stage: string): string {
  switch (stage) {
    case 'channel_lookup':
    case 'channel_config':
      return 'Review channel configuration'
    case 'channel_setup':
    case 'model_mapping':
    case 'adaptor_unavailable':
      return 'Review channel setup'
    case 'request_conversion':
    case 'request_serialization':
    case 'request_filtering':
      return 'Review request preparation'
    case 'upstream_request':
      return 'Upstream connection'
    case 'upstream_response':
    case 'upstream_http':
      return 'Upstream response'
    case 'response_conversion':
    case 'response_parse':
      return 'Response parsing'
    case 'tool_ignored':
      return 'Native Tool support'
    default:
      return 'Unknown stage'
  }
}

/**
 * 返回普通消息审计失败码对应的本地化安全说明。
 *
 * @param code Relay 持久化的稳定错误码。
 * @returns 不包含上游错误正文的英文源键。
 */
export function getMessageAuditRequestFailureLabelKey(code: string): string {
  switch (code) {
    case 'invalid_request':
    case 'read_request_body_failed':
    case 'convert_request_failed':
    case 'bad_request_body':
      return 'The request was rejected before it could be sent upstream.'
    case 'sensitive_words_detected':
    case 'prompt_blocked':
      return 'The request was blocked by a configured safety policy.'
    case 'channel:no_available_key':
    case 'get_channel_failed':
    case 'channel:response_time_exceeded':
      return 'No available channel could process the request.'
    case 'channel_user_concurrency_exceeded':
    case 'channel_user_concurrency_unavailable':
      return 'The channel concurrency limit prevented this request.'
    case 'insufficient_user_quota':
    case 'pre_consume_token_quota_failed':
      return 'The account had insufficient quota for this request.'
    case 'do_request_failed':
    case 'read_response_body_failed':
    case 'bad_response_status_code':
    case 'bad_response':
    case 'bad_response_body':
    case 'empty_response':
    case 'aws_invoke_error':
      return 'The upstream request failed or returned an invalid response.'
    case 'model_not_found':
    case 'model_price_error':
    case 'channel:model_mapped_error':
    case 'channel:param_override_invalid':
    case 'channel:header_override_invalid':
      return 'The requested model or channel configuration was unavailable.'
    case 'access_denied':
      return 'Access to the requested operation was denied.'
    case 'count_token_failed':
    case 'gen_relay_info_failed':
    case 'invalid_api_type':
    case 'json_marshal_failed':
    case 'query_data_error':
    case 'update_data_error':
      return 'An internal gateway error prevented the request from completing.'
    default:
      return 'An unknown failure occurred. Review the error code for details.'
  }
}

/**
 * 返回审核资料未完全读取原因对应的本地化说明。
 *
 * @param reason 服务端生成的稳定未覆盖原因。
 * @returns 未读取、部分读取或正文不可用对应的英文源键。
 */
export function getMessageAuditReviewUncoveredLabelKey(reason: string): string {
  switch (reason) {
    case 'not_read':
      return 'This source was not read.'
    case 'partially_read':
      return 'This source was only partially read.'
    case 'content_unavailable':
      return 'The source content was unavailable.'
    default:
      return 'This source was not fully reviewed.'
  }
}

/**
 * 返回 AI 审核查询需要使用的轮询间隔。
 *
 * @param review 当前审核响应。
 * @returns 排队或运行时返回 1000 毫秒，其余状态关闭轮询。
 */
export function getMessageAuditReviewPollInterval(
  review: MessageAuditReview | undefined
): number | false {
  return review?.status === 'pending' || review?.status === 'running'
    ? 1000
    : false
}

/**
 * 返回稳定审核分类对应的 i18n 文案键。
 *
 * @param category 服务端审核分类枚举。
 * @returns 分类英文源键。
 */
export function getMessageAuditReviewCategoryLabelKey(
  category: string
): string {
  const labels: Record<string, string> = {
    prompt_injection: 'Prompt injection',
    sensitive_information: 'Sensitive information',
    network_abuse: 'Network abuse',
    fraud_illegal: 'Fraud or illegal activity',
    violence_self_harm: 'Violence or self-harm',
    sexual_content: 'Sexual content',
    hate_harassment: 'Hate or harassment',
    policy_evasion: 'Policy evasion',
    other: 'Other risk',
  }
  return labels[category] ?? category
}

/**
 * 返回审计正文保存模式对应的 i18n 文案键。
 *
 * @param auditStatus 服务端审计保存状态。
 * @returns 完整正文、仅用户输入或仅元数据对应的英文源键。
 */
export function getMessageAuditStorageModeLabelKey(
  auditStatus: string
): string {
  switch (auditStatus) {
    case 'content_reduced':
      return 'User input only'
    case 'metadata_only':
      return 'Metadata only'
    default:
      return 'Full captured content'
  }
}

/**
 * 按角色和内容类型过滤详情消息，并保持服务端返回顺序。
 *
 * @param messages 服务端按 sequence 排列的消息。
 * @param hiddenRoles 当前隐藏的角色。
 * @param hiddenContentTypes 当前隐藏的内容类型。
 * @returns 保持原始顺序的可见消息。
 */
export function filterMessageAuditMessages(
  messages: MessageAuditMessage[],
  hiddenRoles: string[],
  hiddenContentTypes: string[]
): MessageAuditMessage[] {
  return messages.filter(
    (message) =>
      !hiddenRoles.includes(message.role) &&
      !hiddenContentTypes.includes(message.content_type)
  )
}

/**
 * 仅在同一推断会话翻页时复用上一页数据。
 *
 * @param previousData 上一次查询成功返回的数据。
 * @param previousSessionId 上一次查询键中的推断会话 ID。
 * @param currentSessionId 当前准备查询的推断会话 ID。
 * @returns 会话一致时返回旧数据，切换会话时返回 undefined。
 */
export function keepMessageAuditSessionPlaceholder<T>(
  previousData: T | undefined,
  previousSessionId: unknown,
  currentSessionId: string | null
): T | undefined {
  if (!currentSessionId || previousSessionId !== currentSessionId) {
    return undefined
  }
  return previousData
}
