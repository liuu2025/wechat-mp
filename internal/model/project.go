package model

// ProjectItem 接入的独立业务项目实体
type ProjectItem struct {
	ID            string `json:"id"`                       // 项目唯一英文代号（如 "shop", "blog"）
	Name          string `json:"name"`                     // 项目中文名称（如 "商城系统"）
	URL           string `json:"url,omitempty"`            // 项目网站网址（选填，如 "https://shop.example.com"）
	Secret        string `json:"secret"`                   // 该项目对接时使用的安全密钥
	Enabled       bool   `json:"enabled"`                  // 是否启用该项目（true=允许对接，false=暂时停用）
	CustomAdTitle string `json:"custom_ad_title,omitempty"`// 项目专属尾注标题（选填，如：项目特惠、网盘须知）
	CustomAd      string `json:"custom_ad"`                // 该项目的专属定制尾注说明文案
	IncludeGlobal bool   `json:"include_global"`           // 是否同时追加系统默认尾注（true=两者叠加，false=仅项目尾注）
	CreatedAt     int64  `json:"created_at"`               // 创建时间戳
	SecretRotatedAt int64 `json:"secret_rotated_at,omitempty"` // 最近一次密钥轮换时间戳
	OwnerAccountID  string `json:"owner_account_id,omitempty"` // 归属账号ID（租户项目；管理员项目为空）
	ExpiresAt       int64  `json:"expires_at,omitempty"`       // 授权到期时间戳（0=永久）
}

// FooterItem 默认尾注模版实体
type FooterItem struct {
	ID      string `json:"id"`      // 模版唯一ID
	Title   string `json:"title"`   // 模版标题（如：日常推广、客服微信）
	Content string `json:"content"` // 尾注具体文案内容（支持多行）
	Active  bool   `json:"active"`  // 是否勾选启用
}

// AddFooterReq 新增尾注模版请求
type AddFooterReq struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Active  bool   `json:"active"`
}

// UpdateFooterReq 编辑尾注模版请求
type UpdateFooterReq struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Active  bool   `json:"active"`
}

// ToggleFooterReq 切换单个尾注勾选请求
type ToggleFooterReq struct {
	ID     string `json:"id"`
	Active bool   `json:"active"`
}

// SetFootersActiveReq 批量更新尾注勾选状态（单选或多选）
type SetFootersActiveReq struct {
	ActiveIDs []string `json:"active_ids"`
}

// DeleteFooterReq 删除尾注模版请求
type DeleteFooterReq struct {
	ID string `json:"id"`
}

// AuthConfig 控制台密码安全配置
type AuthConfig struct {
	Password  string `json:"password"`
	UpdatedAt int64  `json:"updated_at"`
}

// LoginReq 控制台验证码登录请求
type LoginReq struct {
	Code string `json:"code"` // 用户在公众号索取的 6 位通行码
}

// AddProjectReq 新增项目请求
type AddProjectReq struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	URL           string `json:"url"`
	CustomAdTitle string `json:"custom_ad_title"`
	CustomAd      string `json:"custom_ad"`
	IncludeGlobal bool   `json:"include_global"`
	LicenseCode   string `json:"license_code"` // 普通用户创建项目必须携带的有效激活码
}

// ToggleProjectReq 切换项目启停状态请求
type ToggleProjectReq struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

// UpdateProjectReq 编辑项目信息请求
type UpdateProjectReq struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	URL           string `json:"url"`
	CustomAdTitle string `json:"custom_ad_title"`
	CustomAd      string `json:"custom_ad"`
	IncludeGlobal bool   `json:"include_global"`
}

// DeleteProjectReq 删除项目请求
type DeleteProjectReq struct {
	ID string `json:"id"`
}

// RotateKeyReq 轮换项目密钥请求
type RotateKeyReq struct {
	ID string `json:"id"`
}

// RepliesConfig 公众号各场景回复文案模版配置
type RepliesConfig struct {
	Welcome       string `json:"welcome"`        // 新用户关注欢迎语
	CodeSuccess   string `json:"code_success"`   // 验证成功通知模版（支持 {project_name} 变量）
	CodeInvalid   string `json:"code_invalid"`   // 验证码不存在或已过期提示（支持 {code} 变量）
	CodeUsed      string `json:"code_used"`      // 验证码已核销过提示（支持 {code} 变量）
	UniversalCode string `json:"universal_code"` // 通用通行码下发模版（支持 {code} 变量）
	Projects      string `json:"projects"`       // 支持的业务项目清单模版（支持 {project_list} 变量）
	Help          string `json:"help"`           // 帮助指南指引
	UnknownText   string `json:"unknown_text"`   // 未识别的文字兜底回复
	MediaMessage  string `json:"media_message"`  // 收到图片/语音/表情包等非文本消息时的提示
}
