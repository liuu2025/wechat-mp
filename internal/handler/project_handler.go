package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"wechat-mp/internal/model"
	"wechat-mp/internal/service"
)

type ProjectHandler struct {
	projectService *service.ProjectService
}

func NewProjectHandler(ps *service.ProjectService) *ProjectHandler {
	return &ProjectHandler{projectService: ps}
}

// 鉴权中间件：检查 X-Console-Password 是否与系统密码匹配
func (h *ProjectHandler) checkAuth(r *http.Request) bool {
	pwd := r.Header.Get("X-Console-Password")
	if pwd == "" {
		pwd = r.URL.Query().Get("token")
	}
	return h.projectService.CheckPassword(pwd)
}

// Login 校验控制台密码
// POST /api/console/login
func (h *ProjectHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "Method Not Allowed")
		return
	}

	var req model.LoginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	if !h.projectService.CheckPassword(req.Password) {
		writeError(w, http.StatusUnauthorized, 401, "密码错误，请重新输入")
		return
	}

	writeSuccess(w, map[string]interface{}{
		"authorized": true,
		"message":    "登录成功",
	})
}

// ChangePassword 修改控制台管理密码
// POST /api/console/change-password
func (h *ProjectHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "Method Not Allowed")
		return
	}

	var req model.ChangePasswordReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	if strings.TrimSpace(req.NewPassword) == "" {
		writeError(w, http.StatusBadRequest, 400, "新密码不能为空")
		return
	}

	if err := h.projectService.ChangePassword(req.OldPassword, req.NewPassword); err != nil {
		if err == service.ErrInvalidPassword {
			writeError(w, http.StatusUnauthorized, 401, "原密码错误，修改失败")
			return
		}
		writeError(w, http.StatusInternalServerError, 500, err.Error())
		return
	}

	writeSuccess(w, map[string]string{
		"message": "密码修改成功并已持久化保存！",
	})
}

// ListProjects 查询项目列表
// GET /api/projects
func (h *ProjectHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	if !h.checkAuth(r) {
		writeError(w, http.StatusUnauthorized, 401, "未授权访问，请输入管理密码")
		return
	}

	projects, err := h.projectService.ListProjects()
	if err != nil {
		writeError(w, http.StatusInternalServerError, 500, err.Error())
		return
	}

	writeSuccess(w, projects)
}

// AddProject 新增项目
// POST /api/projects/add
func (h *ProjectHandler) AddProject(w http.ResponseWriter, r *http.Request) {
	if !h.checkAuth(r) {
		writeError(w, http.StatusUnauthorized, 401, "未授权访问")
		return
	}

	var req model.AddProjectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	item, err := h.projectService.AddProject(&req)
	if err != nil {
		writeError(w, http.StatusBadRequest, 400, err.Error())
		return
	}

	writeSuccess(w, item)
}

// ToggleProject 切换项目状态
// POST /api/projects/toggle
func (h *ProjectHandler) ToggleProject(w http.ResponseWriter, r *http.Request) {
	if !h.checkAuth(r) {
		writeError(w, http.StatusUnauthorized, 401, "未授权访问")
		return
	}

	var req model.ToggleProjectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	if err := h.projectService.ToggleProject(req.ID, req.Enabled); err != nil {
		writeError(w, http.StatusBadRequest, 400, err.Error())
		return
	}

	writeSuccess(w, map[string]bool{"enabled": req.Enabled})
}

// UpdateProject 更新修改项目信息
// POST /api/projects/update
func (h *ProjectHandler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	if !h.checkAuth(r) {
		writeError(w, http.StatusUnauthorized, 401, "未授权访问")
		return
	}

	var req model.UpdateProjectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	if err := h.projectService.UpdateProject(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, err.Error())
		return
	}

	writeSuccess(w, map[string]string{"updated": req.ID})
}

// DeleteProject 删除项目
// POST /api/projects/delete
func (h *ProjectHandler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	if !h.checkAuth(r) {
		writeError(w, http.StatusUnauthorized, 401, "未授权访问")
		return
	}

	var req model.DeleteProjectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	if err := h.projectService.DeleteProject(req.ID); err != nil {
		writeError(w, http.StatusBadRequest, 400, err.Error())
		return
	}

	writeSuccess(w, map[string]string{"deleted": req.ID})
}

// ListFooters 获取默认尾注模版列表
// GET /api/footers
func (h *ProjectHandler) ListFooters(w http.ResponseWriter, r *http.Request) {
	if !h.checkAuth(r) {
		writeError(w, http.StatusUnauthorized, 401, "未授权访问")
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
	if !h.checkAuth(r) {
		writeError(w, http.StatusUnauthorized, 401, "未授权访问")
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
	if !h.checkAuth(r) {
		writeError(w, http.StatusUnauthorized, 401, "未授权访问")
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
	if !h.checkAuth(r) {
		writeError(w, http.StatusUnauthorized, 401, "未授权访问")
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
	if !h.checkAuth(r) {
		writeError(w, http.StatusUnauthorized, 401, "未授权访问")
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
	if !h.checkAuth(r) {
		writeError(w, http.StatusUnauthorized, 401, "未授权访问")
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

// GetReplies 获取全场景自动回复配置
// GET /api/replies
func (h *ProjectHandler) GetReplies(w http.ResponseWriter, r *http.Request) {
	if !h.checkAuth(r) {
		writeError(w, http.StatusUnauthorized, 401, "未授权访问")
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
	if !h.checkAuth(r) {
		writeError(w, http.StatusUnauthorized, 401, "未授权访问")
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
