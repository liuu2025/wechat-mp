package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"wechat-mp/internal/model"
	"wechat-mp/internal/storage"
)

var (
	ErrAccountNotFound  = errors.New("account not found")
	ErrAccountDisabled  = errors.New("account disabled")
	ErrBootstrapDone    = errors.New("system already initialized")
	ErrLoginRateLimited = errors.New("login rate limited")
)

const (
	sessionTTL     = 7 * 24 * time.Hour // 会话有效期 7 天
	loginMaxPerMin = 5                  // 登录接口单 IP 每分钟尝试上限
)

// AccountService 账号与会话管理（accounts.json + Redis 会话）
type AccountService struct {
	accountsPath string
	store        *storage.Storage
	verifySvc    *VerifyService
	accountsMu   sync.Mutex
}

func NewAccountService(configDir string, store *storage.Storage, verifySvc *VerifyService) *AccountService {
	s := &AccountService{
		accountsPath: filepath.Join(configDir, "accounts.json"),
		store:        store,
		verifySvc:    verifySvc,
	}
	s.ensureInitialized()
	return s
}

func (s *AccountService) ensureInitialized() {
	if _, err := os.Stat(s.accountsPath); os.IsNotExist(err) {
		_ = os.WriteFile(s.accountsPath, []byte("[]"), 0644)
	}
}

func (s *AccountService) readAccounts() ([]model.Account, error) {
	s.accountsMu.Lock()
	defer s.accountsMu.Unlock()
	data, err := os.ReadFile(s.accountsPath)
	if err != nil {
		return nil, err
	}
	var accounts []model.Account
	if err := json.Unmarshal(data, &accounts); err != nil {
		return nil, err
	}
	return accounts, nil
}

func (s *AccountService) writeAccounts(accounts []model.Account) error {
	s.accountsMu.Lock()
	defer s.accountsMu.Unlock()
	data, err := json.MarshalIndent(accounts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.accountsPath, data, 0644)
}

// NeedsBootstrap 系统是否尚未初始化任何账号（引导模式）
func (s *AccountService) NeedsBootstrap() bool {
	accounts, err := s.readAccounts()
	if err != nil {
		return true
	}
	return len(accounts) == 0
}

// LoginWithCode 验证码登录（登录即注册为普通用户；能做什么由激活码决定，账号本身无害）
// 系统首个注册的账号自动成为管理员
func (s *AccountService) LoginWithCode(ctx context.Context, loginCode string) (*model.SessionInfo, error) {
	openID, err := s.consumeLoginCode(ctx, loginCode)
	if err != nil {
		return nil, err
	}

	accounts, err := s.readAccounts()
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		if accounts[i].OpenID == openID {
			if accounts[i].Status != model.AccountActive {
				return nil, ErrAccountDisabled
			}
			accounts[i].LastLogin = time.Now().Unix()
			_ = s.writeAccounts(accounts)
			return s.createSession(&accounts[i])
		}
	}

	// 首个注册的账号自动成为管理员（之后注册的都是普通用户）
	role := model.RoleTenant
	if s.NeedsBootstrap() {
		role = model.RoleAdmin
	}
	account := model.Account{
		ID:        "acc_" + generateRandomHex(4),
		OpenID:    openID,
		Role:      role,
		Status:    model.AccountActive,
		CreatedAt: time.Now().Unix(),
		LastLogin: time.Now().Unix(),
	}
	accounts = append(accounts, account)
	if err := s.writeAccounts(accounts); err != nil {
		return nil, err
	}
	return s.createSession(&account)
}

// BootstrapSession 本地测试通道：系统未初始化（无任何账号）时，以提供的 openid 直接创建管理员会话。
// 仅在系统无账号期间可用，首个账号创建后本通道永久失效（公开部署时窗口期与"首个登录即管理员"一致）
func (s *AccountService) BootstrapSession(openID string) (*model.SessionInfo, error) {
	if !s.NeedsBootstrap() {
		return nil, ErrBootstrapDone
	}
	if openID == "" {
		return nil, fmt.Errorf("openid 不能为空")
	}
	accounts, _ := s.readAccounts()
	for i := range accounts {
		if accounts[i].OpenID == openID {
			accounts[i].Role = model.RoleAdmin
			_ = s.writeAccounts(accounts)
			return s.createSession(&accounts[i])
		}
	}
	account := model.Account{
		ID:        "acc_" + generateRandomHex(4),
		OpenID:    openID,
		Role:      model.RoleAdmin,
		Status:    model.AccountActive,
		CreatedAt: time.Now().Unix(),
		LastLogin: time.Now().Unix(),
	}
	accounts = append(accounts, account)
	if err := s.writeAccounts(accounts); err != nil {
		return nil, err
	}
	return s.createSession(&account)
}

