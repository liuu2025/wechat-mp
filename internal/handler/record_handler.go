package handler

import (
	"net/http"
	"strconv"
	"strings"

	"wechat-mp/internal/service"
)

type RecordHandler struct {
	recordService  *service.RecordService
	accountService *service.AccountService
	projectService *service.ProjectService
}

func NewRecordHandler(rs *service.RecordService, as *service.AccountService, ps *service.ProjectService) *RecordHandler {
	return &RecordHandler{recordService: rs, accountService: as, projectService: ps}
}

// List 取码与核销流水。管理员看全部，租户只看自己的项目。
// GET /api/records?page=1&page_size=30&source=&status=&project=&project_scope=&openid=
func (h *RecordHandler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	sess := authSession(h.accountService, r)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, 401, "未登录或会话已过期")
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	projects, err := h.projectService.ListProjects()
	if err != nil {
		writeError(w, http.StatusInternalServerError, 500, "读取项目筛选失败")
		return
	}
	result, err := h.recordService.List(service.RecordQuery{
		Source:     strings.TrimSpace(r.URL.Query().Get("source")),
		Status:     strings.TrimSpace(r.URL.Query().Get("status")),
		Project:    strings.TrimSpace(r.URL.Query().Get("project")),
		Unassigned: r.URL.Query().Get("project_scope") == "unassigned",
		OpenID:     strings.TrimSpace(r.URL.Query().Get("openid")),
		Page:       page,
		PageSize:   pageSize,
	}, sess.Role, sess.AccountID, projects)
	if err != nil {
		writeError(w, http.StatusInternalServerError, 500, "读取取码记录失败: "+err.Error())
		return
	}
	writeSuccess(w, result)
}
