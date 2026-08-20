package dto

// AdminUserBillingSummaryRequest 描述管理员批量查询用户账务摘要的请求。
type AdminUserBillingSummaryRequest struct {
	UserIDs   []int  `json:"user_ids"`
	SortBy    string `json:"sort_by"`
	SortOrder string `json:"sort_order"`
}

// UserBillingWalletSummary 描述用户钱包额度摘要。
type UserBillingWalletSummary struct {
	Quota     int    `json:"quota"`
	UsedQuota int    `json:"used_quota"`
	Group     string `json:"group"`
}

// UserBillingSubscriptionItem 描述单个有效订阅的当前周期投影视图。
type UserBillingSubscriptionItem struct {
	SubscriptionID  int    `json:"subscription_id"`
	PlanID          int    `json:"plan_id"`
	PlanTitle       string `json:"plan_title"`
	Unlimited       bool   `json:"unlimited"`
	AmountTotal     int64  `json:"amount_total"`
	AmountUsed      int64  `json:"amount_used"`
	AmountRemaining *int64 `json:"amount_remaining,omitempty"`
	StartTime       int64  `json:"start_time"`
	EndTime         int64  `json:"end_time"`
	LastResetTime   int64  `json:"last_reset_time"`
	NextResetTime   int64  `json:"next_reset_time"`
}

// UserBillingSubscriptionSummary 描述用户全部有效订阅的聚合摘要。
type UserBillingSubscriptionSummary struct {
	ActiveCount     int                           `json:"active_count"`
	Unlimited       bool                          `json:"unlimited"`
	FiniteTotal     int64                         `json:"finite_total"`
	FiniteUsed      int64                         `json:"finite_used"`
	FiniteRemaining int64                         `json:"finite_remaining"`
	Items           []UserBillingSubscriptionItem `json:"items"`
}

// UserBillingSortKey 描述跨批次稳定排序使用的统一键。
type UserBillingSortKey struct {
	Kind  string `json:"kind"`
	Value *int64 `json:"value,omitempty"`
}

// AdminUserBillingSummaryItem 描述单个用户的账务摘要结果。
type AdminUserBillingSummaryItem struct {
	UserID       int                             `json:"user_id"`
	Status       string                          `json:"status"`
	ErrorCode    string                          `json:"error_code,omitempty"`
	RemoteStatus *int                            `json:"remote_status,omitempty"`
	RemoteRole   *int                            `json:"remote_role,omitempty"`
	Wallet       *UserBillingWalletSummary       `json:"wallet,omitempty"`
	Subscription *UserBillingSubscriptionSummary `json:"subscription,omitempty"`
	SortKey      UserBillingSortKey              `json:"sort_key"`
}

// AdminUserBillingSummaryResponse 描述管理员批量账务摘要的响应数据。
type AdminUserBillingSummaryResponse struct {
	SortBy    string                        `json:"sort_by"`
	SortOrder string                        `json:"sort_order"`
	Items     []AdminUserBillingSummaryItem `json:"items"`
}
