import type { MessageAuditCleanupTask, MessageAuditMessage } from '../types'

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
