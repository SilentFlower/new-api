package dto

// ChannelPeriodPolicyConfig 描述渠道池子预算、时间规则及一次超限降级配置。
type ChannelPeriodPolicyConfig struct {
	SchemaVersion        int                  `json:"schema_version"`
	PoolDailyQuotaLimit  int64                `json:"pool_daily_quota_limit"`
	PoolWeeklyQuotaLimit int64                `json:"pool_weekly_quota_limit"`
	Rules                []ChannelPeriodRule  `json:"rules"`
	Fallback             ChannelLimitFallback `json:"fallback"`
}

// ChannelLimitFallback 描述当前渠道额度耗尽时唯一允许的目标。
type ChannelLimitFallback struct {
	Enabled   bool   `json:"enabled"`
	ChannelID int    `json:"channel_id"`
	Model     string `json:"model"`
}

// ChannelPeriodRule 描述半开时间区间；星期从周一的 0 到周日的 6。
type ChannelPeriodRule struct {
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

// ChannelPeriodPolicyInput 是带乐观锁的整份策略替换请求。
type ChannelPeriodPolicyInput struct {
	ExpectedRevision int                       `json:"expected_revision"`
	Config           ChannelPeriodPolicyConfig `json:"config"`
}

// ChannelPeriodPolicyView 返回权威配置、版本和解释输入时间的服务器时区。
type ChannelPeriodPolicyView struct {
	Revision int                       `json:"revision"`
	Config   ChannelPeriodPolicyConfig `json:"config"`
	Timezone string                    `json:"timezone"`
	Now      int64                     `json:"now"`
}

// ChannelPeriodSource 描述某个有效指标来自默认、时间规则还是个人覆盖。
type ChannelPeriodSource struct {
	Kind      string `json:"kind"`
	RuleID    string `json:"rule_id,omitempty"`
	RuleName  string `json:"rule_name,omitempty"`
	StartAt   int64  `json:"start_at,omitempty"`
	EndAt     int64  `json:"end_at,omitempty"`
	ExpiresAt int64  `json:"expires_at,omitempty"`
}

// ChannelPeriodMetric 是个人或池子当前周期的可解释额度状态。
type ChannelPeriodMetric struct {
	Scope         string              `json:"scope"`
	Period        string              `json:"period"`
	Limit         int64               `json:"limit"`
	Used          int64               `json:"used"`
	Remaining     *int64              `json:"remaining"`
	ResetAt       int64               `json:"reset_at"`
	TrackingSince int64               `json:"tracking_since"`
	Coverage      string              `json:"coverage"`
	Source        ChannelPeriodSource `json:"source"`
	Enforced      bool                `json:"enforced"`
	BaseLimit     int64               `json:"base_limit"`
	OverrideLimit *int64              `json:"override_limit,omitempty"`
}

// ChannelPeriodStatus 供管理端、本人门户与请求前检查共用。
type ChannelPeriodStatus struct {
	SchemaVersion   int                   `json:"schema_version"`
	Revision        int                   `json:"revision"`
	Timezone        string                `json:"timezone"`
	StorageMode     string                `json:"storage_mode"`
	NextChangeAt    int64                 `json:"next_change_at"`
	Metrics         []ChannelPeriodMetric `json:"metrics"`
	FallbackEnabled bool                  `json:"fallback_enabled"`
	Blocked         bool                  `json:"blocked"`
}

// ChannelUserPeriodOverrideInput 描述单个用户对一个规则的整段提额。
type ChannelUserPeriodOverrideInput struct {
	UserPeriodQuotaLimit int64 `json:"user_period_quota_limit"`
	ExpiresAt            int64 `json:"expires_at"`
}
