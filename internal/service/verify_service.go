package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"time"

	"wechat-mp/internal/config"
	"wechat-mp/internal/model"
	"wechat-mp/internal/storage"
)

type VerifyService struct {
	cfg            *config.Config
	storage        *storage.Storage
	projectService *ProjectService
}

func NewVerifyService(cfg *config.Config, store *storage.Storage, ps *ProjectService) *VerifyService {
	return &VerifyService{
		cfg:            cfg,
		storage:        store,
		projectService: ps,
	}
}

// GenerateCode 为业务项目生成唯一的 6 位数字验证码（集成三道防线限流与配额保护）
func (s *VerifyService) GenerateCode(ctx context.Context, req *model.CreateCodeReq, clientIP string) (*model.CreateCodeResp, error) {
	projectSlug := strings.TrimSpace(req.Project)
	if projectSlug == "" {
		return nil, fmt.Errorf("field 'project' is required")
	}

	projectName := projectSlug
	// 严格白名单与密钥鉴权校验
	if s.projectService != nil {
		p, err := s.projectService.GetProject(projectSlug)
		if err != nil {
			return nil, fmt.Errorf("unauthorized: 项目 '%s' 未在白名单中登记", projectSlug)
		}
		if !p.Enabled {
			return nil, fmt.Errorf("unauthorized: 项目 '%s' 已被管理员停用", p.Name)
		}
		// 校验通信密钥 Key
		if strings.TrimSpace(req.Key) == "" || strings.TrimSpace(req.Key) != p.Secret {
			return nil, fmt.Errorf("unauthorized: 项目通信密钥（Key）不匹配或未提供")
		}
		projectSlug = p.ID   // 始终保持标准英文标识
		projectName = p.Name // 项目中文展示名称
	}

	// ---------------- 第一道防线：单 IP 频控（2秒内最多申请 2 次，防机器疯狂连点） ----------------
	if clientIP != "" {
		allowed, err := s.storage.AllowIPCheck(ctx, clientIP, 2, 2*time.Second)
		if err == nil && !allowed {
			return nil, fmt.Errorf("rate_limit: 请求过于频繁，请稍候再试")
		}
	}

	// ---------------- 第二道防线：单项目总速率熔断（每秒最多允许 15 次，防死循环刷量） ----------------
	allowedRate, err := s.storage.AllowProjectRateCheck(ctx, projectSlug, 15)
	if err == nil && !allowedRate {
		return nil, fmt.Errorf("rate_limit: 当前项目请求并发已达峰值，请稍后再试")
	}

	// ---------------- 第三道防线：单项目活跃码池硬顶（每个项目同时最多 300 个未核销活跃码） ----------------
	allowedQuota, err := s.storage.CheckAndIncrProjectActiveCodes(ctx, projectSlug, 300)
	if err == nil && !allowedQuota {
		return nil, fmt.Errorf("quota_limit: 当前项目等待核销的验证码过多，请等待现有验证码核销或过期后再试")
	}

	expireSec := req.ExpireSeconds
	if expireSec <= 0 {
		expireSec = s.cfg.DefaultCodeExpire
	}

	// 保证生成的 6 位数字在当前活跃池中唯一（防碰撞重试）
	var code string
	for i := 0; i < 5; i++ {
		c, err := randomDigits(6)
		if err != nil {
			s.storage.DecrProjectActiveCodes(ctx, projectSlug)
			return nil, err
		}
		exists, err := s.storage.Exists(ctx, c)
		if err != nil {
			s.storage.DecrProjectActiveCodes(ctx, projectSlug)
			return nil, err
		}
		if !exists {
			code = c
			break
		}
	}
	if code == "" {
		s.storage.DecrProjectActiveCodes(ctx, projectSlug)
		return nil, fmt.Errorf("system busy, failed to generate unique code")
	}

	record := &model.CodeRecord{
		Code:        code,
		Project:     projectSlug,
		ProjectName: projectName,
		Status:      model.StatusPending,
		CreatedAt:   time.Now().Unix(),
		Extra:       req.Extra,
	}

	ttl := time.Duration(expireSec) * time.Second
	if err := s.storage.SaveCode(ctx, record, ttl); err != nil {
		s.storage.DecrProjectActiveCodes(ctx, projectSlug)
		return nil, err
	}

	return &model.CreateCodeResp{
		Code:          code,
		Project:       projectSlug,
		ProjectName:   projectName,
		ExpireSeconds: expireSec,
	}, nil
}