// SetAccountRole 管理员调整账号角色
func (s *AccountService) SetAccountRole(id, role string) error {
	if role != model.RoleAdmin && role != model.RoleTenant {
		return fmt.Errorf("非法的角色值")
	}
	accounts, err := s.readAccounts()
	if err != nil {
		return err
	}
	found := false
	for i := range accounts {
		if accounts[i].ID == id {
			accounts[i].Role = role
			found = true
			break
		}
	}
	if !found {
		return ErrAccountNotFound
	}
	return s.writeAccounts(accounts)
}

// PreviewSession 管理员视角切换：签发一个普通用户身份的预览会话（虚拟身份，不写入账号库）
func (s *AccountService) PreviewSession() (*model.SessionInfo, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	info := &model.SessionInfo{
		Token:     hex.EncodeToString(buf),
		AccountID: "acc_preview",
		OpenID:    "preview_tenant",
		Role:      model.RoleTenant,
	}
	if err := s.store.SaveSession(context.Background(), info, sessionTTL); err != nil {
		return nil, err
	}
	return info, nil
}

// ListAccounts 管理员查看全部账号
func (s *AccountService) ListAccounts() ([]model.Account, error) {
	return s.readAccounts()
}

// SetAccountStatus 管理员启停账号（禁用即踢出其全部会话）
func (s *AccountService) SetAccountStatus(ctx context.Context, id, status string) error {
	if status != model.AccountActive && status != model.AccountDisabled {
		return fmt.Errorf("非法的状态值")
	}
	accounts, err := s.readAccounts()
	if err != nil {
		return err
	}
	found := false
	for i := range accounts {
		if accounts[i].ID == id {
			accounts[i].Status = status
			found = true
			if status == model.AccountDisabled {
				s.store.DeleteAccountSessions(ctx, id)
			}
			break
		}
	}
	if !found {
		return ErrAccountNotFound
	}
	return s.writeAccounts(accounts)
}

// consumeLoginCode 消耗一张公众号通行码换取 openid（一次性，防重放）
func (s *AccountService) consumeLoginCode(ctx context.Context, loginCode string) (string, error) {
	return s.verifySvc.ConsumeLoginCode(ctx, loginCode)
}

// ----------------- 会话 -----------------

func (s *AccountService) createSession(account *model.Account) (*model.SessionInfo, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	info := &model.SessionInfo{
		Token:     hex.EncodeToString(buf),
		AccountID: account.ID,
		OpenID:    account.OpenID,
		Role:      account.Role,
	}
	if err := s.store.SaveSession(context.Background(), info, sessionTTL); err != nil {
		return nil, err
	}
	return info, nil
}

// Authenticate 从请求头解析会话（X-Session-Token），无效返回 nil
func (s *AccountService) Authenticate(r *http.Request) *model.SessionInfo {
	token := r.Header.Get("X-Session-Token")
	if token == "" {
		return nil
	}
	info, err := s.store.GetSession(context.Background(), token)
	if err != nil || info == nil {
		return nil
	}
	return info
}

func (s *AccountService) Logout(token string) {
	s.store.DeleteSession(context.Background(), token)
}

// AllowLoginAttempt 登录接口单 IP 频控（每分钟最多 5 次，防 6 位码爆破）
func (s *AccountService) AllowLoginAttempt(ctx context.Context, ip string) bool {
	if ip == "" {
		return true
	}
	count, err := s.store.RawIncr(ctx, fmt.Sprintf("wx:rate:login:%s", ip), time.Minute)
	if err != nil {
		return true
	}
	return count <= loginMaxPerMin
}
