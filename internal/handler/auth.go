package handler

import (
	"net/http"

	"wechat-mp/internal/model"
	"wechat-mp/internal/service"
)

// authSession 会话校验（任何登录角色），无效返回 nil
func authSession(as *service.AccountService, r *http.Request) *model.SessionInfo {
	return as.Authenticate(r)
}

// authAdmin 管理员校验：未登录 401，非管理员 403
func authAdmin(as *service.AccountService, w http.ResponseWriter, r *http.Request) *model.SessionInfo {
	sess := as.Authenticate(r)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, 401, "未登录或会话已过期")
		return nil
	}
	if sess.Role != model.RoleAdmin {
		writeError(w, http.StatusForbidden, 403, "该操作仅管理员可用")
		return nil
	}
	return sess
}
