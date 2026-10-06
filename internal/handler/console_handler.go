package handler

import (
	"encoding/json"
	"net/http"

	"wechat-mp/internal/model"
	"wechat-mp/internal/service"
)

// ConsoleHandler 控制台登录与会话（验证码登录，无密码）
type ConsoleHandler struct {
	accountService *service.AccountService
}

func NewConsoleHandler(as *service.AccountService) *ConsoleHandler {
	return &ConsoleHandler{accountService: as}
}

// BootstrapStatus 是否尚未初始化（无任何账号；用于登录页显示本地测试通道）
// GET /api/console/bootstrap-status
func (h *ConsoleHandler) BootstrapStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	writeSuccess(w, map[string]bool{"needs_init": h.accountService.NeedsBootstrap()})
}

// BootstrapSession 本地测试通道：一键以当前浏览器身份创建管理员（仅系统未初始化时可用）
// POST /api/console/bootstrap-session
// PreviewSession 管理员视角切换：签发普通用户预览会话（仅管理员可用）
// POST /api/console/preview-session
func (h *ConsoleHandler) PreviewSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	if authAdmin(h.accountService, w, r) == nil {
		return
	}

	sess, err := h.accountService.PreviewSession()
	if err != nil {
		writeError(w, http.StatusInternalServerError, 500, err.Error())
		return
	}

	writeSuccess(w, sess)
}

// DevStatus 本机开发登录通道是否启用（公开接口，仅返回一个布尔）
// GET /api/console/dev-status
func (h *ConsoleHandler) DevStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	allowed := h.accountService.DevLoginAllowed()
	writeSuccess(w, map[string]bool{"dev_login": allowed})
}

// DevLogin 本机开发登录：免通行码，直接以指定 openid 登录已有账号
// POST /api/console/dev-login
func (h *ConsoleHandler) DevLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}

	var req struct {
		OpenID string `json:"openid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	sess, err := h.accountService.DevLogin(r.Context(), req.OpenID)
	if err != nil {
		if err == service.ErrDevLoginDisabled {
			writeError(w, http.StatusForbidden, 403, "本机开发登录通道未启用")
			return
		}
		if err == service.ErrAccountNotFound {
			writeError(w, http.StatusUnauthorized, 401, "该设备身份没有已注册账号，请先完成首次初始化")
			return
		}
		if err == service.ErrAccountDisabled {
			writeError(w, http.StatusForbidden, 403, "该账号已被停用")
			return
		}
		writeError(w, http.StatusUnauthorized, 401, err.Error())
		return
	}

	writeSuccess(w, map[string]interface{}{
		"token":   sess.Token,
		"role":    sess.Role,
		"openid":  sess.OpenID,
		"message": "本机开发登录成功",
	})
}

// Login 验证码登录：消耗公众号通行码 → 解析 openid → 发会话
// POST /api/console/login
func (h *ConsoleHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}

	var req model.LoginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	ip := getClientIP(r)
	if !h.accountService.AllowLoginAttempt(r.Context(), ip) {
		writeError(w, http.StatusTooManyRequests, 429, "尝试过于频繁，请一分钟后再试")
		return
	}

	sess, isNew, err := h.accountService.LoginWithCode(r.Context(), req.Code)
	if err != nil {
		if err == service.ErrAccountDisabled {
			writeError(w, http.StatusForbidden, 403, "该账号已被停用，请联系管理员")
			return
		}
		// 统一模糊提示，不暴露"码不存在/已使用"的细节，防止探测
		writeError(w, http.StatusUnauthorized, 401, "验证码无效或已过期，请在公众号重新获取")
		return
	}

	writeSuccess(w, map[string]interface{}{
		"token":    sess.Token,
		"role":     sess.Role,
		"openid":   sess.OpenID,
		"is_new":   isNew,
		"message":  map[bool]string{true: "账号注册成功，欢迎加入", false: "登录成功"}[isNew],
	})
}

// Me 当前会话信息（前端按角色渲染界面）
// GET /api/console/me
func (h *ConsoleHandler) Me(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	sess := h.accountService.Authenticate(r)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, 401, "未登录或会话已过期")
		return
	}
	writeSuccess(w, sess)
}

// Logout 登出（销毁会话）
// POST /api/console/logout
func (h *ConsoleHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	token := r.Header.Get("X-Session-Token")
	if token != "" {
		h.accountService.Logout(token)
	}
	writeSuccess(w, map[string]string{"message": "已退出登录"})
}

// ----------------- 账号管理（仅管理员） -----------------

// ListAccounts 账号列表
// GET /api/accounts
func (h *ConsoleHandler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	if authAdmin(h.accountService, w, r) == nil {
		return
	}

	accounts, err := h.accountService.ListAccounts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, 500, err.Error())
		return
	}
	writeSuccess(w, accounts)
}

// SetAccountRole 调整账号角色（不允许管理员对自己降级）
// POST /api/accounts/set-role
func (h *ConsoleHandler) SetAccountRole(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	sess := authAdmin(h.accountService, w, r)
	if sess == nil {
		return
	}

	var req struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	if req.ID == sess.AccountID {
		writeError(w, http.StatusBadRequest, 400, "不能调整自己的角色")
		return
	}

	if err := h.accountService.SetAccountRole(req.ID, req.Role); err != nil {
		writeError(w, http.StatusBadRequest, 400, err.Error())
		return
	}

	writeSuccess(w, map[string]string{"message": "账号角色已调整"})
}

// SetAccountStatus 启停账号（禁用即踢出全部会话）
// POST /api/accounts/set-status
func (h *ConsoleHandler) SetAccountStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	sess := authAdmin(h.accountService, w, r)
	if sess == nil {
		return
	}

	var req model.SetAccountStatusReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	if req.ID == sess.AccountID {
		writeError(w, http.StatusBadRequest, 400, "不能停用自己的账号")
		return
	}

	if err := h.accountService.SetAccountStatus(r.Context(), req.ID, req.Status); err != nil {
		writeError(w, http.StatusBadRequest, 400, err.Error())
		return
	}

	writeSuccess(w, map[string]string{"message": "账号状态已更新"})
}