// CheckCode 查询指定验证码的核销状态
func (s *VerifyService) CheckCode(ctx context.Context, code string) (*model.CheckCodeResp, error) {
	record, err := s.storage.GetCode(ctx, code)
	if err != nil {
		return nil, err
	}

	projectName := record.ProjectName
	if projectName == "" {
		projectName = record.Project
	}

	return &model.CheckCodeResp{
		Code:        record.Code,
		Project:     record.Project,
		ProjectName: projectName,
		Status:      record.Status,
		Verified:    record.Status == model.StatusVerified,
		OpenID:      record.OpenID,
		VerifiedAt:  record.VerifiedAt,
		Extra:       record.Extra,
	}, nil
}

// GenerateUniversalCode 为指定微信用户生成或复用通用的通行验证码（流向二）
func (s *VerifyService) GenerateUniversalCode(ctx context.Context, openID string) (string, error) {
	// 1. 防刷：先检查用户当前是否有尚未过期的有效通用码
	activeCode, err := s.storage.GetUserActiveUniversalCode(ctx, openID)
	if err == nil && activeCode != "" {
		return activeCode, nil
	}

	// 2. 生成全新 6 位唯一码
	var code string
	for i := 0; i < 5; i++ {
		c, err := randomDigits(6)
		if err != nil {
			return "", err
		}
		exists, err := s.storage.Exists(ctx, c)
		if err != nil {
			return "", err
		}
		if !exists {
			code = c
			break
		}
	}
	if code == "" {
		return "", fmt.Errorf("system busy, failed to generate code")
	}

	record := &model.CodeRecord{
		Code:        code,
		Project:     "universal",
		ProjectName: "通用登录",
		Status:      model.StatusPending,
		OpenID:      openID,
		CreatedAt:   time.Now().Unix(),
	}

	ttl := time.Duration(s.cfg.DefaultCodeExpire) * time.Second
	if ttl <= 0 {
		ttl = 300 * time.Second
	}

	if err := s.storage.SaveUserUniversalCode(ctx, openID, record, ttl); err != nil {
		return "", err
	}

	return code, nil
}

// VerifyUniversalCode 业务网站主动验证并消费用户的通用码（流向二）
func (s *VerifyService) VerifyUniversalCode(ctx context.Context, req *model.VerifyUniversalReq) (*model.VerifyUniversalResp, error) {
	reqCode := strings.TrimSpace(req.Code)
	if reqCode == "" {
		return nil, fmt.Errorf("code cannot be empty")
	}
	projectSlug := strings.TrimSpace(req.Project)
	if projectSlug == "" {
		return nil, fmt.Errorf("field 'project' is required")
	}

	projectName := projectSlug
	// 严格白名单与密钥鉴权校验
	if s.projectService != nil {
		p, err := s.projectService.GetProject(projectSlug)
		if err != nil {
			return nil, fmt.Errorf("unauthorized: 项目 '%s' 未在白名单中登记", projectSlug)
		}
		if !p.Enabled {
			return nil, fmt.Errorf("unauthorized: 项目 '%s' 已被管理员停用", p.Name)
		}
		// 校验通信密钥 Key
		if strings.TrimSpace(req.Key) == "" || strings.TrimSpace(req.Key) != p.Secret {
			return nil, fmt.Errorf("unauthorized: 项目通信密钥（Key）不匹配或未提供")
		}
		projectSlug = p.ID   // 始终保持标准英文标识
		projectName = p.Name // 项目中文展示名称
	}

	record, err := s.storage.ConsumeUniversalCode(ctx, reqCode, projectSlug, projectName)
	if err != nil {
		return nil, err
	}

	return &model.VerifyUniversalResp{
		Valid:       true,
		OpenID:      record.OpenID,
		Project:     projectSlug,
		ProjectName: projectName,
		VerifiedAt:  record.VerifiedAt,
	}, nil
}

// randomDigits 生成指定长度的纯数字字符串
func randomDigits(n int) (string, error) {
	const digits = "0123456789"
	b := make([]byte, n)
	for i := range b {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(digits))))
		if err != nil {
			return "", err
		}
		b[i] = digits[num.Int64()]
	}
	return string(b), nil
}
