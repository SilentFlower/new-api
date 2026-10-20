package dto

// ChannelLimitParentInput 描述带版本校验的父渠道关联保存请求。
// @return 指针区分缺失字段与显式的零值；零父渠道表示解除关联。
type ChannelLimitParentInput struct {
	ExpectedRevision *int `json:"expected_revision"`
	ParentChannelID  *int `json:"parent_channel_id"`
}

// ChannelLimitParentSummary 提供继承来源所需的非敏感渠道摘要。
// @return 父渠道 ID 和名称。
type ChannelLimitParentSummary struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// ChannelLimitParentView 描述当前父渠道关系及继承项是否只读。
// @return 物理渠道身份、父关系版本和可选父摘要。
type ChannelLimitParentView struct {
	ConcurrencyLimit int                        `json:"concurrency_limit"`
	ChannelID        int                        `json:"channel_id"`
	ParentChannelID  int                        `json:"parent_channel_id"`
	Revision         int                        `json:"revision"`
	ReadOnly         bool                       `json:"read_only"`
	Parent           *ChannelLimitParentSummary `json:"parent,omitempty"`
}
