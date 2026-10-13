package service

import (
	"testing"
	"time"

	"wechat-mp/internal/model"
)

func TestRecordProjectOptionsRetentionPermissionsAndLatestSnapshot(t *testing.T) {
	s := NewRecordService(t.TempDir(), 90)
	now := time.Now().Unix()
	projects := []*model.ProjectItem{
		{ID: "current", Name: "当前项目", OwnerAccountID: "acc_a", Enabled: false, ExpiresAt: now - 1},
		{ID: "reused", Name: "其他租户的新名字", OwnerAccountID: "acc_b"},
		{ID: "foreign", Name: "其他租户项目", OwnerAccountID: "acc_b"},
	}
	records := []*model.CodeRecord{
		{RecordID: "old", Project: "deleted", ProjectName: "删除前旧名称", OwnerAccountID: "acc_a", CreatedAt: now - 100},
		{RecordID: "latest", Project: "deleted", ProjectName: "删除前新名称", OwnerAccountID: "acc_a", CreatedAt: now - 10},
		{RecordID: "reused", Project: "reused", ProjectName: "自己的历史名称", OwnerAccountID: "acc_a", CreatedAt: now - 20},
		{RecordID: "foreign", Project: "foreign", ProjectName: "别人的快照", OwnerAccountID: "acc_b", CreatedAt: now - 30},
		{RecordID: "expired-retention", Project: "outside", ProjectName: "91天外", OwnerAccountID: "acc_a", CreatedAt: now - 91*86400},
		{RecordID: "unassigned", Project: "universal", Source: RecordSourceWechat, CreatedAt: now},
		{RecordID: "changed", Project: "universal", Source: RecordSourceWechat, CreatedAt: now - 5},
	}
	for _, r := range records {
		r.Status = model.StatusPending
		if err := s.RecordCreated(r); err != nil {
			t.Fatal(err)
		}
	}
	// 一条通用码已核销后只看最新快照，不能在下拉留一个“universal”项目。
	records[6].Project, records[6].ProjectName, records[6].OwnerAccountID, records[6].Status = "current", "旧项目名称", "acc_a", model.StatusVerified
	if err := s.RecordVerified(records[6]); err != nil {
		t.Fatal(err)
	}
	tenant, err := s.List(RecordQuery{Project: "deleted", PageSize: 1, Status: model.StatusPending}, model.RoleTenant, "acc_a", projects)
	if err != nil {
		t.Fatal(err)
	}
	options := map[string]model.RecordProjectOption{}
	for _, option := range tenant.ProjectOptions {
		options[option.ID] = option
	}
	if tenant.Total != 2 || len(tenant.Items) != 1 || len(options) != 3 || tenant.HasUnassigned {
		t.Fatalf("选项不能被分页/其他筛选截断，不能泄露别人的项目: %+v", tenant)
	}
	if options["current"].Name != "当前项目" || options["current"].Deleted || options["current"].Historical {
		t.Fatal("停用/过期的当前项目仍应可选且以当前名称展示")
	}
	if options["deleted"].Name != "删除前新名称" || !options["deleted"].Deleted || !options["deleted"].Historical {
		t.Fatal("删除项目应取最近可见快照名称")
	}
	if options["reused"].Name != "自己的历史名称" || options["reused"].Deleted || !options["reused"].Historical {
		t.Fatal("复用ID不能泄露别人的新名称，不能误标已删除")
	}
	for _, id := range []string{"foreign", "outside", "universal"} {
		if _, ok := options[id]; ok {
			t.Fatalf("不该出现的选项: %s", id)
		}
	}
	admin, err := s.List(RecordQuery{Unassigned: true}, model.RoleAdmin, "", projects)
	if err != nil {
		t.Fatal(err)
	}
	if !admin.HasUnassigned || admin.Total != 1 || admin.Items[0].ID != "unassigned" {
		t.Fatalf("未确定项目应只匹配无项目的最新记录: %+v", admin)
	}
	denied, err := s.List(RecordQuery{Unassigned: true}, model.RoleTenant, "acc_a", projects)
	if err != nil || denied.Total != 0 || denied.HasUnassigned {
		t.Fatalf("租户不能请求未确定项目: %+v %v", denied, err)
	}
	// 当前同名ID过滤保留旧快照，不能覆盖流水名称。
	reused, err := s.List(RecordQuery{Project: "reused"}, model.RoleAdmin, "", projects)
	if err != nil || reused.Total != 1 || reused.Items[0].ProjectName != "自己的历史名称" {
		t.Fatalf("查询不得修改历史名称: %+v %v", reused, err)
	}
}

func TestRecordProjectOptionsWithoutRecordsDirectory(t *testing.T) {
	s := NewRecordService(t.TempDir(), 90)
	result, err := s.List(RecordQuery{}, model.RoleTenant, "acc_a", []*model.ProjectItem{
		{ID: "mine", Name: "没有流水的项目", OwnerAccountID: "acc_a"},
		{ID: "others", Name: "他人项目", OwnerAccountID: "acc_b"},
	})
	if err != nil || result.Total != 0 || len(result.ProjectOptions) != 1 || result.ProjectOptions[0].ID != "mine" {
		t.Fatalf("空流水也应显示自己的当前项目: %+v %v", result, err)
	}
}

func TestRecordProjectSlugDoesNotCollideWithAllOption(t *testing.T) {
	s := NewRecordService(t.TempDir(), 90)
	for _, id := range []string{"all", "other"} {
		if err := s.RecordCreated(&model.CodeRecord{RecordID: id, Project: id, CreatedAt: time.Now().Unix()}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := s.List(RecordQuery{Project: "all"}, model.RoleAdmin, "")
	if err != nil || result.Total != 1 || result.Items[0].Project != "all" {
		t.Fatalf("真实项目all不应误匹配全部: %+v %v", result, err)
	}
}
