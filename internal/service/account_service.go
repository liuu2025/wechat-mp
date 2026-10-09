package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
	ErrAccountNotFound    = errors.New("account not found")
	ErrAccountDisabled    = errors.New("account disabled")
	ErrRegistrationClosed = errors.New("registration closed")
	ErrBootstrapDone      = errors.New("system already initialized")
	ErrLoginRateLimited   = errors.New("login rate limited")
)

const (
	sessionTTL     = 7 * 24 * time.Hour // 会话有效期 7 天
	loginMaxPerMin = 5                  // 登录接口单 IP 每分钟尝试上限
)

// AccountService 账号与会话管理（accounts.json + Redis 会话）
type AccountService struct {
	accountsPath string
	settingsPath string
	store        *storage.Storage
	verifySvc    *VerifyService
	cfg          *config.Config
	accountsMu   sync.Mutex
}

func NewAccountService(configDir string, store *storage.Storage, verifySvc *VerifyService, cfg *config.Config) *AccountService {
	s := &AccountService{
		accountsPath: filepath.Join(configDir, "accounts.json"),
		settingsPath: filepath.Join(configDir, "platform_settings.json"),
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
	s.accountsMu.Lock()
	accounts, err := s.readAccountsLocked()
	if err != nil {
		s.accountsMu.Unlock()
		return nil, err
	}
	for i := range accounts {
		if accounts[i].ID == accountID {
			if accounts[i].Status != model.AccountActive {
				s.accountsMu.Unlock()
				return nil, ErrAccountDisabled
			}
			accounts[i].LastLogin = time.Now().Unix()
			if err := s.writeAccountsLocked(accounts); err != nil {
				s.accountsMu.Unlock()
				return nil, err
			}
			account := accounts[i]
			s.accountsMu.Unlock()
			return s.createSession(&account)
		}
	}
	s.accountsMu.Unlock()
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
	return s.readAccountsLocked()
}

func (s *AccountService) readAccountsLocked() ([]model.Account, error) {
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
	return s.writeAccountsLocked(accounts)
}

func (s *AccountService) writeAccountsLocked(accounts []model.Account) error {
	data, err := json.MarshalIndent(accounts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.accountsPath, data, 0644)
}

func defaultPlatformSettings(mode string) model.PlatformSettings {
	return model.PlatformSettings{RegistrationMode: mode}
}

// readPlatformSettingsLocked 缺少配置时兼容旧版本默认开放；文件损坏时安全关闭新注册。
// 调用方必须持有 accountsMu，使注册开关变更与账号创建形成同一个临界区。
func (s *AccountService) readPlatformSettingsLocked() model.PlatformSettings {
	data, err := os.ReadFile(s.settingsPath)
	if os.IsNotExist(err) {
		return defaultPlatformSettings(model.RegistrationOpen)
	}
	if err != nil {
		log.Printf("[Account] 读取平台注册设置失败，新注册已安全关闭: %v", err)
		return defaultPlatformSettings(model.RegistrationClosed)
	}
	var settings model.PlatformSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		log.Printf("[Account] 解析平台注册设置失败，新注册已安全关闭: %v", err)
		return defaultPlatformSettings(model.RegistrationClosed)
	}
	if settings.RegistrationMode != model.RegistrationOpen && settings.RegistrationMode != model.RegistrationClosed {
		log.Printf("[Account] 未知的注册模式 %q，新注册已安全关闭", settings.RegistrationMode)
		return defaultPlatformSettings(model.RegistrationClosed)
	}
	return settings
}

// PlatformSettings 返回当前平台注册设置。
func (s *AccountService) PlatformSettings() model.PlatformSettings {
	s.accountsMu.Lock()
	defer s.accountsMu.Unlock()
	return s.readPlatformSettingsLocked()
}

// SetRegistrationMode 管理员修改注册模式，保存后立即生效。
func (s *AccountService) SetRegistrationMode(mode, updatedBy string) (model.PlatformSettings, error) {
	if mode != model.RegistrationOpen && mode != model.RegistrationClosed {
		return model.PlatformSettings{}, fmt.Errorf("非法的注册模式")
	}
	settings := model.PlatformSettings{
		RegistrationMode: mode,
		UpdatedAt:        time.Now().Unix(),
		UpdatedBy:        updatedBy,
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return model.PlatformSettings{}, err
	}
	s.accountsMu.Lock()
	defer s.accountsMu.Unlock()
	if err := os.WriteFile(s.settingsPath, data, 0o644); err != nil {
		return model.PlatformSettings{}, err
	}
	return settings, nil
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

	account, isNew, err := s.findOrRegisterAccount(openID)
	if err != nil {
		return nil, false, err
	}
	info, err := s.createSession(account)
	return info, isNew, err
}

// findOrRegisterAccount 原子完成账号查找、注册开关判断、首个管理员判定与写入。
func (s *AccountService) findOrRegisterAccount(openID string) (*model.Account, bool, error) {
	s.accountsMu.Lock()
	defer s.accountsMu.Unlock()

	accounts, err := s.readAccountsLocked()
	if err != nil {
		return nil, false, err
	}
	for i := range accounts {
		if accounts[i].OpenID == openID {
			if accounts[i].Status != model.AccountActive {
				return nil, false, ErrAccountDisabled
			}
			accounts[i].LastLogin = time.Now().Unix()
			if err := s.writeAccountsLocked(accounts); err != nil {
				return nil, false, err
			}
			account := accounts[i]
			return &account, false, nil
		}
	}

	// 完全未初始化时始终允许第一个真实微信身份创建唯一管理员，避免配置导致永久锁死。
	if len(accounts) > 0 && s.readPlatformSettingsLocked().RegistrationMode != model.RegistrationOpen {
		return nil, false, ErrRegistrationClosed
	}

	// 首个注册的账号自动成为管理员（之后注册的都是普通用户）
	role := model.RoleTenant
	if len(accounts) == 0 {
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
	if err := s.writeAccountsLocked(accounts); err != nil {
		return nil, true, err
	}
	return &account, true, nil
}

// BootstrapSession 本地测试通道：系统未初始化（无任何账号）时，以提供的 openid 直接创建管理员会话。
// 仅在系统无账号期间可用，首个账号创建后本通道永久失效（公开部署时窗口期与"首个登录即管理员"一致）
func (s *AccountService) BootstrapSession(openID string) (*model.SessionInfo, error) {
	if openID == "" {
		return nil, fmt.Errorf("openid 不能为空")
	}
	s.accountsMu.Lock()
	accounts, err := s.readAccountsLocked()
	if err != nil {
		s.accountsMu.Unlock()
		return nil, err
	}
	if len(accounts) != 0 {
		s.accountsMu.Unlock()
		return nil, ErrBootstrapDone
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
	if err := s.writeAccountsLocked(accounts); err != nil {
		s.accountsMu.Unlock()
		return nil, err
	}
	s.accountsMu.Unlock()
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
	s.accountsMu.Lock()
	accounts, err := s.readAccountsLocked()
	if err != nil {
		s.accountsMu.Unlock()
		return err
	}
	found := false
	for i := range accounts {
		if accounts[i].ID == id {
			accounts[i].Status = status
			found = true
			break
		}
	}
	if !found {
		s.accountsMu.Unlock()
		return ErrAccountNotFound
	}
	if err := s.writeAccountsLocked(accounts); err != nil {
		s.accountsMu.Unlock()
		return err
	}
	s.accountsMu.Unlock()
	if status == model.AccountDisabled {
		s.store.DeleteAccountSessions(ctx, id)
	}
	return nil
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
