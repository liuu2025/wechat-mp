package model

// ReplySettings 是规则与完整回复的统一配置，不再包含公共尾注开关。
type ReplySettings struct {
	Version         int                            `json:"version"`
	Revision        string                         `json:"revision"`
	Rules           RulesConfig                    `json:"rules"`
	Replies         RepliesConfig                  `json:"replies"`
	SnippetSections map[string]ReplySnippetSection `json:"snippet_sections"`
}

// ReplySnippet 只在编辑时复制插入，保存/删除原片段不会影响已保存的回复。
type ReplySnippet struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

// ReplySnippetSelection 是插入回复后的独立副本。内容库后续编辑或删除不会静默改变已保存回复。
type ReplySnippetSelection struct {
	SourceID string `json:"source_id"`
	Title    string `json:"title"`
	Content  string `json:"content"`
}

// ReplySnippetSection 保存某个回复场景选中的常用内容和可编辑分区标题。
// map 键使用回复字段名；自定义规则使用 custom:<rule-id>。
type ReplySnippetSection struct {
	Title        string                  `json:"title"`
	Items        []ReplySnippetSelection `json:"items"`
	CompactTitle bool                    `json:"compact_title"` // 分区标题与第一项内容仅换行；旧配置默认空一行
}

type ReplyPreviewReq struct {
	ProjectDraft     *ProjectItem   `json:"project_draft,omitempty"` // 只读预览项目表单，绝不保存
	Draft            *ReplySettings `json:"draft,omitempty"`
	Field            string         `json:"field,omitempty"`
	Message          string         `json:"message,omitempty"`
	ProjectID        string         `json:"project_id,omitempty"`
	Code             string         `json:"code,omitempty"`
	UseLiveCodeState bool           `json:"use_live_code_state,omitempty"`
}

type ReplyPreviewResp struct {
	Matched       string `json:"matched"`
	MatchedZh     string `json:"matched_zh"`
	Reply         string `json:"reply"`
	LiveCodeState bool   `json:"live_code_state,omitempty"`
	CodeState     string `json:"code_state,omitempty"`
}

type DeleteReplySnippetReq struct {
	ID string `json:"id"`
}

type ReorderReplySnippetsReq struct {
	IDs []string `json:"ids"`
}
