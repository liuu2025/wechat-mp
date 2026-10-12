package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wechat-mp/internal/config"
	"wechat-mp/internal/model"
)

// ---------- 授权到期算术 ----------

func TestComputeExpiry_未到期续期自动顺延(t *testing.T) {
	now := time.Now().Unix()
	future := now + 10*86400 // 原到期还在 10 天后
	got := ComputeExpiry(future, 30)
	want := future + 30*86400
	if got != want {
		t.Fatalf("未到期续期应从原到期时间顺延: got %d want %d", got, want)
	}
}

func TestComputeExpiry_已过期从当天重算(t *testing.T) {
	now := time.Now().Unix()
	past := now - 90*86400 // 原到期早已过去
	got := ComputeExpiry(past, 30)
	want := now + 30*86400
	if got != want {
		t.Fatalf("过期后续期应从当下重算: got %d want %d", got, want)
	}
}

func TestComputeExpiry_零值视为新激活(t *testing.T) {
	now := time.Now().Unix()
	got := ComputeExpiry(0, 30)
	if got < now+30*86400 || got > now+30*86400+5 {
		t.Fatalf("新激活应从当下起算: got %d", got)
	}
}

// ---------- 项目可用性 ----------

func TestIsProjectUsable(t *testing.T) {
	now := time.Now().Unix()
	cases := []struct {
		name string
		p    *model.ProjectItem
		want bool
	}{
		{"启用且永久", &model.ProjectItem{Enabled: true}, true},
		{"已停用", &model.ProjectItem{Enabled: false}, false},
		{"启用但已到期", &model.ProjectItem{Enabled: true, ExpiresAt: now - 1}, false},
		{"启用且未到期", &model.ProjectItem{Enabled: true, ExpiresAt: now + 60}, true},
		{"空项目", nil, false},
	}
	for _, c := range cases {
		if got := IsProjectUsable(c.p); got != c.want {
			t.Fatalf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestVerificationSettingsRuntimePersistenceAndBounds(t *testing.T) {
	dir := t.TempDir()
	svc := NewProjectService(dir)
	initial, err := svc.GetVerificationSettings(300)
	if err != nil {
		t.Fatal(err)
	}
	if initial.DefaultCodeExpireSeconds != 300 {
		t.Fatalf("首次应沿用环境默认值: %#v", initial)
	}

	saved := &model.VerificationSettings{DefaultCodeExpireSeconds: 420, UpdatedAt: 123, UpdatedBy: "acc_admin"}
	if err := svc.SaveVerificationSettings(saved); err != nil {
		t.Fatal(err)
	}
	reloaded := NewProjectService(dir)
	got, err := reloaded.GetVerificationSettings(180)
	if err != nil {
		t.Fatal(err)
	}
	if *got != *saved {
		t.Fatalf("运行时设置未持久化: got %#v want %#v", got, saved)
	}
	verify := NewVerifyService(&config.Config{DefaultCodeExpire: 180}, nil, reloaded)
	if seconds := verify.CodeExpireSeconds(); seconds != 420 {
		t.Fatalf("保存后的设置没有即时覆盖环境默认值: got %d want 420", seconds)
	}
	if err := svc.SaveVerificationSettings(&model.VerificationSettings{DefaultCodeExpireSeconds: 601}); err == nil {
		t.Fatal("超过 10 分钟必须拒绝")
	}
}

// ---------- 规则校验 ----------

func validRules() *model.RulesConfig {
	return &model.RulesConfig{
		UniversalCode: model.BuiltinRule{Enabled: true, Keywords: []string{"验证码", "取码"}},
		Help:          model.BuiltinRule{Enabled: true, Keywords: []string{"帮助"}},
		Projects:      model.BuiltinRule{Enabled: false, Keywords: []string{"项目"}},
		Custom:        []model.CustomRule{},
	}
}

func TestValidateRules_合法配置通过(t *testing.T) {
	if err := validateRules(validRules()); err != nil {
		t.Fatalf("合法配置不应报错: %v", err)
	}
}

func TestValidateRules_跨规则关键词重复被拒(t *testing.T) {
	r := validRules()
	r.Custom = []model.CustomRule{{ID: "c1", Keywords: []string{"帮助"}, Reply: "x", Enabled: true}}
	err := validateRules(r)
	if err == nil || !strings.Contains(err.Error(), "帮助") {
		t.Fatalf("跨规则重复应被拒绝且提示包含关键词: %v", err)
	}
}

func TestValidateRules_启用但无触发词被拒(t *testing.T) {
	r := validRules()
	r.Help.Keywords = nil
	err := validateRules(r)
	if err == nil || !strings.Contains(err.Error(), "使用指南") {
		t.Fatalf("启用但无触发词应被拒绝: %v", err)
	}
}

func TestValidateRules_启用但空回复被拒(t *testing.T) {
	r := validRules()
	r.Custom = []model.CustomRule{{ID: "c1", Keywords: []string{"价格"}, Reply: "  ", Enabled: true}}
	err := validateRules(r)
	if err == nil || !strings.Contains(err.Error(), "自定义规则#1") {
		t.Fatalf("启用但空回复应被拒绝: %v", err)
	}
}

func TestValidateRules_停用的空规则放行(t *testing.T) {
	r := validRules()
	r.Custom = []model.CustomRule{{ID: "c1", Keywords: []string{"价格"}, Reply: "", Enabled: false}}
	if err := validateRules(r); err != nil {
		t.Fatalf("停用规则允许暂存空内容: %v", err)
	}
}

// ---------- 激活码生命周期（文件级，无需 Redis） ----------

func newTestLicenseService(t *testing.T) *LicenseService {
	t.Helper()
	return NewLicenseService(t.TempDir())
}

func TestLicense_生成的码格式与数量(t *testing.T) {
	s := newTestLicenseService(t)
	codes, err := s.GenerateCodes(5, 30, 90)
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if len(codes) != 5 {
		t.Fatalf("应生成 5 枚，实际 %d", len(codes))
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != 14 || strings.Count(c, "-") != 2 {
			t.Fatalf("码格式应为 XXXX-XXXX-XXXX: %s", c)
		}
		if seen[c] {
			t.Fatalf("码重复: %s", c)
		}
		seen[c] = true
		if strings.ContainsAny(c, "0O1I") {
			t.Fatalf("码不应包含易混字符 0/O/1/I: %s", c)
		}
	}
}

func TestLicense_兑换一次性(t *testing.T) {
	s := newTestLicenseService(t)
	codes, _ := s.GenerateCodes(1, 30, 90)
	days, err := s.Consume(codes[0], "acc_1", "proj_1")
	if err != nil || days != 30 {
		t.Fatalf("首次兑换应成功返回 30 天: days=%d err=%v", days, err)
	}
	if _, err := s.Consume(codes[0], "acc_2", "proj_2"); err != ErrLicenseUsed {
		t.Fatalf("二次兑换应报已使用: %v", err)
	}
}

func TestLicense_兑换期过期(t *testing.T) {
	dir := t.TempDir()
	s := NewLicenseService(dir)
	// 手工写入一枚已过兑换期的码
	expired := `[
		{"code": "AAAA-BBBB-CCCC", "duration_days": 30, "redeem_deadline": 1, "status": "unused", "created_at": 1}
	]`
	if err := os.WriteFile(filepath.Join(dir, "licenses.json"), []byte(expired), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Consume("AAAA-BBBB-CCCC", "acc_1", "p"); err != ErrLicenseExpired {
		t.Fatalf("逾期码应报已过期: %v", err)
	}
}

func TestLicense_作废后不可用(t *testing.T) {
	s := newTestLicenseService(t)
	codes, _ := s.GenerateCodes(1, 30, 90)
	if err := s.VoidCode(codes[0]); err != nil {
		t.Fatalf("作废失败: %v", err)
	}
	if _, err := s.Consume(codes[0], "acc_1", "p"); err != ErrLicenseVoid {
		t.Fatalf("作废码兑换应报已作废: %v", err)
	}
	if err := s.VoidCode(codes[0]); err == nil {
		t.Fatalf("已使用/作废的码不应再次作废成功")
	}
}

func TestComputeExpiry_与激活码时长联动(t *testing.T) {
	now := time.Now().Unix()
	got := ComputeExpiry(now, 7)
	if got != now+7*86400 {
		t.Fatalf("7 天码应到期于 now+7d: got %d", got)
	}
}
