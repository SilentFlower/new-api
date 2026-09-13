package dto

// ChannelBudgetAction 描述超限后的动作：inherit 沿用策略级默认，reject 拒绝，fallback 降级到目标。
type ChannelBudgetAction struct {
	Mode      string `json:"mode"`
	ChannelID int    `json:"channel_id"`
	Model     string `json:"model"`
}

// ChannelBudgetSchedule 描述可复用的半开时段；星期从周一的 0 到周日的 6。
type ChannelBudgetSchedule struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled"`
	Kind         string `json:"kind"`
	StartLocal   string `json:"start_local"`
	EndLocal     string `json:"end_local"`
	StartAt      int64  `json:"start_at"`
	EndAt        int64  `json:"end_at"`
	StartWeekday int    `json:"start_weekday"`
	EndWeekday   int    `json:"end_weekday"`
	StartTime    string `json:"start_time"`
	EndTime      string `json:"end_time"`
	CreatedAt    int64  `json:"created_at"`
}

// ChannelBudgetRow 是一条预算行：范围 × 周期 × 模型 × 上限 × 超限动作。
type ChannelBudgetRow struct {
	ID            string              `json:"id"`
	Name          string              `json:"name"`
	Enabled       bool                `json:"enabled"`
	Scope         string              `json:"scope"`
	Window        string              `json:"window"`
	ScheduleID    string              `json:"schedule_id"`
	Models        []string            `json:"models"`
	Limit         int64               `json:"limit"`
	OnExceed      ChannelBudgetAction `json:"on_exceed"`
	CreatedAt     int64               `json:"created_at"`
	CounterSource string              `json:"counter_source,omitempty"`
}

// ChannelPeriodPolicyConfig 是 schema_version=2 的渠道预算策略：时段列表、预算行与策略级默认动作。
type ChannelPeriodPolicyConfig struct {
	SchemaVersion             int                        `json:"schema_version"`
	ModelUsageTrackingEnabled *bool                      `json:"model_usage_tracking_enabled,omitempty"`
	ModelUsageTracking        *ChannelModelUsageTracking `json:"model_usage_tracking,omitempty"`
	DefaultOnExceed           ChannelBudgetAction        `json:"default_on_exceed"`
	Schedules                 []ChannelBudgetSchedule    `json:"schedules"`
	Budgets                   []ChannelBudgetRow         `json:"budgets"`
}

// ChannelModelUsageTracking 保存服务端维护的累计覆盖边界，客户端只能原样往返。
// @param FirstEnabledAt 首次开启时间；EnabledAt、DisabledAt 为最近切换时间。
// @return 渠道模型累计历史元数据。
type ChannelModelUsageTracking struct {
	FirstEnabledAt int64 `json:"first_enabled_at"`
	EnabledAt      int64 `json:"enabled_at"`
	DisabledAt     int64 `json:"disabled_at"`
}

