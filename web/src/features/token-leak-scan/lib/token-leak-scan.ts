/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type {
  TokenLeakFinding,
  TokenLeakNotification,
  TokenLeakScanTask,
} from '../types'

/**
 * 判断泄露扫描任务是否仍需轮询。
 *
 * @param task 当前系统任务。
 * @returns pending 或 running 时返回 true。
 */
export function isTokenLeakScanTaskActive(
  task: TokenLeakScanTask | null
): boolean {
  return task?.status === 'pending' || task?.status === 'running'
}

/**
 * 将单 Token 扫描输入解析为严格的正整数 ID。
 *
 * @param value 输入框文本。
 * @returns 合法 ID；非法时返回 null。
 */
export function parseTokenLeakTokenID(value: string): number | null {
  const tokenID = Number(value.trim())
  return Number.isInteger(tokenID) && tokenID > 0 ? tokenID : null
}

/**
 * 将小时输入转换为表单数值，并阻止 NaN 进入表单状态。
 *
 * @param value 输入框原始文本。
 * @param valueAsNumber 浏览器解析的数值。
 * @returns 有限数值；空值或非法数值返回 0 以触发表单校验。
 */
export function parseTokenLeakIntervalInput(
  value: string,
  valueAsNumber: number
): number {
  if (value === '' || !Number.isFinite(valueAsNumber)) {
    return 0
  }
  return valueAsNumber
}

/**
 * 按通知渠道选出最新审计记录。
 *
 * @param notifications finding 的通知审计列表。
 * @returns 每个渠道 ID 最大的一条记录，按 ID 倒序排列。
 */
export function selectLatestTokenLeakNotificationsByChannel(
  notifications: TokenLeakNotification[]
): TokenLeakNotification[] {
  const latestByChannel = new Map<string, TokenLeakNotification>()
  for (const notification of notifications) {
    const existing = latestByChannel.get(notification.channel)
    if (!existing || notification.id > existing.id) {
      latestByChannel.set(notification.channel, notification)
    }
  }
  return [...latestByChannel.values()].sort((left, right) => right.id - left.id)
}

/**
 * 判断二次确认弹窗当前是否允许提交禁用操作。
 *
 * @param finding 当前确认目标。
 * @param pending 禁用请求是否正在提交。
 * @returns 仅开放 finding 且没有进行中请求时返回 true。
 */
export function canSubmitTokenLeakDisable(
  finding: TokenLeakFinding | null,
  pending: boolean
): boolean {
  return finding?.status === 'open' && !pending
}
