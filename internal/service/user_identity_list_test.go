package service

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"wechat-mp/internal/model"
)

func TestRegisteredUsersReadOnlyAndAccountLink(t *testing.T) {
	s, err := NewUserIdentityService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"openid_admin", "mock_wx_user_35152", "openid_reader"} {
		if _, err := s.GetOrCreate(id, int64(100+i)); err != nil {
			t.Fatal(err)
		}
	}
	before, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	accounts := []model.Account{
		{OpenID: "openid_admin", Role: model.RoleAdmin, Status: model.AccountActive},
		{OpenID: "openid_reader", Role: model.RoleTenant, Status: model.AccountDisabled},
		{OpenID: "not_in_registry", Role: model.RoleTenant},
	}
	all := s.List(UserIdentityQuery{}, accounts)
	if all.Total != 3 || all.KnownCount != 3 || all.AccountCount != 2 || all.MockCount != 1 || all.NextNumber != 1004 {
		t.Fatalf("登记与平台注册统计必须分离: %+v", all)
	}
	if all.Items[0].UserNumber != 1001 || !all.Items[0].Registered || all.Items[0].Role != model.RoleAdmin || all.Items[1].Registered || all.Items[1].Kind != "mock" || all.Items[2].Status != model.AccountDisabled {
		t.Fatalf("编号与账号关联错误: %+v", all.Items)
	}
	queries := []struct {
		query  UserIdentityQuery
		number int64
	}{
		{UserIdentityQuery{Registration: "unregistered"}, 1002},
		{UserIdentityQuery{Kind: "mock"}, 1002},
		{UserIdentityQuery{Keyword: "用户1003"}, 1003},
		{UserIdentityQuery{Keyword: "1001"}, 1001},
		{UserIdentityQuery{Keyword: "OPENID_READER", Registration: "registered", Kind: "ordinary"}, 1003},
	}
	for _, test := range queries {
		result := s.List(test.query, accounts)
		if result.Total != 1 || result.Items[0].UserNumber != test.number || result.NextNumber != 1004 || result.KnownCount != 3 {
			t.Fatalf("筛选不能重排编号或改变全局统计: %+v => %+v", test.query, result)
		}
	}
	if got := s.List(UserIdentityQuery{Keyword: "absent", Page: 999}, accounts); got.Page != 1 || len(got.Items) != 0 {
		t.Fatalf("空查询分页错误: %+v", got)
	}
	after, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("只读列表修改了永久编号文件")
	}
	if _, exists := s.Lookup("not_in_registry"); exists {
		t.Fatal("读取时不应自动为未知账号创建编号")
	}
}

func TestRegisteredUsersPaginationWith105PersistedIdentities(t *testing.T) {
	dir := t.TempDir()
	s, err := NewUserIdentityService(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 105; i++ {
		if _, err := s.GetOrCreate(fmt.Sprintf("openid_%03d", i), int64(i+100)); err != nil {
			t.Fatal(err)
		}
	}
	// 重新从真实落盘的数据加载，检查 100 上限与尾页，不使用内存伪造列表。
	s, err = NewUserIdentityService(dir)
	if err != nil {
		t.Fatal(err)
	}
	first := s.List(UserIdentityQuery{PageSize: 999}, nil)
	if first.Total != 105 || first.Page != 1 || first.PageSize != 100 || len(first.Items) != 100 || first.Items[99].UserNumber != 1100 {
		t.Fatalf("100人上限错误: %+v", first)
	}
	last := s.List(UserIdentityQuery{Page: int(^uint(0) >> 1), PageSize: 100}, nil)
	if last.Page != 2 || len(last.Items) != 5 || last.Items[0].UserNumber != 1101 || last.Items[4].UserNumber != 1105 {
		t.Fatalf("尾页或极大页码错误: %+v", last)
	}
}
