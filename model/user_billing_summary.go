package model

import "context"

// UserBillingSummaryRow 描述账务摘要所需的最小用户字段集合。
type UserBillingSummaryRow struct {
	ID        int    `json:"id"`
	Quota     int    `json:"quota"`
	UsedQuota int    `json:"used_quota"`
	Group     string `json:"group"`
	Status    int    `json:"status"`
	Role      int    `json:"role"`
}

// GetUserBillingSummaryRows 批量查询账务摘要所需的用户字段。
//
// @param ctx 请求上下文。
// @param userIDs 用户 ID 列表。
// @return []UserBillingSummaryRow 已存在且未软删除的用户摘要。
// @return error 数据库查询失败时返回错误。
func GetUserBillingSummaryRows(ctx context.Context, userIDs []int) ([]UserBillingSummaryRow, error) {
	if len(userIDs) == 0 {
		return []UserBillingSummaryRow{}, nil
	}
	var users []UserBillingSummaryRow
	err := DB.WithContext(ctx).
		Model(&User{}).
		Select([]string{"id", "quota", "used_quota", "group", "status", "role"}).
		Where("id IN ?", userIDs).
		Order("id asc").
		Find(&users).Error
	return users, err
}

// GetActiveUserSubscriptionsByUserIDs 批量查询指定用户在同一时点有效的订阅。
//
// @param ctx 请求上下文。
// @param userIDs 用户 ID 列表。
// @param now 数据库统一时间戳。
// @return []UserSubscription 有效订阅列表。
// @return error 数据库查询失败时返回错误。
func GetActiveUserSubscriptionsByUserIDs(ctx context.Context, userIDs []int, now int64) ([]UserSubscription, error) {
	if len(userIDs) == 0 {
		return []UserSubscription{}, nil
	}
	var subscriptions []UserSubscription
	err := DB.WithContext(ctx).
		Select([]string{
			"id", "user_id", "plan_id", "amount_total", "amount_used", "start_time", "end_time",
			"status", "last_reset_time", "next_reset_time",
		}).
		Where("user_id IN ? AND status = ? AND end_time > ?", userIDs, "active", now).
		Order("user_id asc, end_time asc, id asc").
		Find(&subscriptions).Error
	return subscriptions, err
}

// GetSubscriptionPlansForBillingSummary 批量查询账务摘要投影所需的套餐字段。
//
// @param ctx 请求上下文。
// @param planIDs 套餐 ID 列表。
// @return []SubscriptionPlan 套餐标题与周期重置配置。
// @return error 数据库查询失败时返回错误。
func GetSubscriptionPlansForBillingSummary(ctx context.Context, planIDs []int) ([]SubscriptionPlan, error) {
	if len(planIDs) == 0 {
		return []SubscriptionPlan{}, nil
	}
	var plans []SubscriptionPlan
	err := DB.WithContext(ctx).
		Select([]string{"id", "title", "quota_reset_period", "quota_reset_custom_seconds"}).
		Where("id IN ?", planIDs).
		Order("id asc").
		Find(&plans).Error
	return plans, err
}
