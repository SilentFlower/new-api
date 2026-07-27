import type { MessageAuditCleanupTask } from '../types'

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
