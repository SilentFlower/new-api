package model

// MessageAuditReviewChannelOption 是审核设置页可见的精简渠道信息。
type MessageAuditReviewChannelOption = ChannelModelOption

// ListMessageAuditReviewChannelOptions 返回启用渠道及其配置模型，不包含密钥。
//
// @return 精简渠道列表和数据库查询错误。
func ListMessageAuditReviewChannelOptions() ([]MessageAuditReviewChannelOption, error) {
	return ListEnabledChannelModelOptions()
}
