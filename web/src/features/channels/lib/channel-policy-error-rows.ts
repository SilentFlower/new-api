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
import type { ChannelBudgetRow } from '../period-types'

/**
 * 去掉后端 message 开头的哨兵前缀（如「渠道周期策略参数无效: 」），只留具体原因。
 * @param message 后端 message 原文。
 * @returns 具体原因；没有前缀时原样返回。
 */
export function channelPolicyErrorDetail(message: string): string {
  return message.replace(/^[^:：]*[:：]\s*/, '').trim()
}

/**
 * 从后端 400 文案中找出被点名的预算行。
 * 后端行级错误的格式为「<原因>：<行名>」「<行名> 的<原因>」或「<原因>：<行名> / <行名>」；先按分隔后的整段精确匹配，避免行名互为前缀时误标，没有命中时再退回子串匹配。
 * @param message 后端 message 原文。
 * @param rows 当前草稿行。
 * @returns 命中行的 id。
 */
export function matchChannelPolicyErrorRows(
  message: string,
  rows: Pick<ChannelBudgetRow, 'id' | 'name'>[]
): string[] {
  const named = rows
    .map((row) => ({ id: row.id, name: row.name.trim() }))
    .filter((row) => row.name)
  if (!message || named.length === 0) return []
  const tokens = new Set(
    message
      .split(/[:：]|\s\/\s|\s的/)
      .map((item) => item.trim())
      .filter(Boolean)
  )
  const exact = named.filter((row) => tokens.has(row.name))
  if (exact.length > 0) return exact.map((row) => row.id)
  return named.filter((row) => message.includes(row.name)).map((row) => row.id)
}