// ChannelLimitFallback 描述解析后的唯一降级目标。
type ChannelLimitFallback struct {
	Enabled   bool   `json:"enabled"`
	ChannelID int    `json:"channel_id"`
	Model     string `json:"model"`
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

// ChannelBudgetPreviewRow 描述预览时刻某行是否处于时段内、是否为分组生效行及其来源。
type ChannelBudgetPreviewRow struct {
	BudgetID string              `json:"budget_id"`
	Active   bool                `json:"active"`
	Enforced bool                `json:"enforced"`
	Source   ChannelPeriodSource `json:"source"`
}

// ChannelBudgetPreview 是只读预览结果：归一化配置与每行在预览时刻的解析状态。
type ChannelBudgetPreview struct {
	Config       ChannelPeriodPolicyConfig `json:"config"`
	Revision     int                       `json:"revision"`
	Timezone     string                    `json:"timezone"`
	Now          int64                     `json:"now"`
	NextChangeAt int64                     `json:"next_change_at"`
	Rows         []ChannelBudgetPreviewRow `json:"rows"`
}

// ChannelPeriodSource 描述某个有效指标来自默认、时段还是个人覆盖。
type ChannelPeriodSource struct {
	Kind         string `json:"kind"`
	ScheduleID   string `json:"schedule_id,omitempty"`
	ScheduleName string `json:"schedule_name,omitempty"`
	StartAt      int64  `json:"start_at,omitempty"`
	EndAt        int64  `json:"end_at,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
}

// ChannelPeriodMetric 是某条预算行在当前窗口对当前用户的可解释额度状态。
type ChannelPeriodMetric struct {
	BudgetID      string              `json:"budget_id"`
	BudgetName    string              `json:"budget_name"`
	Scope         string              `json:"scope"`
	Period        string              `json:"period"`
	Models        []string            `json:"models"`
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

// ChannelBudgetUserOverrideInput 描述单个用户对一条个人预算行的提额。
type ChannelBudgetUserOverrideInput struct {
	Limit     int64 `json:"limit"`
	ExpiresAt int64 `json:"expires_at"`
}

// ChannelBudgetUsageInput 描述直接设置某行当前窗口已用额度的请求；scope=user 时须给出 user_id。
type ChannelBudgetUsageInput struct {
	Scope     string `json:"scope"`
	UserID    int    `json:"user_id"`
	UsedQuota int64  `json:"used_quota"`
}

// ChannelBudgetUsageItem 是某行当前窗口内一个用户的已用额度。
type ChannelBudgetUsageItem struct {
	UserID      int    `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	UsedQuota   int64  `json:"used_quota"`
}

// ChannelBudgetUsageView 是某行当前窗口的用量视图：池子汇总或分页的用户列表。
type ChannelBudgetUsageView struct {
	ChannelID     int                      `json:"channel_id"`
	BudgetID      string                   `json:"budget_id"`
	Scope         string                   `json:"scope"`
	WindowStart   int64                    `json:"window_start"`
	WindowEnd     int64                    `json:"window_end"`
	TrackingSince int64                    `json:"tracking_since"`
	StorageMode   string                   `json:"storage_mode"`
	UsedQuota     int64                    `json:"used_quota"`
	Page          int                      `json:"page"`
	PageSize      int                      `json:"page_size"`
	Total         int                      `json:"total"`
	Items         []ChannelBudgetUsageItem `json:"items"`
}

// ChannelBudgetUsageTopUser 是个人预算行当前窗口内用量最高的用户及其生效上限。
type ChannelBudgetUsageTopUser struct {
	UserID         int    `json:"user_id"`
	Username       string `json:"username"`
	DisplayName    string `json:"display_name"`
	UsedQuota      int64  `json:"used_quota"`
	EffectiveLimit int64  `json:"effective_limit"`
	Override       bool   `json:"override"`
}

// ChannelBudgetUsageSummaryItem 是一条已保存预算行的当前窗口用量摘要。
// 池子行 UsedQuota 为池子汇总；个人行 UsedQuota 为用量最高用户的已用额度，PoolUsedQuota 为同计数身份的池子汇总。
type ChannelBudgetUsageSummaryItem struct {
	BudgetID      string                     `json:"budget_id"`
	Scope         string                     `json:"scope"`
	WindowStart   int64                      `json:"window_start"`
	WindowEnd     int64                      `json:"window_end"`
	TrackingSince int64                      `json:"tracking_since"`
	UsedQuota     int64                      `json:"used_quota"`
	PoolUsedQuota int64                      `json:"pool_used_quota"`
	TopUser       *ChannelBudgetUsageTopUser `json:"top_user,omitempty"`
}

// ChannelBudgetUsageSummaryView 一次返回渠道全部已保存预算行的当前窗口用量，供预算表与用量页签单次加载。
type ChannelBudgetUsageSummaryView struct {
	ChannelID   int                             `json:"channel_id"`
	Revision    int                             `json:"revision"`
	StorageMode string                          `json:"storage_mode"`
	Now         int64                           `json:"now"`
	Items       []ChannelBudgetUsageSummaryItem `json:"items"`
}
