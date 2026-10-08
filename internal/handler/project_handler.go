package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"wechat-mp/internal/model"
	"wechat-mp/internal/service"
)

// ProjectHandler 项目与配置管理接口（会话鉴权：管理员全权，普通用户仅限自己的项目）
type ProjectHandler struct {
	projectService *service.ProjectService
	accountService *service.AccountService
	licenseService *service.LicenseService
}

func NewProjectHandler(ps *service.ProjectService, as *service.AccountService, ls *service.LicenseService) *ProjectHandler {
	return &ProjectHandler{projectService: ps, accountService: as, licenseService: ls}
}

// ----------------- 鉴权辅助 -----------------

func (h *ProjectHandler) auth(r *http.Request) *model.SessionInfo {
	return h.accountService.Authenticate(r)
}

func (h *ProjectHandler) authAdmin(w http.ResponseWriter, r *http.Request) *model.SessionInfo {
	return authAdmin(h.accountService, w, r)
}

// canManageProject 会话有效 且 （管理员 或 项目归属当前账号）
func (h *ProjectHandler) canManageProject(w http.ResponseWriter, r *http.Request, projectID string) *model.SessionInfo {
	sess := h.auth(r)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, 401, "未登录或会话已过期")
		return nil
	}
	if sess.Role != model.RoleAdmin {
		p, err := h.projectService.GetProject(projectID)
		if err != nil || p.OwnerAccountID != sess.AccountID {
			writeError(w, http.StatusForbidden, 403, "无权操作该项目")
			return nil
		}
	}
	return sess
}

// ----------------- 登录与账号 -----------------

// ----------------- 项目管理 -----------------

// ListProjects 查询项目列表（管理员看全部，普通用户只看自己的）
// GET /api/projects
func (h *ProjectHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	sess := h.auth(r)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, 401, "未登录或会话已过期")
		return
	}

	projects, err := h.projectService.ListProjects()
	if err != nil {
		writeError(w, http.StatusInternalServerError, 500, err.Error())
		return
	}

	if sess.Role != model.RoleAdmin {
		filtered := make([]*model.ProjectItem, 0)
		for _, p := range projects {
			if p.OwnerAccountID == sess.AccountID {
				filtered = append(filtered, p)
			}
		}
		projects = filtered
	}

	writeSuccess(w, projects)
}

// AddProject 新增项目（管理员直接创建；普通用户必须携带有效激活码）
// POST /api/projects/add
func (h *ProjectHandler) AddProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	sess := h.auth(r)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, 401, "未登录或会话已过期")
		return
	}

	var req model.AddProjectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	if strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, 400, "项目唯一标识与名称均必填")
		return
	}

	// 防止消耗激活码后才发现项目已存在：先预检
	if _, err := h.projectService.GetProject(req.ID); err == nil {
		writeError(w, http.StatusBadRequest, 400, "项目唯一标识已存在，请更换")
		return
	}

	var expiresAt int64
	if sess.Role != model.RoleAdmin {
		code := strings.TrimSpace(req.LicenseCode)
		if code == "" {
			writeError(w, http.StatusBadRequest, 400, "创建项目需要有效的激活码，请向管理员获取")
			return
		}
		duration, err := h.licenseService.Consume(code, sess.AccountID, req.ID)
		if err != nil {
			writeError(w, http.StatusBadRequest, 400, err.Error())
			return
		}
		expiresAt = service.ComputeExpiry(0, duration)
	}

	item, err := h.projectService.AddProject(&req, sess.AccountID, expiresAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, 400, err.Error())
		return
	}

	writeSuccess(w, item)
}

