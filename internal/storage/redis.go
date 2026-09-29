package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"wechat-mp/internal/model"

	"github.com/redis/go-redis/v9"
)

var (
	ErrCodeNotFound = errors.New("code not found or expired")
)

type Storage struct {
	client *redis.Client
}

func NewStorage(addr, password string, db int) (*Storage, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("connect redis failed: %w", err)
	}

	return &Storage{client: rdb}, nil
}

func (s *Storage) Close() error {
	return s.client.Close()
}

func codeKey(code string) string {
	return fmt.Sprintf("wx:code:%s", code)
}

// SaveCode 保存生成的验证码记录（并设置 TTL 自动失效）
func (s *Storage) SaveCode(ctx context.Context, record *model.CodeRecord, ttl time.Duration) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, codeKey(record.Code), data, ttl).Err()
}

// GetCode 获取验证码记录
func (s *Storage) GetCode(ctx context.Context, code string) (*model.CodeRecord, error) {
	val, err := s.client.Get(ctx, codeKey(code)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, ErrCodeNotFound
	}
	if err != nil {
		return nil, err
	}

	var record model.CodeRecord
	if err := json.Unmarshal([]byte(val), &record); err != nil {
		return nil, err
	}
	return &record, nil
}

// MarkVerified 标记验证码已成功核销（保留剩余 TTL，供业务方轮询查询）
func (s *Storage) MarkVerified(ctx context.Context, code, openID string) (*model.CodeRecord, error) {
	record, err := s.GetCode(ctx, code)
	if err != nil {
		return nil, err
	}

	record.Status = model.StatusVerified
	record.OpenID = openID
	record.VerifiedAt = time.Now().Unix()

	data, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}

	// 保留当前的剩余过期时间（至少保留 2 分钟供查询）
	ttl, err := s.client.TTL(ctx, codeKey(code)).Result()
	if err != nil || ttl < 2*time.Minute {
		ttl = 2 * time.Minute
	}

	if err := s.client.Set(ctx, codeKey(code), data, ttl).Err(); err != nil {
		return nil, err
	}

	return record, nil
}

// Exists 检查验证码是否存在
func (s *Storage) Exists(ctx context.Context, code string) (bool, error) {
	n, err := s.client.Exists(ctx, codeKey(code)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func userActiveCodeKey(openid string) string {
	return fmt.Sprintf("wx:user:active_code:%s", openid)
}

// GetUserActiveUniversalCode 获取用户当前尚未过期的通用验证码（防刷重用）
func (s *Storage) GetUserActiveUniversalCode(ctx context.Context, openid string) (string, error) {
	code, err := s.client.Get(ctx, userActiveCodeKey(openid)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	// 同时校验该 code 是否仍在活跃池中
	exists, _ := s.Exists(ctx, code)
	if !exists {
		_ = s.client.Del(ctx, userActiveCodeKey(openid)).Err()
		return "", nil
	}
	return code, nil
}

// SaveUserUniversalCode 保存用户主动索取的通用通行码
func (s *Storage) SaveUserUniversalCode(ctx context.Context, openid string, record *model.CodeRecord, ttl time.Duration) error {
	if err := s.SaveCode(ctx, record, ttl); err != nil {
		return err
	}
	// 绑定用户与当前活跃码
	return s.client.Set(ctx, userActiveCodeKey(openid), record.Code, ttl).Err()
}

// ConsumeUniversalCode 网站核销通用码（一次性核销并废除该码）
func (s *Storage) ConsumeUniversalCode(ctx context.Context, code, targetProject string) (*model.CodeRecord, error) {
	record, err := s.GetCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if record.Status == model.StatusVerified {
		return nil, errors.New("code has already been verified and consumed")
	}

	record.Status = model.StatusVerified
	record.Project = targetProject
	record.VerifiedAt = time.Now().Unix()

	// 立即清理用户活跃码映射，并设置已核销状态保留短暂 TTL
	if record.OpenID != "" {
		_ = s.client.Del(ctx, userActiveCodeKey(record.OpenID)).Err()
	}

	data, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}

	// 验证成功后保留 1 分钟作为防重复提交标记，随后自动清理
	_ = s.client.Set(ctx, codeKey(code), data, 1*time.Minute).Err()

	return record, nil
}

const menuKey = "wx:custom_menu"

// SaveMenu 持久化菜单配置
func (s *Storage) SaveMenu(ctx context.Context, menu *model.MenuConfig) error {
	data, err := json.Marshal(menu)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, menuKey, data, 0).Err()
}

// GetMenu 获取当前自定义菜单配置
func (s *Storage) GetMenu(ctx context.Context) (*model.MenuConfig, error) {
	val, err := s.client.Get(ctx, menuKey).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil // 无自定义菜单
	}
	if err != nil {
		return nil, err
	}
	var menu model.MenuConfig
	if err := json.Unmarshal([]byte(val), &menu); err != nil {
		return nil, err
	}
	return &menu, nil
}
