package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"wechat-mp/internal/config"
	"wechat-mp/internal/model"
)

func writeTestAccounts(t *testing.T, dir string, accounts []model.Account) {
	t.Helper()
	data, err := json.Marshal(accounts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "accounts.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDevAccounts_仅开发模式返回正常账号并脱敏OpenID(t *testing.T) {
	dir := t.TempDir()
	accounts := []model.Account{
		{ID: "acc_admin", OpenID: "mock_wx_admin_12345", Role: model.RoleAdmin, Status: model.AccountActive},
		{ID: "acc_tenant", OpenID: "tenant_openid_67890", Role: model.RoleTenant, Status: model.AccountActive},
		{ID: "acc_disabled", OpenID: "disabled_openid_1", Role: model.RoleTenant, Status: model.AccountDisabled},
	}
	writeTestAccounts(t, dir, accounts)

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

func TestPlatformSettings_缺失时默认开放并可持久化切换(t *testing.T) {
	dir := t.TempDir()
	svc := NewAccountService(dir, nil, nil, &config.Config{})

	if got := svc.PlatformSettings().RegistrationMode; got != model.RegistrationOpen {
		t.Fatalf("旧版本缺少设置文件时应默认开放，实际 %q", got)
	}

	updated, err := svc.SetRegistrationMode(model.RegistrationClosed, "acc_admin")
	if err != nil {
		t.Fatal(err)
	}
	if updated.RegistrationMode != model.RegistrationClosed || updated.UpdatedBy != "acc_admin" || updated.UpdatedAt == 0 {
		t.Fatalf("保存结果不完整: %+v", updated)
	}

	reloaded := NewAccountService(dir, nil, nil, &config.Config{})
	got := reloaded.PlatformSettings()
	if got.RegistrationMode != model.RegistrationClosed || got.UpdatedBy != "acc_admin" {
		t.Fatalf("重建服务后应读到已持久化设置，实际 %+v", got)
	}
	if _, err := svc.SetRegistrationMode("invalid", "acc_admin"); err == nil {
		t.Fatal("非法注册模式应被拒绝")
	}
}

func TestPlatformSettings_文件损坏时安全关闭(t *testing.T) {
	dir := t.TempDir()
	svc := NewAccountService(dir, nil, nil, &config.Config{})
	if err := os.WriteFile(filepath.Join(dir, "platform_settings.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := svc.PlatformSettings().RegistrationMode; got != model.RegistrationClosed {
		t.Fatalf("设置文件损坏时应安全关闭，实际 %q", got)
	}
}

func TestRegistrationClosed_已有账号可登录陌生账号被拒绝(t *testing.T) {
	dir := t.TempDir()
	writeTestAccounts(t, dir, []model.Account{{
		ID: "acc_admin", OpenID: "openid_admin", Role: model.RoleAdmin, Status: model.AccountActive,
	}})
	svc := NewAccountService(dir, nil, nil, &config.Config{})
	if _, err := svc.SetRegistrationMode(model.RegistrationClosed, "acc_admin"); err != nil {
		t.Fatal(err)
	}

	account, isNew, err := svc.findOrRegisterAccount("openid_admin")
	if err != nil || isNew || account.ID != "acc_admin" {
		t.Fatalf("关闭注册不应影响已有账号登录: account=%+v isNew=%v err=%v", account, isNew, err)
	}
	if _, _, err := svc.findOrRegisterAccount("openid_new"); !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("陌生账号应被注册开关拒绝，实际错误: %v", err)
	}

	accounts, err := svc.ListAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 {
		t.Fatalf("拒绝注册后不应产生账号，实际 %d 个", len(accounts))
	}

	if _, err := svc.SetRegistrationMode(model.RegistrationOpen, "acc_admin"); err != nil {
		t.Fatal(err)
	}
	account, isNew, err = svc.findOrRegisterAccount("openid_new")
	if err != nil || !isNew || account.Role != model.RoleTenant {
		t.Fatalf("重新开放后应创建普通账号: account=%+v isNew=%v err=%v", account, isNew, err)
	}
}

func TestRegistrationClosed_空系统仍允许创建首个管理员(t *testing.T) {
	dir := t.TempDir()
	svc := NewAccountService(dir, nil, nil, &config.Config{})
	if _, err := svc.SetRegistrationMode(model.RegistrationClosed, "preconfigured"); err != nil {
		t.Fatal(err)
	}

	account, isNew, err := svc.findOrRegisterAccount("first_openid")
	if err != nil || !isNew || account.Role != model.RoleAdmin {
		t.Fatalf("空系统应保留初始化出口: account=%+v isNew=%v err=%v", account, isNew, err)
	}
	if _, _, err := svc.findOrRegisterAccount("second_openid"); !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("首个管理员之后应立即执行关闭策略，实际错误: %v", err)
	}
}

func TestConcurrentRegistration_账号不丢失且仅一个管理员(t *testing.T) {
	dir := t.TempDir()
	svc := NewAccountService(dir, nil, nil, &config.Config{})

	const total = 50
	var wg sync.WaitGroup
	errs := make(chan error, total)
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			account, isNew, err := svc.findOrRegisterAccount(fmt.Sprintf("openid_%02d", i))
			if err != nil {
				errs <- err
				return
			}
			if !isNew || account == nil {
				errs <- fmt.Errorf("第 %d 个并发注册未创建账号", i)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	accounts, err := svc.ListAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != total {
		t.Fatalf("并发注册应完整保留 %d 个账号，实际 %d 个", total, len(accounts))
	}
	admins := 0
	for _, account := range accounts {
		if account.Role == model.RoleAdmin {
			admins++
		}
	}
	if admins != 1 {
		t.Fatalf("并发首次注册只能产生一个管理员，实际 %d 个", admins)
	}
}
