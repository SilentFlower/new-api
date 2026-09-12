package dto

// ChannelPeriodPolicyConfigV1 保留 schema_version=1 的存储形状，仅供启动迁移与读取兜底转换使用。
type ChannelPeriodPolicyConfigV1 struct {
	SchemaVersion        int                   `json:"schema_version"`
	PoolDailyQuotaLimit  int64                 `json:"pool_daily_quota_limit"`
	PoolWeeklyQuotaLimit int64                 `json:"pool_weekly_quota_limit"`
	Rules                []ChannelPeriodRuleV1 `json:"rules"`
	Fallback             ChannelLimitFallback  `json:"fallback"`
}

// ChannelPeriodRuleV1 是 v1 的时间规则：一个时段附带四项额度。
type ChannelPeriodRuleV1 struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Enabled              bool   `json:"enabled"`
	Kind                 string `json:"kind"`
	StartLocal           string `json:"start_local"`
	EndLocal             string `json:"end_local"`
	StartAt              int64  `json:"start_at"`
	EndAt                int64  `json:"end_at"`
	StartWeekday         int    `json:"start_weekday"`
	EndWeekday           int    `json:"end_weekday"`
	StartTime            string `json:"start_time"`
	EndTime              string `json:"end_time"`
	CreatedAt            int64  `json:"created_at"`
	UserDailyQuotaLimit  *int64 `json:"user_daily_quota_limit"`
	PoolDailyQuotaLimit  *int64 `json:"pool_daily_quota_limit"`
	UserPeriodQuotaLimit *int64 `json:"user_period_quota_limit"`
	PoolPeriodQuotaLimit *int64 `json:"pool_period_quota_limit"`
}
