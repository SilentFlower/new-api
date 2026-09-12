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
/** 四个逐项覆盖的规则金额字段。 */
export const periodQuotaKeys = [
  'user_daily_quota_limit',
  'pool_daily_quota_limit',
  'user_period_quota_limit',
  'pool_period_quota_limit',
] as const
/** 时间规则表单与传输协议，null 继承、0 明确不限。 */
export const channelPeriodRuleSchema = z.object({
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
  user_daily_quota_limit: quota.nullable(),
  pool_daily_quota_limit: quota.nullable(),
  user_period_quota_limit: quota.nullable(),
  pool_period_quota_limit: quota.nullable(),
})
/** 独立配置协议，不向普通渠道更新回传策略。 */
export const channelPeriodConfigSchema = z.object({
  schema_version: z.literal(1),
  pool_daily_quota_limit: quota,
  pool_weekly_quota_limit: quota,
  rules: z.array(channelPeriodRuleSchema).max(64),
  fallback: z.object({
    enabled: z.boolean(),
    channel_id: z.number().int().nonnegative(),
    model: z.string(),
  }),
})
/** 周期规则。 */
export type ChannelPeriodRule = z.infer<typeof channelPeriodRuleSchema>
/** 周期策略。 */
export type ChannelPeriodConfig = z.infer<typeof channelPeriodConfigSchema>
/** 指标来源。 */
export interface ChannelPeriodSource {
  kind: 'default' | 'weekly' | 'date_range' | 'personal'
  rule_id?: string
  rule_name?: string
  start_at?: number
  end_at?: number
  expires_at?: number
}
/** 权威策略与只读预览。 */
export interface ChannelPeriodView {
  revision: number
  config: ChannelPeriodConfig
  timezone: string
  now: number
  limits?: Record<string, number>
  sources?: Record<string, ChannelPeriodSource>
  next_change_at?: number
}
/** 个人或池子当前计数。 */
export interface ChannelPeriodMetric {
  scope: 'user' | 'pool'
  period: 'daily' | 'weekly' | 'custom'
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
/** 安全降级目标选项。 */
export interface ChannelPeriodTarget {
  id: number
  name: string
  models: string[]
}
