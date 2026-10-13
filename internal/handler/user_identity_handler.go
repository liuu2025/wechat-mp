package handler

import (
	"net/http"
	"strconv"
	"strings"

	"wechat-mp/internal/service"
)

type UserIdentityHandler struct {
	identities *service.UserIdentityService
	accounts   *service.AccountService
}

func NewUserIdentityHandler(identities *service.UserIdentityService, accounts *service.AccountService) *UserIdentityHandler {
	return &UserIdentityHandler{identities: identities, accounts: accounts}
}

// List 仅管理员可查看全部已登记身份；无写入、删除或重新编号接口。
func (h *UserIdentityHandler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	if authAdmin(h.accounts, w, r) == nil {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	accounts, err := h.accounts.ListAccounts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, 500, "读取平台账号失败")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	writeSuccess(w, h.identities.List(service.UserIdentityQuery{
		Keyword:      strings.TrimSpace(r.URL.Query().Get("keyword")),
		Registration: strings.TrimSpace(r.URL.Query().Get("registration")),
		Kind:         strings.TrimSpace(r.URL.Query().Get("kind")), Page: page, PageSize: pageSize,
	}, accounts))
}
