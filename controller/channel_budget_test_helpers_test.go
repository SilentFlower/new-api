package controller

import appdto "github.com/QuantumNous/new-api/dto"

// budgetPoolDailyConfig 构造只含一条池子每日行的 v2 策略；targetChannelID > 0 时策略级动作为降级。
func budgetPoolDailyConfig(limit int64, targetChannelID int, targetModel string) appdto.ChannelPeriodPolicyConfig {
	config := appdto.ChannelPeriodPolicyConfig{
		SchemaVersion:   2,
		DefaultOnExceed: appdto.ChannelBudgetAction{Mode: "reject"},
		Schedules:       []appdto.ChannelBudgetSchedule{},
		Budgets:         []appdto.ChannelBudgetRow{{Name: "池子每日", Enabled: true, Scope: "pool", Window: "daily", Models: []string{}, Limit: limit, OnExceed: appdto.ChannelBudgetAction{Mode: "inherit"}}},
	}
	if targetChannelID > 0 {
		config.DefaultOnExceed = appdto.ChannelBudgetAction{Mode: "fallback", ChannelID: targetChannelID, Model: targetModel}
	}
	return config
}
