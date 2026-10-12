package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wechat-mp/internal/model"
)

func TestRecordServiceLifecycleAndTenantIsolation(t *testing.T) {
	configDir := t.TempDir()
	svc := NewRecordService(configDir, 90)
	now := time.Now().Unix()

	web := &model.CodeRecord{
		Code:           "123456",
		PollToken:      "poll-secret",
		RecordID:       "rec_web",
		Source:         RecordSourceWeb,
		Project:        "shop",
		ProjectName:    "商城",
		OwnerAccountID: "acc_a",
		Status:         model.StatusPending,
		CreatedAt:      now,
		ExpiresAt:      now + 180,
	}
	if err := svc.RecordCreated(web); err != nil {
		t.Fatal(err)
	}
	web.Status = model.StatusVerified
	web.OpenID = "openid_a"
	web.VerifiedAt = now + 10
	if err := svc.RecordVerified(web); err != nil {
		t.Fatal(err)
	}

	wechat := &model.CodeRecord{
		Code:        "654321",
		RecordID:    "rec_wechat",
		Source:      RecordSourceWechat,
		Project:     "universal",
		ProjectName: "通用登录",
		Status:      model.StatusPending,
		OpenID:      "openid_b",
		CreatedAt:   now,
		ExpiresAt:   now + 180,
	}
	if err := svc.RecordCreated(wechat); err != nil {
		t.Fatal(err)
	}

	admin, err := svc.List(RecordQuery{Page: 1, PageSize: 30}, model.RoleAdmin, "acc_admin")
	if err != nil {
		t.Fatal(err)
	}
	if admin.Total != 2 {
		t.Fatalf("管理员应看到 2 条，实际 %d", admin.Total)
	}
	var gotWeb *model.VerificationRecord
	for i := range admin.Items {
		if admin.Items[i].ID == "rec_web" {
			gotWeb = &admin.Items[i]
		}
	}
	if gotWeb == nil || gotWeb.Status != model.StatusVerified || gotWeb.OpenID != "openid_a" || gotWeb.Code != "123456" {
		t.Fatalf("网页取码最新核销快照不正确: %#v", gotWeb)
	}

	tenantA, err := svc.List(RecordQuery{Page: 1, PageSize: 30}, model.RoleTenant, "acc_a")
	if err != nil {
		t.Fatal(err)
	}
	if tenantA.Total != 1 || tenantA.Items[0].ID != "rec_web" {
		t.Fatalf("租户只能看到自己项目流水: %#v", tenantA.Items)
	}
	tenantB, err := svc.List(RecordQuery{Page: 1, PageSize: 30}, model.RoleTenant, "acc_b")
	if err != nil {
		t.Fatal(err)
	}
	if tenantB.Total != 0 {
		t.Fatalf("无归属的通用码不应对租户展示: %#v", tenantB.Items)
	}

	path := filepath.Join(configDir, "records", time.Now().Format("2006-01")+".jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "123456") || strings.Contains(string(raw), "poll-secret") {
		t.Fatal("流水文件应保留验证码用于审计，但不得包含 poll_token")
	}
}

func TestRecordServiceDerivesExpiredStatus(t *testing.T) {
	svc := NewRecordService(t.TempDir(), 90)
	now := time.Now().Unix()
	record := &model.CodeRecord{
		RecordID:  "rec_expired",
		Source:    RecordSourceWeb,
		Project:   "shop",
		Status:    model.StatusPending,
		CreatedAt: now - 60,
		ExpiresAt: now - 1,
	}
	if err := svc.RecordCreated(record); err != nil {
		t.Fatal(err)
	}
	result, err := svc.List(RecordQuery{Status: model.StatusExpired, Page: 1, PageSize: 30}, model.RoleAdmin, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Items[0].Status != model.StatusExpired {
		t.Fatalf("应动态计算为 expired: %#v", result.Items)
	}
}

func TestRecordServicePaginationUpperBoundWithRealFile(t *testing.T) {
	svc := NewRecordService(t.TempDir(), 90)
	now := time.Now().Unix()
	for i := 0; i < 105; i++ {
		record := &model.CodeRecord{
			RecordID:       "rec_page_" + time.Unix(0, int64(i)).Format("150405.000000000"),
			Source:         RecordSourceWeb,
			Project:        "shop",
			OwnerAccountID: "acc_a",
			Status:         model.StatusPending,
			CreatedAt:      now + int64(i),
			ExpiresAt:      now + 3600,
		}
		if err := svc.RecordCreated(record); err != nil {
			t.Fatal(err)
		}
	}

	first, err := svc.List(RecordQuery{Page: 1, PageSize: 999}, model.RoleAdmin, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 105 || first.PageSize != 100 || len(first.Items) != 100 {
		t.Fatalf("分页上限应固定为 100: total=%d page_size=%d items=%d", first.Total, first.PageSize, len(first.Items))
	}
	second, err := svc.List(RecordQuery{Page: 2, PageSize: 100}, model.RoleAdmin, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 5 {
		t.Fatalf("第二页应剩余 5 条，实际 %d", len(second.Items))
	}
}

func TestRecordServiceReturnsAndSearchesPermanentUserNumber(t *testing.T) {
	configDir := t.TempDir()
	identities, err := NewUserIdentityService(configDir)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewRecordService(configDir, 90, identities)
	now := time.Now().Unix()
	record := &model.CodeRecord{
		RecordID: "rec_numbered", Source: RecordSourceWechat, Status: model.StatusPending,
		OpenID: "openid_numbered", CreatedAt: now, ExpiresAt: now + 180,
	}
	if err := svc.RecordCreated(record); err != nil {
		t.Fatal(err)
	}

	for _, query := range []string{"1001", "用户1001", "openid_numbered"} {
		result, err := svc.List(RecordQuery{OpenID: query, Page: 1, PageSize: 30}, model.RoleAdmin, "")
		if err != nil {
			t.Fatal(err)
		}
		if result.Total != 1 || result.Items[0].UserNumber != 1001 {
			t.Fatalf("应能用 %q 查到用户1001: %#v", query, result.Items)
		}
	}
}
