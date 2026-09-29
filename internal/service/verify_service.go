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

// GenerateCode 为业务项目生成唯一的 6 位数字验证码
func (s *VerifyService) GenerateCode(ctx context.Context, req *model.CreateCodeReq) (*model.CreateCodeResp, error) {
	if strings.TrimSpace(req.Project) == "" {
		return nil, fmt.Errorf("field 'project' is required")
	}

	// 严格白名单与密钥鉴权校验
	if s.projectService != nil {
		p, err := s.projectService.GetProject(req.Project)
		if err != nil {
			return nil, fmt.Errorf("unauthorized: project '%s' not registered in whitelist", req.Project)
		}
		if !p.Enabled {
			return nil, fmt.Errorf("unauthorized: project '%s' is disabled by administrator", p.Name)
		}
		// 校验通信密钥 Key
		if strings.TrimSpace(req.Key) == "" || strings.TrimSpace(req.Key) != p.Secret {
			return nil, fmt.Errorf("unauthorized: invalid or missing project key")
		}
		req.Project = p.Name // 标准化为项目正式名称
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
			return nil, err
		}
		exists, err := s.storage.Exists(ctx, c)
		if err != nil {
			return nil, err
		}
		if !exists {
			code = c
			break
		}
	}
	if code == "" {
		return nil, fmt.Errorf("system busy, failed to generate unique code")
	}

	record := &model.CodeRecord{
		Code:      code,
		Project:   req.Project,
		Status:    model.StatusPending,
		CreatedAt: time.Now().Unix(),
		Extra:     req.Extra,
	}

	ttl := time.Duration(expireSec) * time.Second
	if err := s.storage.SaveCode(ctx, record, ttl); err != nil {
		return nil, err
	}

	return &model.CreateCodeResp{
		Code:          code,
		Project:       req.Project,
		ExpireSeconds: expireSec,
	}, nil
}

// CheckCode 查询指定验证码的核销状态
func (s *VerifyService) CheckCode(ctx context.Context, code string) (*model.CheckCodeResp, error) {
	record, err := s.storage.GetCode(ctx, code)
	if err != nil {
		return nil, err
	}

	return &model.CheckCodeResp{
		Code:       record.Code,
		Project:    record.Project,
		Status:     record.Status,
		Verified:   record.Status == model.StatusVerified,
		OpenID:     record.OpenID,
		VerifiedAt: record.VerifiedAt,
		Extra:      record.Extra,
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
		Code:      code,
		Project:   "universal",
		Status:    model.StatusPending,
		OpenID:    openID,
		CreatedAt: time.Now().Unix(),
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
	if strings.TrimSpace(req.Code) == "" {
		return nil, fmt.Errorf("code cannot be empty")
	}
	projectName := strings.TrimSpace(req.Project)
	if projectName == "" {
		return nil, fmt.Errorf("field 'project' is required")
	}

	// 严格白名单与密钥鉴权校验
	if s.projectService != nil {
		p, err := s.projectService.GetProject(projectName)
		if err != nil {
			return nil, fmt.Errorf("unauthorized: project '%s' not registered in whitelist", projectName)
		}
		if !p.Enabled {
			return nil, fmt.Errorf("unauthorized: project '%s' is disabled by administrator", p.Name)
		}
		// 校验通信密钥 Key
		if strings.TrimSpace(req.Key) == "" || strings.TrimSpace(req.Key) != p.Secret {
			return nil, fmt.Errorf("unauthorized: invalid or missing project key")
		}
		projectName = p.Name
	}

	record, err := s.storage.ConsumeUniversalCode(ctx, req.Code, projectName)
	if err != nil {
		return nil, err
	}

	return &model.VerifyUniversalResp{
		Valid:      true,
		OpenID:     record.OpenID,
		Project:    record.Project,
		VerifiedAt: record.VerifiedAt,
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
