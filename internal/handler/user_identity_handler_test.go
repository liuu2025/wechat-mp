package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"wechat-mp/internal/config"
	"wechat-mp/internal/service"
)

func TestRegisteredUsersRequiresLoginAndRejectsWrites(t *testing.T) {
	h := NewUserIdentityHandler(nil, service.NewAccountService(t.TempDir(), nil, nil, &config.Config{}))
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		w := httptest.NewRecorder()
		h.List(w, httptest.NewRequest(method, "/api/user-identities", nil))
		want := http.StatusMethodNotAllowed
		if method == http.MethodGet {
			want = http.StatusUnauthorized
		}
		if w.Code != want {
			t.Fatalf("%s: got %d want %d", method, w.Code, want)
		}
	}
}
