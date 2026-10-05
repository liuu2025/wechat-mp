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

// Account 平台账号（以微信公众号 openid 为唯一身份）
type Account struct {
	ID        string `json:"id"`                  // 账号ID（acc_ 前缀）
	OpenID    string `json:"openid"`              // 微信 openid（唯一身份，由微信服务器背书）
	Role      string `json:"role"`                // admin | tenant
	Status    string `json:"status"`              // active | disabled
	CreatedAt int64  `json:"created_at"`          // 注册时间
	LastLogin int64  `json:"last_login,omitempty"` // 最近登录时间
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

// SetAccountRoleReq 管理员调整账号角色请求
type SetAccountRoleReq struct {
	ID   string `json:"id"`
	Role string `json:"role"` // admin | tenant
}
