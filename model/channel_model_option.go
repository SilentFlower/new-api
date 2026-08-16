package model

import "github.com/QuantumNous/new-api/common"

// ChannelModelOption 是管理端选择模型时使用的精简渠道信息。
type ChannelModelOption struct {
	ID     int      `json:"id"`
	Name   string   `json:"name"`
	Models []string `json:"models" gorm:"-:all"`
}

// ListEnabledChannelModelOptions 返回启用渠道及其配置模型，不包含密钥。
//
// @return 精简渠道列表和数据库查询错误。
func ListEnabledChannelModelOptions() ([]ChannelModelOption, error) {
	var channels []Channel
	if err := DB.Select("id, name, models").Where("status = ?", common.ChannelStatusEnabled).Order("id asc").Find(&channels).Error; err != nil {
		return nil, err
	}
	options := make([]ChannelModelOption, 0, len(channels))
	for _, channel := range channels {
		options = append(options, ChannelModelOption{ID: channel.Id, Name: channel.Name, Models: channel.GetModels()})
	}
	return options, nil
}
