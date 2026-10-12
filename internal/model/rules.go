package model

// BuiltinRule 内置动作规则（关键词可编辑、只可停用不可删除）
type BuiltinRule struct {
	Enabled  bool     `json:"enabled"`
	Keywords []string `json:"keywords"`
}

// CustomRule 自定义回复规则（关键词 → 固定内容回复）
type CustomRule struct {
	ID           string   `json:"id"`
	Keywords     []string `json:"keywords"`
	Reply        string   `json:"reply"`
	AppendFooter bool     `json:"append_footer,omitempty"` // 仅兼容旧配置；不再自动追加内容
	Enabled      bool     `json:"enabled"`
	CreatedAt    int64    `json:"created_at"`
}

// RulesConfig 关键词规则引擎配置（config/rules.json）
// 匹配优先级固定：六位数字（契约锁死）→ 内置规则 → 自定义规则 → 兜底回复
type RulesConfig struct {
	UniversalCode BuiltinRule  `json:"universal_code"` // 流向一：索取通行码
	Help          BuiltinRule  `json:"help"`           // 使用指南
	Projects      BuiltinRule  `json:"projects"`       // 项目清单
	Custom        []CustomRule `json:"custom"`         // 自定义规则
}

// SaveRulesReq 控制台整包保存规则请求
type SaveRulesReq struct {
	Rules RulesConfig `json:"rules"`
}

// TestRuleReq 规则试跑请求
type TestRuleReq struct {
	Message string `json:"message"`
}

// TestRuleResp 规则试跑响应：命中规则 + 实际回复文本
type TestRuleResp struct {
	Matched   string `json:"matched"`    // 命中的规则标识：verify_code/universal_code/help/projects/custom:<id>/fallback
	Reply     string `json:"reply"`      // 实际回复内容（不含 XML 包装）
	MatchedZh string `json:"matched_zh"` // 命中规则的中文名
}
