package model

// CodeStatus 验证码核销状态
const (
	StatusPending  = "pending"
	StatusVerified = "verified"
	StatusUsed     = "used"    // 已被使用/已消费
	StatusExpired  = "expired" // 已过期
)

// 标准错误代号（供调用方做 switch 逻辑分支）
const (
	ErrTypeNotFound     = "code_not_found"
	ErrTypeExpired      = "code_expired"
	ErrTypeAlreadyUsed  = "code_already_used"
	ErrTypeUnauthorized = "unauthorized"
	ErrTypeInvalidParam = "invalid_param"
)

// CodeRecord 存储在 Redis 中的验证码实体信息
type CodeRecord struct {
	Code        string `json:"code"`
	Project     string `json:"project"`      // 项目唯一英文代号
	ProjectName string `json:"project_name"` // 项目中文展示名称
	Status      string `json:"status"`       // pending | verified | used
	OpenID      string `json:"openid,omitempty"`
	SessionTag  string `json:"session_tag,omitempty"` // 业务后端签发的会话标识（并发分桶与归属依据）
	IP          string `json:"ip,omitempty"`          // 发码时的用户 IP（X-Forwarded-For，审计与 IP 桶）
	PollToken   string `json:"poll_token,omitempty"`  // 轮询密钥（高熵随机，业务后端凭此查询状态）
	CreatedAt   int64  `json:"created_at"`
	VerifiedAt  int64  `json:"verified_at,omitempty"`
	Extra       string `json:"extra,omitempty"` // 兼容保留（旧字段名，新数据与 SessionTag 同值）
}

// CreateCodeReq 项目方请求生成验证码的参数
type CreateCodeReq struct {
	Project     string `json:"project"`      // 项目标识（如 "blog", "shop"）
	Key         string `json:"key"`          // 项目安全通信密钥（必须与后台配置一致）
	SessionTag  string `json:"session_tag"`  // 会话标识：业务后端签发的本次尝试/访客标识（准入契约要求）
	Extra       string `json:"extra"`        // 旧字段名（兼容别名，session_tag 为空时作为其值）
	ExpireSeconds int  `json:"expire_seconds"` // 自定义过期秒数（可选）
}

// CreateCodeResp 生成验证码的响应结构
type CreateCodeResp struct {
	Code          string `json:"code"`
	PollToken     string `json:"poll_token"`   // 轮询密钥：业务后端凭此查询核销状态（高熵随机，替代 6 位码查询）
	Project       string `json:"project"`      // 始终返回项目英文代号（用于机器比对）
	ProjectName   string `json:"project_name"` // 项目中文展示名称
	ExpireSeconds int    `json:"expire_seconds"`
}

// CheckCodeResp 查询核销状态的响应结构
type CheckCodeResp struct {
	Code        string `json:"code"`
	Project     string `json:"project"`      // 始终返回项目英文代号
	ProjectName string `json:"project_name"` // 项目中文展示名称
	Status      string `json:"status"`       // pending | verified | used | expired
	Verified    bool   `json:"verified"`
	OpenID      string `json:"openid,omitempty"`
	VerifiedAt  int64  `json:"verified_at,omitempty"`
	SessionTag  string `json:"session_tag,omitempty"` // 会话标识（与请求中的 session_tag 呼应）
	ErrorType   string `json:"error_type,omitempty"` // 当异常时携带机器错误代号
}

// VerifyUniversalReq 业务网站提交通用验证码进行核销的请求
type VerifyUniversalReq struct {
	Project string `json:"project"` // 核销该码的目标项目（如 "shop", "blog"）
	Key     string `json:"key"`     // 项目安全通信密钥（必须与后台配置一致）
	Code    string `json:"code"`    // 用户在微信中索取到的 6 位验证码
}

// VerifyUniversalResp 业务网站提交通用验证码核销的响应
type VerifyUniversalResp struct {
	Valid       bool   `json:"valid"`        // 是否核销成功
	OpenID      string `json:"openid"`       // 成功核销时绑定的微信用户 OpenID
	Project     string `json:"project"`      // 始终返回目标项目英文代号
	ProjectName string `json:"project_name"`  // 目标项目中文展示名称
	VerifiedAt  int64  `json:"verified_at"`  // 核销时间戳
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
