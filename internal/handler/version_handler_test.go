package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"wechat-mp/internal/buildinfo"
)

func TestVersionEndpointIsPublicReadOnlyAndUncached(t *testing.T) {
	w := httptest.NewRecorder()
	Version(w, httptest.NewRequest(http.MethodGet, "/api/version", nil))
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("版本读取应成功且不缓存: %d %v", w.Code, w.Header())
	}
	var response struct {
		Code int            `json:"code"`
		Data buildinfo.Info `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != 0 || response.Data.DisplayVersion == "" || response.Data != buildinfo.Current() {
		t.Fatalf("必须返回实际构建信息: %+v", response)
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		w = httptest.NewRecorder()
		Version(w, httptest.NewRequest(method, "/api/version", nil))
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("不能写入版本: %s %d", method, w.Code)
		}
	}
}
