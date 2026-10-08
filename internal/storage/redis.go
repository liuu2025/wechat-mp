package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

// SaveCodeWithPollToken 保存码记录并建立轮询密钥索引（同管道原子写入）
func (s *Storage) SaveCodeWithPollToken(ctx context.Context, record *model.CodeRecord, ttl time.Duration) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	pipe := s.client.TxPipeline()
	pipe.Set(ctx, codeKey(record.Code), data, ttl)
	pipe.Set(ctx, pollKey(record.PollToken), record.Code, ttl)
	_, err = pipe.Exec(ctx)
	return err
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

	// 核销完成，释放项目活跃码池额度
	s.ReleaseProjectActiveCode(ctx, record.Project, code)
	// 释放会话/IP 并发桶，并刷新轮询密钥索引有效期（与码记录同步）
	s.ReleaseCodeSlots(ctx, record.SessionTag, record.IP, code)
	if record.PollToken != "" {
		_ = s.SavePollToken(ctx, record.PollToken, code, ttl)
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

// ----------------- 安全防护：IP 与项目级限流配额 -----------------

// AllowIPCheck 检查单 IP 频控（防短时间机器连击，如 2 秒内最多 2 次）
func (s *Storage) AllowIPCheck(ctx context.Context, ip string, maxInWindow int, window time.Duration) (bool, error) {
	if ip == "" {
		return true, nil
	}
	key := fmt.Sprintf("wx:rate:ip:%s", ip)
	count, err := s.client.Incr(ctx, key).Result()
	if err != nil {
		return true, nil // Redis 异常时平滑放行，不阻断正常业务
	}
	if count == 1 {
		_ = s.client.Expire(ctx, key, window).Err()
	}
	return count <= int64(maxInWindow), nil
}

// AllowProjectRateCheck 检查单项目全局速率（防项目死循环刷量，如 1 秒最多 15 次）
func (s *Storage) AllowProjectRateCheck(ctx context.Context, project string, maxPerSec int) (bool, error) {
	if project == "" {
		return true, nil
	}
	key := fmt.Sprintf("wx:rate:prj:%s:%d", project, time.Now().Unix())
	count, err := s.client.Incr(ctx, key).Result()
	if err != nil {
		return true, nil
	}
	if count == 1 {
		_ = s.client.Expire(ctx, key, 2*time.Second).Err()
	}
	return count <= int64(maxPerSec), nil
}

// projectActiveCodeKey 项目活跃码池（ZSET：member=验证码，score=过期时间戳秒）
func projectActiveCodeKey(project string) string {
	return fmt.Sprintf("wx:quota:prj_active:%s", project)
}

// TryAcquireProjectActiveCode 尝试占用单项目活跃码池额度（上限内才发码）
// 先按过期时间清理已过期成员，再判容量；额度随核销释放、随过期自动回收，不存在只增不减的泄漏
func (s *Storage) TryAcquireProjectActiveCode(ctx context.Context, project, code string, ttl time.Duration, maxActive int) (bool, error) {
	if project == "" || code == "" {
		return true, nil
	}
	key := projectActiveCodeKey(project)
	ok, err := s.tryAcquireOnce(ctx, key, code, ttl, maxActive)
	if err != nil {
		// 兼容旧版本遗留的 string 计数器（只增不减的泄漏数据）：一次性清除后重试
		if strings.Contains(strings.ToLower(err.Error()), "wrongtype") {
			_ = s.client.Del(ctx, key).Err()
			ok, err = s.tryAcquireOnce(ctx, key, code, ttl, maxActive)
		}
	}
	if err != nil {
		// Redis 异常时平滑放行，不阻断正常业务
		return true, nil
	}
	return ok, nil
}

func (s *Storage) tryAcquireOnce(ctx context.Context, key, code string, ttl time.Duration, maxActive int) (bool, error) {
	now := float64(time.Now().Unix())
	expireAt := now + ttl.Seconds()
	pipe := s.client.TxPipeline()
	pipe.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%f", now))
	cardCmd := pipe.ZCard(ctx, key)
	pipe.ZAdd(ctx, key, redis.Z{Score: expireAt, Member: code})
	if _, err := pipe.Exec(ctx); err != nil {
		return false, err
	}
	if cardCmd.Val() >= int64(maxActive) {
		_ = s.client.ZRem(ctx, key, code).Err()
		return false, nil
	}
	return true, nil
}

// ReleaseProjectActiveCode 验证码核销时释放活跃码池额度
func (s *Storage) ReleaseProjectActiveCode(ctx context.Context, project, code string) {
	if project == "" || code == "" {
		return
	}
	_ = s.client.ZRem(ctx, projectActiveCodeKey(project), code).Err()
}

// ----------------- 核销防爆破：openid 维度失败频控 -----------------

const verifyFailWindow = 10 * time.Minute

// RecordVerifyFailure 记录一次核销失败（6 位数字不存在或已核销）
func (s *Storage) RecordVerifyFailure(ctx context.Context, openID string) {
	if openID == "" {
		return
	}
	key := fmt.Sprintf("wx:rate:verify_fail:%s", openID)
	count, err := s.client.Incr(ctx, key).Result()
	if err != nil {
		return
	}
	if count == 1 {
		_ = s.client.Expire(ctx, key, verifyFailWindow).Err()
	}
}

// IsVerifyBlocked 判断该 openid 是否因连续失败被临时拉黑
func (s *Storage) IsVerifyBlocked(ctx context.Context, openID string, maxFailures int) (bool, error) {
	if openID == "" {
		return false, nil
	}
	count, err := s.client.Get(ctx, fmt.Sprintf("wx:rate:verify_fail:%s", openID)).Int()
	if err != nil {
		return false, nil
	}
	return count >= maxFailures, nil
}

// ClearVerifyFailures 核销成功后清空失败计数
func (s *Storage) ClearVerifyFailures(ctx context.Context, openID string) {
	if openID == "" {
		return
	}
	_ = s.client.Del(ctx, fmt.Sprintf("wx:rate:verify_fail:%s", openID)).Err()
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
func (s *Storage) ConsumeUniversalCode(ctx context.Context, code, targetProject, targetProjectName, ownerAccountID string) (*model.CodeRecord, error) {
	record, err := s.GetCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if record.Status == model.StatusVerified || record.Status == model.StatusUsed {
		return nil, errors.New("code_already_used")
	}

	record.Status = model.StatusUsed
	record.Project = targetProject
	record.ProjectName = targetProjectName
	record.OwnerAccountID = ownerAccountID
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

	// 释放会话/IP 桶与轮询密钥索引（码已消费）
	s.ReleaseCodeSlots(ctx, record.SessionTag, record.IP, code)
	s.DeletePollToken(ctx, record.PollToken)

	return record, nil
}

// ----------------- 登录会话 -----------------

func sessionKey(token string) string {
	return fmt.Sprintf("wx:session:%s", token)
}

// SaveSession 保存登录会话（含 TTL）
func (s *Storage) SaveSession(ctx context.Context, info *model.SessionInfo, ttl time.Duration) error {
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, sessionKey(info.Token), data, ttl).Err()
}

// GetSession 读取会话（不存在或已过期返回 nil, nil）
func (s *Storage) GetSession(ctx context.Context, token string) (*model.SessionInfo, error) {
	val, err := s.client.Get(ctx, sessionKey(token)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var info model.SessionInfo
	if err := json.Unmarshal([]byte(val), &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// DeleteSession 删除单个会话（登出）
func (s *Storage) DeleteSession(ctx context.Context, token string) {
	_ = s.client.Del(ctx, sessionKey(token)).Err()
}

// DeleteAccountSessions 删除某账号的全部会话（禁用账号时踢出）
func (s *Storage) DeleteAccountSessions(ctx context.Context, accountID string) {
	var cursor uint64
	for {
		keys, next, err := s.client.Scan(ctx, cursor, "wx:session:*", 200).Result()
		if err != nil {
			return
		}
		for _, key := range keys {
			val, err := s.client.Get(ctx, key).Result()
			if err != nil {
				continue
			}
			var info model.SessionInfo
			if json.Unmarshal([]byte(val), &info) == nil && info.AccountID == accountID {
				_ = s.client.Del(ctx, key).Err()
			}
		}
		if next == 0 {
			return
		}
		cursor = next
	}
}

// RawIncr 通用计数器（INCR + 窗口 TTL），供登录限流等使用
func (s *Storage) RawIncr(ctx context.Context, key string, window time.Duration) (int64, error) {
	count, err := s.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if count == 1 {
		_ = s.client.Expire(ctx, key, window).Err()
	}
	return count, nil
}

// ----------------- 会话标识 / IP 并发桶 -----------------

func bucketKey(kind, id string) string {
	return fmt.Sprintf("wx:bucket:%s:%s", kind, id)
}

// TryAcquireSlot 在限容桶中占一个坑位（ZSET：member=验证码，score=过期时间；过期自动回收）
func (s *Storage) TryAcquireSlot(ctx context.Context, kind, id, code string, ttl time.Duration, max int) (bool, error) {
	key := bucketKey(kind, id)
	now := float64(time.Now().Unix())
	pipe := s.client.TxPipeline()
	pipe.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%f", now))
	cardCmd := pipe.ZCard(ctx, key)
	pipe.ZAdd(ctx, key, redis.Z{Score: now + ttl.Seconds(), Member: code})
	if _, err := pipe.Exec(ctx); err != nil {
		// Redis 异常时平滑放行，不阻断正常业务
		return true, nil
	}
	if cardCmd.Val() >= int64(max) {
		_ = s.client.ZRem(ctx, key, code).Err()
		return false, nil
	}
	return true, nil
}

// ReleaseCodeSlots 核销时释放码占用的全部分桶（会话标识桶与 IP 桶）
func (s *Storage) ReleaseCodeSlots(ctx context.Context, tag, ip, code string) {
	if tag != "" {
		_ = s.client.ZRem(ctx, bucketKey("tag", tag), code).Err()
	}
	if ip != "" {
		_ = s.client.ZRem(ctx, bucketKey("ip", ip), code).Err()
	}
}

// ----------------- 轮询密钥（poll_token）索引 -----------------

func pollKey(token string) string {
	return fmt.Sprintf("wx:poll:%s", token)
}

// SavePollToken 建立轮询密钥 → 码 的索引（TTL 与码一致）
func (s *Storage) SavePollToken(ctx context.Context, token, code string, ttl time.Duration) error {
	return s.client.Set(ctx, pollKey(token), code, ttl).Err()
}

// GetCodeByPollToken 以轮询密钥换取码记录（6 位码本身不再是查询句柄）
func (s *Storage) GetCodeByPollToken(ctx context.Context, token string) (*model.CodeRecord, error) {
	code, err := s.client.Get(ctx, pollKey(token)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, ErrCodeNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.GetCode(ctx, code)
}

// DeletePollToken 删除轮询密钥索引（码被消费时）
func (s *Storage) DeletePollToken(ctx context.Context, token string) {
	if token != "" {
		_ = s.client.Del(ctx, pollKey(token)).Err()
	}
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
