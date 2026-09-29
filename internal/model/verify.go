package model

// CodeStatus 验证码核销状态
const (
	StatusPending  = "pending"
	StatusVerified = "verified"
)

// CodeRecord 存储在 Redis 中的验证码实体信息
type CodeRecord struct {
	Code       string `json:"code"`
	Project    string `json:"project"`
	Status     string `json:"status"` // pending | verified
	OpenID     string `json:"openid,omitempty"`
	CreatedAt  int64  `json:"created_at"`
	VerifiedAt int64  `json:"verified_at,omitempty"`
	Extra      string `json:"extra,omitempty"` // 业务自传字段（如前端 session_id、用户临时ID等）
}

// CreateCodeReq 项目方请求生成验证码的参数
type CreateCodeReq struct {
	Project       string `json:"project"`        // 项目标识（如 "blog", "shop"）
	Extra         string `json:"extra"`          // 业务透传信息
	ExpireSeconds int    `json:"expire_seconds"` // 自定义过期秒数（可选）
}

// CreateCodeResp 生成验证码的响应结构
type CreateCodeResp struct {
	Code          string `json:"code"`
	Project       string `json:"project"`
	ExpireSeconds int    `json:"expire_seconds"`
}

// CheckCodeResp 查询核销状态的响应结构
type CheckCodeResp struct {
	Code       string `json:"code"`
	Project    string `json:"project"`
	Status     string `json:"status"`
	Verified   bool   `json:"verified"`
	OpenID     string `json:"openid,omitempty"`
	VerifiedAt int64  `json:"verified_at,omitempty"`
	Extra      string `json:"extra,omitempty"`
}

// VerifyUniversalReq 业务网站提交通用验证码进行核销的请求
type VerifyUniversalReq struct {
	Project string `json:"project"` // 核销该码的目标项目（如 "shop", "blog"）
	Code    string `json:"code"`    // 用户在微信中索取到的 6 位验证码
}

// VerifyUniversalResp 业务网站提交通用验证码核销的响应
type VerifyUniversalResp struct {
	Valid      bool   `json:"valid"`       // 是否核销成功
	OpenID     string `json:"openid"`      // 成功核销时绑定的微信用户 OpenID
	Project    string `json:"project"`     // 核销的目标项目
	VerifiedAt int64  `json:"verified_at"` // 核销时间戳
}

// APIResponse 通用 API JSON 返回包装
type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MenuButton 微信自定义菜单按钮结构
type MenuButton struct {
	Type      string        `json:"type"`                // click, view
	Name      string        `json:"name"`                // 菜单名称
	Key       string        `json:"key,omitempty"`       // click 类型附带的标识 key
	URL       string        `json:"url,omitempty"`       // view 类型跳转网址
	SubButton []*MenuButton `json:"sub_button,omitempty"`// 二级子菜单（最多 5 个）
}

// MenuConfig 微信菜单整体结构（最多 3 个一级菜单）
type MenuConfig struct {
	Button []*MenuButton `json:"button"`
}
