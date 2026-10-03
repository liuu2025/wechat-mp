package service

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"

	"wechat-mp/internal/model"
)

var (
	ErrLicenseNotFound = errors.New("激活码不存在")
	ErrLicenseUsed     = errors.New("激活码已被使用")
	ErrLicenseVoid     = errors.New("激活码已作废")
	ErrLicenseExpired  = errors.New("激活码已超过兑换期")
)

// 去混淆字符集（无 0/O/1/I），码格式 XXXX-XXXX-XXXX
const licenseAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// LicenseService 激活码（卡密）管理：生成 / 兑换（原子） / 作废 / 列表
type LicenseService struct {
	mu        sync.Mutex // 单实例文件锁：兑换原子性由此保证
	codesPath string
}

func NewLicenseService(configDir string) *LicenseService {
	s := &LicenseService{codesPath: filepath.Join(configDir, "licenses.json")}
	s.ensureInitialized()
	return s
}

func (s *LicenseService) ensureInitialized() {
	if _, err := os.Stat(s.codesPath); os.IsNotExist(err) {
		_ = os.WriteFile(s.codesPath, []byte("[]"), 0644)
	}
}

func (s *LicenseService) readCodes() ([]model.LicenseCode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.codesPath)
	if err != nil {
		return nil, err
	}
	var codes []model.LicenseCode
	if err := json.Unmarshal(data, &codes); err != nil {
		return nil, err
	}
	return codes, nil
}

func (s *LicenseService) writeCodes(codes []model.LicenseCode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(codes, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.codesPath, data, 0644)
}

func randomLicenseCode() (string, error) {
	out := make([]byte, 12)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(licenseAlphabet))))
		if err != nil {
			return "", err
		}
		out[i] = licenseAlphabet[n.Int64()]
	}
	return string(out[0:4]) + "-" + string(out[4:8]) + "-" + string(out[8:12]), nil
}

// GenerateCodes 批量生成激活码
func (s *LicenseService) GenerateCodes(count, durationDays, redeemDays int) ([]string, error) {
	if count < 1 || count > 100 {
		return nil, fmt.Errorf("生成数量须在 1-100 之间")
	}
	if durationDays < 1 || durationDays > 3650 {
		return nil, fmt.Errorf("可用天数须在 1-3650 之间")
	}
	if redeemDays < 1 || redeemDays > 3650 {
		return nil, fmt.Errorf("兑换期须在 1-3650 天之间")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	codes, err := s.readCodesLocked()
	if err != nil {
		return nil, err
	}
	existing := make(map[string]bool)
	for _, c := range codes {
		existing[c.Code] = true
	}

	now := time.Now().Unix()
	deadline := now + int64(redeemDays)*86400
	generated := make([]string, 0, count)
	for len(generated) < count {
		code, err := randomLicenseCode()
		if err != nil {
			return nil, err
		}
		if existing[code] {
			continue
		}
		codes = append(codes, model.LicenseCode{
			Code:           code,
			DurationDays:   durationDays,
			RedeemDeadline: deadline,
			Status:         model.LicenseUnused,
			CreatedAt:      now,
		})
		existing[code] = true
		generated = append(generated, code)
	}

	data, err := json.MarshalIndent(codes, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(s.codesPath, data, 0644); err != nil {
		return nil, err
	}
	return generated, nil
}

func (s *LicenseService) readCodesLocked() ([]model.LicenseCode, error) {
	data, err := os.ReadFile(s.codesPath)
	if err != nil {
		return nil, err
	}
	var codes []model.LicenseCode
	if err := json.Unmarshal(data, &codes); err != nil {
		return nil, err
	}
	return codes, nil
}

// Consume 兑换激活码（原子：同一码并发请求只有一个成功），返回可用时长天数
func (s *LicenseService) Consume(code, accountID, projectID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	codes, err := s.readCodesLocked()
	if err != nil {
		return 0, err
	}
	now := time.Now().Unix()
	for i := range codes {
		if codes[i].Code != code {
			continue
		}
		switch codes[i].Status {
		case model.LicenseUsed:
			return 0, ErrLicenseUsed
		case model.LicenseVoid:
			return 0, ErrLicenseVoid
		}
		if codes[i].RedeemDeadline > 0 && now > codes[i].RedeemDeadline {
			return 0, ErrLicenseExpired
		}
		codes[i].Status = model.LicenseUsed
		codes[i].UsedBy = accountID
		codes[i].UsedAt = now
		codes[i].ProjectID = projectID
		data, err := json.MarshalIndent(codes, "", "  ")
		if err != nil {
			return 0, err
		}
		if err := os.WriteFile(s.codesPath, data, 0644); err != nil {
			return 0, err
		}
		return codes[i].DurationDays, nil
	}
	return 0, ErrLicenseNotFound
}

// VoidCode 作废未使用的激活码
func (s *LicenseService) VoidCode(code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	codes, err := s.readCodesLocked()
	if err != nil {
		return err
	}
	for i := range codes {
		if codes[i].Code == code {
			if codes[i].Status != model.LicenseUnused {
				return fmt.Errorf("仅未使用的激活码可以作废")
			}
			codes[i].Status = model.LicenseVoid
			data, err := json.MarshalIndent(codes, "", "  ")
			if err != nil {
				return err
			}
			return os.WriteFile(s.codesPath, data, 0644)
		}
	}
	return ErrLicenseNotFound
}

// LicenseStatusText 计算展示状态（未使用但超过兑换期的按"已过期"展示，惰性无需写盘）
func LicenseStatusText(c model.LicenseCode) string {
	if c.Status == model.LicenseUnused && c.RedeemDeadline > 0 && time.Now().Unix() > c.RedeemDeadline {
		return "已过期"
	}
	switch c.Status {
	case model.LicenseUnused:
		return "未使用"
	case model.LicenseUsed:
		return "已使用"
	case model.LicenseVoid:
		return "已作废"
	}
	return c.Status
}

// ComputeExpiry 续期算术：新时长从「原到期时间」和「现在」中较晚者起累加
func ComputeExpiry(currentExpiry int64, durationDays int) int64 {
	now := time.Now().Unix()
	base := now
	if currentExpiry > now {
		base = currentExpiry
	}
	return base + int64(durationDays)*86400
}

// ListAll 全量码列表（管理员对账用）
func (s *LicenseService) ListAll() ([]model.LicenseCode, error) {
	return s.readCodes()
}
