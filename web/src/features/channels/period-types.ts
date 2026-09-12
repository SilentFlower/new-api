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
import { z } from 'zod'

/** 周期额度上界，与后端 common.MaxPeriodQuota 一致；保持在 2^53 内以保证 JSON 精确往返。 */
export const MAX_PERIOD_QUOTA = 1_000_000_000_000_000
const quota = z.number().int().min(0).max(MAX_PERIOD_QUOTA)
/** 超限动作：inherit 沿用策略级默认，reject 拒绝，fallback 降级到目标。 */
export const channelBudgetActionSchema = z.object({
  mode: z.enum(['inherit', 'reject', 'fallback']),
  channel_id: z.number().int().nonnegative(),
  model: z.string(),
})
/** 可复用时段；新时段用 new- 前缀的临时 id 供同一次保存的行引用。 */
export const channelBudgetScheduleSchema = z.object({
  id: z.string(),
  name: z.string().trim().min(1).max(80),
  enabled: z.boolean(),
  kind: z.enum(['date_range', 'weekly']),
  start_local: z.string(),
  end_local: z.string(),
  start_at: z.number().int().nonnegative(),
  end_at: z.number().int().nonnegative(),
  start_weekday: z.number().int().min(0).max(6),
  end_weekday: z.number().int().min(0).max(6),
  start_time: z.string(),
  end_time: z.string(),
  created_at: z.number().int().nonnegative(),
})
/** 预算行：范围 × 周期 × 模型 × 上限 × 超限动作。 */
export const channelBudgetRowSchema = z.object({
  id: z.string(),
  name: z.string().trim().min(1).max(80),
  enabled: z.boolean(),
  scope: z.enum(['user', 'pool']),
  window: z.enum(['daily', 'weekly', 'occurrence']),
  schedule_id: z.string(),
  models: z.array(z.string().trim().min(1).max(255)).max(64),
  limit: quota,
  on_exceed: channelBudgetActionSchema,
  created_at: z.number().int().nonnegative(),
})
/** schema_version=2 的预算策略协议。 */
export const channelPeriodConfigSchema = z.object({
  schema_version: z.literal(2),
  default_on_exceed: channelBudgetActionSchema,
  schedules: z.array(channelBudgetScheduleSchema).max(64),
  budgets: z.array(channelBudgetRowSchema).max(128),
})
/** 超限动作。 */
export type ChannelBudgetAction = z.infer<typeof channelBudgetActionSchema>
/** 时段。 */
export type ChannelBudgetSchedule = z.infer<typeof channelBudgetScheduleSchema>
/** 预算行。 */
export type ChannelBudgetRow = z.infer<typeof channelBudgetRowSchema>
/** 预算策略。 */
export type ChannelPeriodConfig = z.infer<typeof channelPeriodConfigSchema>
/** 指标来源。 */
export interface ChannelPeriodSource {
  kind: 'default' | 'weekly' | 'date_range' | 'personal'
  schedule_id?: string
  schedule_name?: string
  start_at?: number
  end_at?: number
  expires_at?: number
}
/** 权威策略视图。 */
export interface ChannelPeriodView {
  revision: number
  config: ChannelPeriodConfig
  timezone: string
  now: number
}
/** 预览时每行的解析状态。 */
export interface ChannelBudgetPreviewRow {
  budget_id: string
  active: boolean
  enforced: boolean
  source: ChannelPeriodSource
}
/** 只读预览结果。 */
export interface ChannelBudgetPreview {
  config: ChannelPeriodConfig
  revision: number
  timezone: string
  now: number
  next_change_at: number
  rows: ChannelBudgetPreviewRow[]
}
/** 某行对当前用户的额度状态。 */
export interface ChannelPeriodMetric {
  budget_id: string
  budget_name: string
  scope: 'user' | 'pool'
  period: 'daily' | 'weekly' | 'occurrence'
  models: string[]
  limit: number
  used: number
  remaining: number | null
  reset_at: number
  tracking_since: number
  coverage: string
  source: ChannelPeriodSource
  enforced: boolean
  base_limit: number
  override_limit?: number
}
/** 统一周期状态。 */
export interface ChannelPeriodStatus {
  schema_version: number
  revision: number
  timezone: string
  storage_mode: 'memory' | 'redis'
  next_change_at: number
  metrics: ChannelPeriodMetric[]
  fallback_enabled: boolean
  blocked: boolean
}
/** 降级目标候选；self 表示本渠道，只能作为按模型行的同渠道换模型目标。 */
export interface ChannelPeriodTarget {
  id: number
  name: string
  models: string[]
  self: boolean
}
/** 某行当前窗口的用量视图。 */
export interface ChannelBudgetUsageView {
  channel_id: number
  budget_id: string
  scope: 'user' | 'pool'
  window_start: number
  window_end: number
  tracking_since: number
  storage_mode: 'memory' | 'redis'
  used_quota: number
  page: number
  page_size: number
  total: number
  items: {
    user_id: number
    username: string
    display_name: string
    used_quota: number
  }[]
}
/** 行级提额列表项。 */
export interface ChannelBudgetUserOverrideItem {
  user: { id: number; username: string; display_name: string }
  budget_id: string
  budget_name: string
  base_limit: number
  limit: number
  expires_at: number
}
