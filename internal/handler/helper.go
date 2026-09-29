package handler

import (
	"encoding/json"
	"net/http"

	"wechat-mp/internal/model"
)

func writeJSON(w http.ResponseWriter, status int, resp *model.APIResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

func writeSuccess(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusOK, &model.APIResponse{
		Code:    0,
		Message: "success",
		Data:    data,
	})
}

func writeError(w http.ResponseWriter, status int, code int, message string) {
	writeJSON(w, status, &model.APIResponse{
		Code:    code,
		Message: message,
	})
}
