package handler

import (
	"encoding/json"
	"net/http"

	"wechat-mp/internal/model"
	"wechat-mp/internal/service"
)

// LicenseHandler 激活码（卡密）管理接口（仅管理员）
type LicenseHandler struct {
	licenseService *service.LicenseService
	accountService *service.AccountService
}

func NewLicenseHandler(ls *service.LicenseService, as *service.AccountService) *LicenseHandler {
	return &LicenseHandler{licenseService: ls, accountService: as}
}

// ListCodes 激活码列表（含使用留痕）
// GET /api/licenses
func (h *LicenseHandler) ListCodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	if authAdmin(h.accountService, w, r) == nil {
		return
	}
	codes, err := h.licenseService.ListAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, 500, err.Error())
		return
	}
	writeSuccess(w, codes)
}

// GenerateCodes 批量生成激活码
// POST /api/licenses/generate
func (h *LicenseHandler) GenerateCodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	if authAdmin(h.accountService, w, r) == nil {
		return
	}

	var req model.GenerateLicenseReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	generated, err := h.licenseService.GenerateCodes(req.Count, req.DurationDays, req.RedeemDays)
	if err != nil {
		writeError(w, http.StatusBadRequest, 400, err.Error())
		return
	}

	writeSuccess(w, map[string]interface{}{
		"codes":  generated,
		"count":  len(generated),
		"days":   req.DurationDays,
		"redeem": req.RedeemDays,
	})
}

// VoidCode 作废未使用的激活码
// POST /api/licenses/void
func (h *LicenseHandler) VoidCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	if authAdmin(h.accountService, w, r) == nil {
		return
	}

	var req model.VoidLicenseReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "invalid body")
		return
	}
	defer r.Body.Close()

	if err := h.licenseService.VoidCode(req.Code); err != nil {
		writeError(w, http.StatusBadRequest, 400, err.Error())
		return
	}

	writeSuccess(w, map[string]string{"message": "激活码已作废"})
}
