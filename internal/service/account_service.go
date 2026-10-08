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

	"wechat-mp/internal/config"
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
	cfg          *config.Config
	accountsMu   sync.Mutex
}

func NewAccountService(configDir string, store *storage.Storage, verifySvc *VerifyService, cfg *config.Config) *AccountService {
	s := &AccountService{
		accountsPath: filepath.Join(configDir, "accounts.json"),
		store:        store,
		verifySvc:    verifySvc,
		cfg:          cfg,
	}
	s.ensureInitialized()
	return s
}

var ErrDevLoginDisabled = errors.New("dev login disabled")

// DevLogin 本机开发通道：免通行码直接以指定账号 ID 登录已有账号。
// 仅当环境变量 DEV_LOCAL_LOGIN 开启时可用（本地开发用）；公开部署不设置该变量，此通道自动关闭。
// 不创建新账号——账号 ID 必须已注册，防止公开环境下被陌生人利用。
func (s *AccountService) DevLogin(ctx context.Context, accountID string) (*model.SessionInfo, error) {
	if s.cfg == nil || !s.cfg.DevLocalLogin {
		return nil, ErrDevLoginDisabled
	}
	if accountID == "" {
		return nil, fmt.Errorf("账号 ID 不能为空")
	}
	accounts, err := s.readAccounts()
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		if accounts[i].ID == accountID {
			if accounts[i].Status != model.AccountActive {
				return nil, ErrAccountDisabled
			}
			accounts[i].LastLogin = time.Now().Unix()
			_ = s.writeAccounts(accounts)
			return s.createSession(&accounts[i])
		}
	}
	return nil, ErrAccountNotFound
}

// DevAccounts 返回本地开发登录可选择的正常账号，仅在开发登录开启时可用。
func (s *AccountService) DevAccounts() ([]model.DevAccountOption, error) {
	if !s.DevLoginAllowed() {
		return nil, ErrDevLoginDisabled
	}
	accounts, err := s.readAccounts()
	if err != nil {
		return nil, err
	}
	options := make([]model.DevAccountOption, 0, len(accounts))
	for _, account := range accounts {
		if account.Status != model.AccountActive {
			continue
		}
		options = append(options, model.DevAccountOption{
			ID:           account.ID,
			MaskedOpenID: maskOpenID(account.OpenID),
			Role:         account.Role,
		})
	}
	return options, nil
}

func maskOpenID(openID string) string {
	if len(openID) <= 10 {
		return openID
	}
	return openID[:8] + "****"
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
// 系统首个注册的账号自动成为管理员。返回值第二个 bool 表示本次是否新建了账号。
func (s *AccountService) LoginWithCode(ctx context.Context, loginCode string) (*model.SessionInfo, bool, error) {
	openID, err := s.consumeLoginCode(ctx, loginCode)
	if err != nil {
		return nil, false, err
	}

	accounts, err := s.readAccounts()
	if err != nil {
		return nil, false, err
	}
	for i := range accounts {
		if accounts[i].OpenID == openID {
			if accounts[i].Status != model.AccountActive {
				return nil, false, ErrAccountDisabled
			}
			accounts[i].LastLogin = time.Now().Unix()
			_ = s.writeAccounts(accounts)
			info, err := s.createSession(&accounts[i])
			return info, false, err
		}
	}

	// 首个注册的账号自动成为管理员（之后注册的都是普通用户）
	role := model.RoleTenant
	isNew := true
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
		return nil, true, err
	}
	info, err := s.createSession(&account)
	return info, isNew, err
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

// DevLoginAllowed 本机开发登录通道是否启用
func (s *AccountService) DevLoginAllowed() bool {
	return s.cfg != nil && s.cfg.DevLocalLogin
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

// Authenticate 从请求头解析会话（X-Session-Token），并以账号文件中的当前角色与状态为准。
// 这样即使 Redis 中仍有旧会话，账号降权或禁用也会立即生效。
func (s *AccountService) Authenticate(r *http.Request) *model.SessionInfo {
	token := r.Header.Get("X-Session-Token")
	if token == "" {
		return nil
	}
	info, err := s.store.GetSession(context.Background(), token)
	if err != nil || info == nil {
		return nil
	}
	// 管理员签发的临时普通用户预览身份不写入账号文件。
	if info.AccountID == "acc_preview" && info.Role == model.RoleTenant {
		return info
	}
	accounts, err := s.readAccounts()
	if err != nil {
		return nil
	}
	for i := range accounts {
		account := &accounts[i]
		if account.ID == info.AccountID && account.OpenID == info.OpenID && account.Status == model.AccountActive {
			info.Role = account.Role
			return info
		}
	}
	s.store.DeleteSession(context.Background(), token)
	return nil
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