// RenewProject 使用激活码为项目续期
// POST /api/projects/renew
func (h *ProjectHandler) RenewProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}

	var req struct {
		ID          string `json:"id"`
		LicenseCode string `json:"license_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	sess := h.canManageProject(w, r, req.ID)
	if sess == nil {
		return
	}

	project, err := h.projectService.GetProject(req.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, 404, "项目不存在")
		return
	}

	duration, err := h.licenseService.Consume(strings.TrimSpace(req.LicenseCode), sess.AccountID, req.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, 400, err.Error())
		return
	}

	newExpiry := service.ComputeExpiry(project.ExpiresAt, duration)
	if err := h.projectService.SetProjectExpiry(req.ID, newExpiry); err != nil {
		writeError(w, http.StatusInternalServerError, 500, err.Error())
		return
	}

	writeSuccess(w, map[string]interface{}{
		"expires_at": newExpiry,
		"message":    "续期成功",
	})
}

// ToggleProject 切换项目状态
// POST /api/projects/toggle
func (h *ProjectHandler) ToggleProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}

	var req model.ToggleProjectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	if h.canManageProject(w, r, req.ID) == nil {
		return
	}

	if err := h.projectService.ToggleProject(req.ID, req.Enabled); err != nil {
		writeError(w, http.StatusBadRequest, 400, err.Error())
		return
	}

	writeSuccess(w, map[string]bool{"enabled": req.Enabled})
}

// UpdateProject 更新修改项目信息
// POST /api/projects/update
func (h *ProjectHandler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}

	var req model.UpdateProjectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	if h.canManageProject(w, r, req.ID) == nil {
		return
	}

	if err := h.projectService.UpdateProject(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, err.Error())
		return
	}

	writeSuccess(w, map[string]string{"updated": req.ID})
}

// DeleteProject 删除项目（管理员可删任何，普通用户仅自己的）
// POST /api/projects/delete
func (h *ProjectHandler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}

	var req model.DeleteProjectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	if h.canManageProject(w, r, req.ID) == nil {
		return
	}

	if err := h.projectService.DeleteProject(req.ID); err != nil {
		writeError(w, http.StatusBadRequest, 400, err.Error())
		return
	}

	writeSuccess(w, map[string]string{"deleted": req.ID})
}

// RotateProjectKey 轮换项目密钥：生成新密钥，旧密钥立即失效（需管理密码二次确认）
// POST /api/projects/rotate-key
func (h *ProjectHandler) RotateProjectKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}

	var req model.RotateKeyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	if h.canManageProject(w, r, req.ID) == nil {
		return
	}

	secret, err := h.projectService.RotateProjectSecret(req.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, 500, err.Error())
		return
	}

	writeSuccess(w, map[string]interface{}{
		"secret":  secret,
		"message": "密钥已更换，旧密钥立即失效。请立即更新业务系统配置；核销中的验证码不受影响。",
	})
}

// ----------------- 默认尾注模版（仅管理员） -----------------

// ListFooters 获取默认尾注模版列表
// GET /api/footers
func (h *ProjectHandler) ListFooters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	if h.authAdmin(w, r) == nil {
		return
	}

	footers, err := h.projectService.ListFooters()
	if err != nil {
		writeError(w, http.StatusInternalServerError, 500, err.Error())
		return
	}

	writeSuccess(w, footers)
}

// AddFooter 新增默认尾注模版
// POST /api/footers/add
func (h *ProjectHandler) AddFooter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	if h.authAdmin(w, r) == nil {
		return
	}

	var req model.AddFooterReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	item, err := h.projectService.AddFooter(&req)
	if err != nil {
		writeError(w, http.StatusBadRequest, 400, err.Error())
		return
	}

	writeSuccess(w, item)
}

// UpdateFooter 修改默认尾注模版
// POST /api/footers/update
func (h *ProjectHandler) UpdateFooter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	if h.authAdmin(w, r) == nil {
		return
	}

	var req model.UpdateFooterReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	if err := h.projectService.UpdateFooter(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, err.Error())
		return
	}

	writeSuccess(w, map[string]string{"updated": req.ID})
}

// ToggleFooter 切换单个尾注激活状态
// POST /api/footers/toggle
func (h *ProjectHandler) ToggleFooter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	if h.authAdmin(w, r) == nil {
		return
	}

	var req model.ToggleFooterReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	if err := h.projectService.ToggleFooter(req.ID, req.Active); err != nil {
		writeError(w, http.StatusBadRequest, 400, err.Error())
		return
	}

	writeSuccess(w, map[string]bool{"active": req.Active})
}

// SetFootersActive 批量激活尾注（单选或多选）
// POST /api/footers/set-active
func (h *ProjectHandler) SetFootersActive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	if h.authAdmin(w, r) == nil {
		return
	}

	var req model.SetFootersActiveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	if err := h.projectService.SetFootersActive(req.ActiveIDs); err != nil {
		writeError(w, http.StatusBadRequest, 400, err.Error())
		return
	}

	writeSuccess(w, map[string]interface{}{"active_ids": req.ActiveIDs})
}

// DeleteFooter 删除尾注模版
// POST /api/footers/delete
func (h *ProjectHandler) DeleteFooter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	if h.authAdmin(w, r) == nil {
		return
	}

	var req model.DeleteFooterReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	if err := h.projectService.DeleteFooter(req.ID); err != nil {
		writeError(w, http.StatusBadRequest, 400, err.Error())
		return
	}

	writeSuccess(w, map[string]string{"deleted": req.ID})
}

// ----------------- 全场景回复文案（仅管理员） -----------------

// GetReplies 获取全场景自动回复配置
// GET /api/replies
func (h *ProjectHandler) GetReplies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	if h.authAdmin(w, r) == nil {
		return
	}

	replies, err := h.projectService.GetReplies()
	if err != nil {
		writeError(w, http.StatusInternalServerError, 500, err.Error())
		return
	}

	writeSuccess(w, replies)
}

// UpdateReplies 更新全场景自动回复配置
// POST /api/replies/update
func (h *ProjectHandler) UpdateReplies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	if h.authAdmin(w, r) == nil {
		return
	}

	var req model.RepliesConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	if err := h.projectService.UpdateReplies(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, err.Error())
		return
	}

	writeSuccess(w, map[string]string{"message": "自动回复文案配置已成功保存！"})
}
