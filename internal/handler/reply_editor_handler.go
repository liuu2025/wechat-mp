package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"wechat-mp/internal/model"
	"wechat-mp/internal/service"
)

// ReplyEditorHandler 管理统一回复配置、常用内容库和无副作用预览。
type ReplyEditorHandler struct {
	projectService *service.ProjectService
	wechatService  *service.WechatService
	accountService *service.AccountService
}

func NewReplyEditorHandler(ps *service.ProjectService, ws *service.WechatService, as *service.AccountService) *ReplyEditorHandler {
	return &ReplyEditorHandler{projectService: ps, wechatService: ws, accountService: as}
}

func (h *ReplyEditorHandler) Settings(w http.ResponseWriter, r *http.Request) {
	if authAdmin(h.accountService, w, r) == nil {
		return
	}
	switch r.Method {
	case http.MethodGet:
		settings, err := h.projectService.GetReplySettings()
		if err != nil {
			writeError(w, http.StatusInternalServerError, 500, "回复配置读取失败: "+err.Error())
			return
		}
		writeSuccess(w, settings)
	case http.MethodPost:
		var settings model.ReplySettings
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			writeError(w, http.StatusBadRequest, 400, "JSON 请求体格式不正确: "+err.Error())
			return
		}
		if err := h.projectService.SaveReplySettings(&settings); err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, service.ErrReplyRevision) {
				status = http.StatusConflict
			}
			writeError(w, status, status, err.Error())
			return
		}
		writeSuccess(w, map[string]string{"message": "回复规则已保存并即时生效", "revision": settings.Revision})
	default:
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
	}
}

func (h *ReplyEditorHandler) Library(w http.ResponseWriter, r *http.Request) {
	if authAdmin(h.accountService, w, r) == nil {
		return
	}
	switch r.Method {
	case http.MethodGet:
		library, err := h.projectService.ListReplySnippets()
		if err != nil {
			writeError(w, http.StatusInternalServerError, 500, "常用内容读取失败: "+err.Error())
			return
		}
		writeSuccess(w, library)
	case http.MethodPost:
		var item model.ReplySnippet
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			writeError(w, http.StatusBadRequest, 400, "JSON 请求体格式不正确: "+err.Error())
			return
		}
		if err := h.projectService.SaveReplySnippet(&item); err != nil {
			writeError(w, http.StatusBadRequest, 400, err.Error())
			return
		}
		writeSuccess(w, map[string]string{"message": "常用内容已保存", "id": item.ID})
	default:
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
	}
}

func (h *ReplyEditorHandler) DeleteSnippet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	if authAdmin(h.accountService, w, r) == nil {
		return
	}
	var req model.DeleteReplySnippetReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "JSON 请求体格式不正确: "+err.Error())
		return
	}
	if strings.TrimSpace(req.ID) == "" {
		writeError(w, http.StatusBadRequest, 400, "缺少常用内容 ID")
		return
	}
	if err := h.projectService.DeleteReplySnippet(req.ID); err != nil {
		writeError(w, http.StatusBadRequest, 400, err.Error())
		return
	}
	writeSuccess(w, map[string]string{"message": "常用内容已删除；已插入的回复不受影响"})
}

func (h *ReplyEditorHandler) ReorderSnippets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	if authAdmin(h.accountService, w, r) == nil {
		return
	}
	var req model.ReorderReplySnippetsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "JSON 请求体格式不正确: "+err.Error())
		return
	}
	if err := h.projectService.ReorderReplySnippets(req.IDs); err != nil {
		writeError(w, http.StatusBadRequest, 400, err.Error())
		return
	}
	writeSuccess(w, map[string]string{"message": "常用内容顺序已更新"})
}

func (h *ReplyEditorHandler) Preview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	if authAdmin(h.accountService, w, r) == nil {
		return
	}
	var req model.ReplyPreviewReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "JSON 请求体格式不正确: "+err.Error())
		return
	}
	resp, err := h.wechatService.PreviewReply(req.Draft, strings.TrimSpace(req.Field), strings.TrimSpace(req.Message), strings.TrimSpace(req.ProjectID), strings.TrimSpace(req.Code))
	if err != nil {
		writeError(w, http.StatusBadRequest, 400, "预览失败: "+err.Error())
		return
	}
	writeSuccess(w, resp)
}
