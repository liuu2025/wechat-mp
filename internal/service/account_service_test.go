package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"wechat-mp/internal/config"
	"wechat-mp/internal/model"
)

func TestDevAccounts_仅开发模式返回正常账号并脱敏OpenID(t *testing.T) {
	dir := t.TempDir()
	accounts := []model.Account{
		{ID: "acc_admin", OpenID: "mock_wx_admin_12345", Role: model.RoleAdmin, Status: model.AccountActive},
		{ID: "acc_tenant", OpenID: "tenant_openid_67890", Role: model.RoleTenant, Status: model.AccountActive},
		{ID: "acc_disabled", OpenID: "disabled_openid_1", Role: model.RoleTenant, Status: model.AccountDisabled},
	}
	data, err := json.Marshal(accounts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "accounts.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	disabled := NewAccountService(dir, nil, nil, &config.Config{})
	if _, err := disabled.DevAccounts(); err != ErrDevLoginDisabled {
		t.Fatalf("开发登录关闭时应拒绝账号列表，实际错误: %v", err)
	}

	enabled := NewAccountService(dir, nil, nil, &config.Config{DevLocalLogin: true})
	options, err := enabled.DevAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 2 {
		t.Fatalf("应只返回 2 个正常账号，实际 %d", len(options))
	}
	if options[0].ID != "acc_admin" || options[0].Role != model.RoleAdmin {
		t.Fatalf("管理员选项不正确: %+v", options[0])
	}
	if options[0].MaskedOpenID != "mock_wx_****" {
		t.Fatalf("OpenID 脱敏结果不正确: %q", options[0].MaskedOpenID)
	}
}
