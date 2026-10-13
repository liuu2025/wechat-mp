package model

// RegisteredUser 是永久编号表的只读视图，不代表已创建平台账号。
type RegisteredUser struct {
	OpenID      string `json:"openid"`
	UserNumber  int64  `json:"user_number"`
	FirstSeenAt int64  `json:"first_seen_at"`
	Kind        string `json:"kind"` // mock 为已知仿真前缀；ordinary 不表示已验证真实来源
	Registered  bool   `json:"registered"`
	Role        string `json:"role,omitempty"`
	Status      string `json:"status,omitempty"`
}

type RegisteredUserList struct {
	Items        []RegisteredUser `json:"items"`
	Total        int              `json:"total"`
	Page         int              `json:"page"`
	PageSize     int              `json:"page_size"`
	KnownCount   int              `json:"known_count"`
	AccountCount int              `json:"account_count"`
	MockCount    int              `json:"mock_count"`
	NextNumber   int64            `json:"next_number"`
}
