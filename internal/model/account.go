package model

// 账号角色
const (
	RoleAdmin  = "admin"
	RoleTenant = "tenant"
)

// 账号状态
const (
	AccountActive   = "active"
	AccountDisabled = "disabled"
)

// 平台账号注册模式
const (
	RegistrationOpen   = "open"
	RegistrationClosed = "closed"
)

// Account 平台账号（以微信公众号 openid 为唯一身份）
type Account struct {
	ID        string `json:"id"`                   // 账号ID（acc_ 前缀）
	OpenID    string `json:"openid"`               // 微信 openid（唯一身份，由微信服务器背书）
	Role      string `json:"role"`                 // admin | tenant
	Status    string `json:"status"`               // active | disabled
	CreatedAt int64  `json:"created_at"`           // 注册时间
	LastLogin int64  `json:"last_login,omitempty"` // 最近登录时间
}

// DevAccountOption 本地开发登录页可选择的已有账号。
// 仅返回账号 ID 与脱敏 OpenID，避免开发页面暴露完整微信身份标识。
type DevAccountOption struct {
	ID           string `json:"id"`
	MaskedOpenID string `json:"masked_openid"`
	Role         string `json:"role"`
}

// PlatformSettings 平台运行时业务设置（持久化于 config/platform_settings.json）。
type PlatformSettings struct {
	RegistrationMode string `json:"registration_mode"`
	UpdatedAt        int64  `json:"updated_at,omitempty"`
	UpdatedBy        string `json:"updated_by,omitempty"`
}

// SetRegistrationModeReq 管理员修改账号注册模式。
type SetRegistrationModeReq struct {
	Mode string `json:"mode"`
}

// SessionInfo 登录会话（存于 Redis，7 天有效）
type SessionInfo struct {
	Token     string `json:"token"`
	AccountID string `json:"account_id"`
	OpenID    string `json:"openid"`
	Role      string `json:"role"`
	IsNew     bool   `json:"is_new,omitempty"` // 本次登录是否新建了账号（不落 Redis，仅登录响应携带）
}

// SetAccountStatusReq 管理员启停账号请求
type SetAccountStatusReq struct {
	ID     string `json:"id"`
	Status string `json:"status"` // active | disabled
}
