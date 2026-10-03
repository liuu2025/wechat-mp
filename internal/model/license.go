package model

// 激活码状态
const (
	LicenseUnused = "unused" // 未兑换
	LicenseUsed   = "used"   // 已兑换（烧毁）
	LicenseVoid   = "void"   // 管理员作废
)

// LicenseCode 项目激活码（卡密模型：兑换期 + 时长，一次性使用）
type LicenseCode struct {
	Code           string `json:"code"`                      // 码本身（XXXX-XXXX-XXXX）
	DurationDays   int    `json:"duration_days"`             // 兑换后项目可用的天数
	RedeemDeadline int64  `json:"redeem_deadline"`           // 兑换截止时间戳（逾期作废）
	Status         string `json:"status"`                    // unused | used | void
	UsedBy         string `json:"used_by,omitempty"`         // 兑换者账号ID
	UsedAt         int64  `json:"used_at,omitempty"`         // 兑换时间
	ProjectID      string `json:"project_id,omitempty"`      // 兑换到的项目（历史溯源，换绑场景为最后关联）
	CreatedAt      int64  `json:"created_at"`
}

// GenerateLicenseReq 管理员批量生成激活码请求
type GenerateLicenseReq struct {
	Count        int `json:"count"`          // 生成数量（1-100）
	DurationDays int `json:"duration_days"`  // 兑换后可用天数
	RedeemDays   int `json:"redeem_days"`    // 兑换期天数（生成后多少天内必须兑换）
}

// VoidLicenseReq 作废激活码请求
type VoidLicenseReq struct {
	Code string `json:"code"`
}

// RedeemProjectReq 租户创建/续期项目时的兑换请求体片段
type RedeemProjectReq struct {
	LicenseCode string `json:"license_code"` // 激活码
}
