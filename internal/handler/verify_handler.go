package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"wechat-mp/internal/model"
	"wechat-mp/internal/service"
	"wechat-mp/internal/storage"
)

type VerifyHandler struct {
	verifyService *service.VerifyService
}

func NewVerifyHandler(svc *service.VerifyService) *VerifyHandler {
	return &VerifyHandler{verifyService: svc}
}

// CreateCode 处理各项目生成验证码的请求
// POST /api/code/create
func (h *VerifyHandler) CreateCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "Method Not Allowed")
		return
	}

	var req model.CreateCodeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid json body: "+err.Error())
		return
	}
	defer r.Body.Close()

	if strings.TrimSpace(req.Project) == "" {
		writeError(w, http.StatusBadRequest, 400, "field 'project' is required")
		return
	}

	resp, err := h.verifyService.GenerateCode(r.Context(), &req)
	if err != nil {
		if strings.HasPrefix(err.Error(), "unauthorized") {
			writeError(w, http.StatusUnauthorized, 401, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, 500, "failed to generate code: "+err.Error())
		return
	}

	writeSuccess(w, resp)
}

// CheckCode 处理各项目查询验证码状态的请求
// GET /api/code/check?code=xxxxxx
func (h *VerifyHandler) CheckCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, 405, "Method Not Allowed")
		return
	}

	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		writeError(w, http.StatusBadRequest, 400, "query param 'code' is required")
		return
	}

	resp, err := h.verifyService.CheckCode(r.Context(), code)
	if err != nil {
		if errors.Is(err, storage.ErrCodeNotFound) {
			writeError(w, http.StatusNotFound, 404, "code not found or expired")
			return
		}
		writeError(w, http.StatusInternalServerError, 500, "internal error: "+err.Error())
		return
	}

	writeSuccess(w, resp)
}

// VerifyUniversal 处理业务网站主动提交 6 位通用码核销的请求（流向二）
// POST /api/code/verify-universal
func (h *VerifyHandler) VerifyUniversal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "Method Not Allowed")
		return
	}

	var req model.VerifyUniversalReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid json body: "+err.Error())
		return
	}
	defer r.Body.Close()

	if strings.TrimSpace(req.Code) == "" {
		writeError(w, http.StatusBadRequest, 400, "field 'code' is required")
		return
	}

	resp, err := h.verifyService.VerifyUniversalCode(r.Context(), &req)
	if err != nil {
		if errors.Is(err, storage.ErrCodeNotFound) {
			writeError(w, http.StatusNotFound, 404, "code not found or expired")
			return
		}
		if strings.HasPrefix(err.Error(), "unauthorized") {
			writeError(w, http.StatusUnauthorized, 401, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, 400, err.Error())
		return
	}

	writeSuccess(w, resp)
}

// Health 健康检查接口
// GET /health
func (h *VerifyHandler) Health(w http.ResponseWriter, r *http.Request) {
	writeSuccess(w, map[string]string{
		"status":  "healthy",
		"service": "wechat-mp-hub",
	})
}
