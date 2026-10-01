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
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}

	var req model.CreateCodeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "JSON 请求体格式不正确: "+err.Error())
		return
	}
	defer r.Body.Close()

	if strings.TrimSpace(req.Project) == "" {
		writeError(w, http.StatusBadRequest, 400, "缺少必填参数 'project'（项目标识）")
		return
	}

	resp, err := h.verifyService.GenerateCode(r.Context(), &req)
	if err != nil {
		if strings.HasPrefix(err.Error(), "unauthorized") {
			msg := strings.TrimPrefix(err.Error(), "unauthorized: ")
			writeError(w, http.StatusUnauthorized, 401, msg)
			return
		}
		writeError(w, http.StatusInternalServerError, 500, "生成验证码失败: "+err.Error())
		return
	}

	writeSuccess(w, resp)
}

// CheckCode 处理各项目查询验证码状态的请求
// GET /api/code/check?code=xxxxxx
func (h *VerifyHandler) CheckCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}

	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		writeError(w, http.StatusBadRequest, 400, "缺少必填参数 'code'（验证码）")
		return
	}

	resp, err := h.verifyService.CheckCode(r.Context(), code)
	if err != nil {
		if errors.Is(err, storage.ErrCodeNotFound) {
			// 返回 200 携带明确的业务状态，避免浏览器控制台爆红且方便业务端直接判断
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(model.APIResponse{
				Code:    404,
				Message: "验证码已失效或不存在",
				Data: map[string]interface{}{
					"code":       code,
					"status":     model.StatusExpired,
					"verified":   false,
					"error_type": model.ErrTypeNotFound,
				},
			})
			return
		}
		writeError(w, http.StatusInternalServerError, 500, "系统内部异常: "+err.Error())
		return
	}

	writeSuccess(w, resp)
}

// VerifyUniversal 处理业务网站主动提交 6 位通用码核销的请求（流向二）
// POST /api/code/verify-universal
func (h *VerifyHandler) VerifyUniversal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}

	var req model.VerifyUniversalReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "JSON 请求体格式不正确: "+err.Error())
		return
	}
	defer r.Body.Close()

	if strings.TrimSpace(req.Code) == "" {
		writeError(w, http.StatusBadRequest, 400, "缺少必填参数 'code'（验证码）")
		return
	}

	resp, err := h.verifyService.VerifyUniversalCode(r.Context(), &req)
	if err != nil {
		if errors.Is(err, storage.ErrCodeNotFound) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(model.APIResponse{
				Code:    404,
				Message: "验证码已失效或不存在",
				Data: map[string]interface{}{
					"valid":      false,
					"error_type": model.ErrTypeNotFound,
				},
			})
			return
		}
		if err.Error() == "code_already_used" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(model.APIResponse{
				Code:    40002,
				Message: "该验证码已于早前完成核销，请勿重复使用",
				Data: map[string]interface{}{
					"valid":      false,
					"error_type": model.ErrTypeAlreadyUsed,
				},
			})
			return
		}
		if strings.HasPrefix(err.Error(), "unauthorized") {
			msg := strings.TrimPrefix(err.Error(), "unauthorized: ")
			writeError(w, http.StatusUnauthorized, 401, msg)
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
