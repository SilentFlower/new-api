package model

import "github.com/QuantumNous/new-api/common"

// MessageAuditReviewChannelOption 是审核设置页可见的精简渠道信息。
type MessageAuditReviewChannelOption struct {
	ID     int      `json:"id"`
	Name   string   `json:"name"`
	Models []string `json:"models" gorm:"-:all"`
}

// ListMessageAuditReviewChannelOptions 返回启用渠道及其配置模型，不包含密钥。
//
// @return 精简渠道列表和数据库查询错误。
func ListMessageAuditReviewChannelOptions() ([]MessageAuditReviewChannelOption, error) {
	var channels []Channel
	if err := DB.Select("id, name, models").Where("status = ?", common.ChannelStatusEnabled).Order("id asc").Find(&channels).Error; err != nil {
		return nil, err
	}
	options := make([]MessageAuditReviewChannelOption, 0, len(channels))
	for _, channel := range channels {
		options = append(options, MessageAuditReviewChannelOption{ID: channel.Id, Name: channel.Name, Models: channel.GetModels()})
	}
	return options, nil
}
