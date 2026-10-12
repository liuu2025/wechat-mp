package model

// ReplySettings 是规则与完整回复的统一配置，不再包含公共尾注开关。
type ReplySettings struct {
	Version  int           `json:"version"`
	Revision string        `json:"revision"`
	Rules    RulesConfig   `json:"rules"`
	Replies  RepliesConfig `json:"replies"`
}

// ReplySnippet 只在编辑时复制插入，保存/删除原片段不会影响已保存的回复。
type ReplySnippet struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

type ReplyPreviewReq struct {
	Draft     *ReplySettings `json:"draft,omitempty"`
	Field     string         `json:"field,omitempty"`
	Message   string         `json:"message,omitempty"`
	ProjectID string         `json:"project_id,omitempty"`
	Code      string         `json:"code,omitempty"`
}

type ReplyPreviewResp struct {
	Matched   string `json:"matched"`
	MatchedZh string `json:"matched_zh"`
	Reply     string `json:"reply"`
}

type DeleteReplySnippetReq struct {
	ID string `json:"id"`
}
