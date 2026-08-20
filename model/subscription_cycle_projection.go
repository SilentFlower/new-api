package model

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// MaxSubscriptionResetCustomSeconds 限制自定义重置周期，避免秒数在时间运算中溢出。
const MaxSubscriptionResetCustomSeconds = int64(math.MaxInt64 / int64(time.Second))

// ValidateSubscriptionResetConfiguration 校验订阅额度重置配置。
//
// @param period 重置周期类型。
// @param customSeconds 自定义周期秒数。
// @return error 配置无效时返回错误。
func ValidateSubscriptionResetConfiguration(period string, customSeconds int64) error {
	period = strings.TrimSpace(period)
	switch period {
	case "", SubscriptionResetNever, SubscriptionResetDaily, SubscriptionResetWeekly, SubscriptionResetMonthly:
		return nil
	case SubscriptionResetCustom:
		if customSeconds <= 0 || customSeconds > MaxSubscriptionResetCustomSeconds {
			return fmt.Errorf("invalid custom subscription reset seconds: %d", customSeconds)
		}
		return nil
	default:
		return fmt.Errorf("invalid subscription reset period: %s", period)
	}
}

// ProjectUserSubscriptionCycle 计算订阅在指定时点的当前周期只读视图。
//
// @param subscription 原始订阅快照。
// @param plan 订阅套餐及其重置配置。
// @param now 统一时间戳。
// @return UserSubscription 投影后的订阅快照。
// @return bool 投影是否改变了周期字段。
// @return error 重置配置不可靠时返回错误。
func ProjectUserSubscriptionCycle(subscription UserSubscription, plan SubscriptionPlan, now int64) (UserSubscription, bool, error) {
	if err := ValidateSubscriptionResetConfiguration(plan.QuotaResetPeriod, plan.QuotaResetCustomSeconds); err != nil {
		return subscription, false, err
	}
	return projectUserSubscriptionCycle(subscription, plan, now)
}

func projectUserSubscriptionCycle(subscription UserSubscription, plan SubscriptionPlan, now int64) (UserSubscription, bool, error) {
	if subscription.NextResetTime > 0 && subscription.NextResetTime > now {
		return subscription, false, nil
	}
	period := NormalizeResetPeriod(plan.QuotaResetPeriod)
	if period == SubscriptionResetNever {
		return subscription, false, nil
	}
	baseUnix := subscription.LastResetTime
	if baseUnix <= 0 {
		baseUnix = subscription.StartTime
	}
	if period == SubscriptionResetCustom {
		return projectCustomUserSubscriptionCycle(subscription, baseUnix, plan.QuotaResetCustomSeconds, now)
	}
	base := time.Unix(baseUnix, 0)
	next := calcNextResetTime(base, &plan, subscription.EndTime)
	if next > 0 && next <= baseUnix {
		return subscription, false, fmt.Errorf("subscription reset cycle did not advance")
	}
	advanced := false
	for next > 0 && next <= now {
		advanced = true
		previous := next
		base = time.Unix(next, 0)
		next = calcNextResetTime(base, &plan, subscription.EndTime)
		if next > 0 && next <= previous {
			return subscription, false, fmt.Errorf("subscription reset cycle did not advance")
		}
	}
	if !advanced {
		if subscription.NextResetTime == 0 && next > 0 {
			subscription.NextResetTime = next
			subscription.LastResetTime = base.Unix()
			return subscription, true, nil
		}
		return subscription, false, nil
	}
	subscription.AmountUsed = 0
	subscription.LastResetTime = base.Unix()
	subscription.NextResetTime = next
	return subscription, true, nil
}

func projectCustomUserSubscriptionCycle(subscription UserSubscription, baseUnix int64, periodSeconds int64, now int64) (UserSubscription, bool, error) {
	if baseUnix > math.MaxInt64-periodSeconds {
		return subscription, false, fmt.Errorf("custom subscription reset time overflow")
	}
	firstReset := baseUnix + periodSeconds
	if subscription.EndTime > 0 && firstReset > subscription.EndTime {
		return subscription, false, nil
	}
	if firstReset > now {
		if subscription.NextResetTime == 0 {
			subscription.LastResetTime = baseUnix
			subscription.NextResetTime = firstReset
			return subscription, true, nil
		}
		return subscription, false, nil
	}

	projectionEnd := now
	if subscription.EndTime > 0 && subscription.EndTime < projectionEnd {
		projectionEnd = subscription.EndTime
	}
	// 自定义周期可能累计数十亿次，直接按整周期数跳跃，避免逐周期循环阻塞批量摘要。
	if baseUnix < 0 && projectionEnd > math.MaxInt64+baseUnix {
		return subscription, false, fmt.Errorf("custom subscription reset range overflow")
	}
	elapsed := projectionEnd - baseUnix
	periods := elapsed / periodSeconds
	lastReset := baseUnix + periods*periodSeconds
	nextReset := int64(0)
	if lastReset <= math.MaxInt64-periodSeconds {
		candidate := lastReset + periodSeconds
		if subscription.EndTime <= 0 || candidate <= subscription.EndTime {
			nextReset = candidate
		}
	}

	subscription.AmountUsed = 0
	subscription.LastResetTime = lastReset
	subscription.NextResetTime = nextReset
	return subscription, true, nil
}
