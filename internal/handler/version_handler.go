package handler

import (
	"net/http"
	"wechat-mp/internal/buildinfo"
)

// Version returns public, immutable build metadata, never runtime config, credentials or user data.
func Version(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	writeSuccess(w, buildinfo.Current())
}
