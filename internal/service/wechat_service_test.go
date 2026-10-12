package service

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"wechat-mp/internal/model"
)

// ---------- 消息路由（匹配优先级） ----------

func rulesForMatch() *model.RulesConfig {
	return &model.RulesConfig{
		UniversalCode: model.BuiltinRule{Enabled: true, Keywords: []string{"验证码", "取码"}},
		Help:          model.BuiltinRule{Enabled: false, Keywords: []string{"帮助"}},
		Projects:      model.BuiltinRule{Enabled: true, Keywords: []string{"项目"}},
		Custom: []model.CustomRule{
			{ID: "c1", Keywords: []string{"价格"}, Reply: "报价内容", Enabled: true},
			{ID: "c2", Keywords: []string{"客服"}, Reply: "客服内容", Enabled: false},
		},
	}
}

func TestMatchTextRule_六位数字最优先(t *testing.T) {
	s := &WechatService{}
	if got := s.matchTextRule(rulesForMatch(), "839201"); got != "verify_code" {
		t.Fatalf("6 位数字应命中核验: %s", got)
	}
}

func TestMatchTextRule_内置关键词命中(t *testing.T) {
	s := &WechatService{}
	r := rulesForMatch()
	if got := s.matchTextRule(r, "取码"); got != "universal_code" {
		t.Fatalf("取码应命中通行码: %s", got)
	}
}

func TestMatchTextRule_停用规则跳过(t *testing.T) {
	s := &WechatService{}
	r := rulesForMatch() // help 已停用
	if got := s.matchTextRule(r, "帮助"); got != "fallback" {
		t.Fatalf("停用的规则应被跳过落到兜底: %s", got)
	}
}

func TestMatchTextRule_自定义规则命中与优先序(t *testing.T) {
	s := &WechatService{}
	if got := s.matchTextRule(rulesForMatch(), "价格"); got != "custom:c1" {
		t.Fatalf("自定义关键词应命中 c1: %s", got)
	}
	// 停用的 c2 不应命中
	if got := s.matchTextRule(rulesForMatch(), "客服"); got != "fallback" {
		t.Fatalf("停用的自定义规则不应命中: %s", got)
	}
}

func TestMatchTextRule_大小写与空白容错(t *testing.T) {
	s := &WechatService{}
	r := rulesForMatch()
	r.UniversalCode.Keywords = []string{"Code"}
	if got := s.matchTextRule(r, " code "); got != "universal_code" {
		t.Fatalf("大小写与首尾空白应容错: %s", got)
	}
}

func TestMatchTextRule_兜底(t *testing.T) {
	s := &WechatService{}
	if got := s.matchTextRule(rulesForMatch(), "你好"); got != "fallback" {
		t.Fatalf("未命中任何规则应落兜底: %s", got)
	}
}

// ---------- 核销成功文案 ----------

func TestFormatSuccessMessage_带网址原样替换(t *testing.T) {
	got := formatSuccessMessage("恭喜！【{project_name}】验证成功。\n网址：{project_url}", "商城", "https://shop.example.com")
	if !strings.Contains(got, "商城") || !strings.Contains(got, "https://shop.example.com") {
		t.Fatalf("应替换项目名与网址: %s", got)
	}
}

func TestFormatSuccessMessage_无网址时微创消融(t *testing.T) {
	tpl := "恭喜！【{project_name}】验证成功。\n网址：{project_url}\n您的身份已核销。"
	got := formatSuccessMessage(tpl, "商城", "")
	if strings.Contains(got, "{project_url}") {
		t.Fatalf("未填网址时变量应被消融: %s", got)
	}
	if !strings.Contains(got, "恭喜") || !strings.Contains(got, "已核销") {
		t.Fatalf("主句应保留: %s", got)
	}
}

func TestFormatUniversalCodeMessageUsesRecordExpiry(t *testing.T) {
	record := &model.CodeRecord{Code: "123456", CreatedAt: 1_700_000_000, ExpiresAt: 1_700_000_300}
	got := formatUniversalCodeMessage("码：{code}\n有效期：{expire_minutes} 分钟/{expire_seconds} 秒\n截止：{expire_time}", record, 180)
	if !strings.Contains(got, "码：123456") || !strings.Contains(got, "5 分钟/300 秒") {
		t.Fatalf("没有使用验证码记录自身的有效期: %q", got)
	}
	wantTime := time.Unix(record.ExpiresAt, 0).In(time.FixedZone("北京时间", 8*60*60)).Format("15:04:05")
	if !strings.Contains(got, "截止："+wantTime) {
		t.Fatalf("真实截止时间渲染错误: got %q want %q", got, wantTime)
	}
}

// ---------- 项目清单排版 ----------

func TestFormatProjectListText_连续编号并用空行分隔(t *testing.T) {
	now := time.Now().Unix()
	projects := []*model.ProjectItem{
		{ID: "first", Name: "新增测试项目", URL: " https://first.example.com ", Enabled: true},
		{ID: "disabled", Name: "已停用项目", Enabled: false},
		{ID: "expired", Name: "已过期项目", Enabled: true, ExpiresAt: now - 1},
		{ID: "test01", Name: "测试用户项目01", Enabled: true, ExpiresAt: now + 60},
	}
	want := "1. 新增测试项目\n   网址：https://first.example.com\n\n2. 测试用户项目01\n   项目标识：test01"
	if got := formatProjectListText(projects); got != want {
		t.Fatalf("项目清单排版不符合预期:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatProjectListText_无可用项目(t *testing.T) {
	projects := []*model.ProjectItem{{ID: "disabled", Name: "已停用项目", Enabled: false}}
	if got := formatProjectListText(projects); got != "暂无已启用的业务平台" {
		t.Fatalf("无可用项目时的提示错误: %q", got)
	}
}

func TestPreviewReply_项目清单使用新排版(t *testing.T) {
	projectService := NewProjectService(t.TempDir())
	projects := []*model.ProjectItem{
		{ID: "first", Name: "新增测试项目", URL: "https://first.example.com", Enabled: true},
		{ID: "test01", Name: "测试用户项目01", URL: "https://second.example.com", Enabled: true},
	}
	data, err := json.Marshal(projects)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectService.projectsPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	service := &WechatService{projectService: projectService}
	draft := &model.ReplySettings{Replies: model.RepliesConfig{Projects: "🌐【当前已支持的业务平台】\n{project_list}\n更多项目持续接入中..."}}
	preview, err := service.PreviewReply(context.Background(), draft, "projects", "", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	want := "🌐【当前已支持的业务平台】\n1. 新增测试项目\n   网址：https://first.example.com\n\n2. 测试用户项目01\n   网址：https://second.example.com\n更多项目持续接入中..."
	if preview.Reply != want {
		t.Fatalf("项目清单预览排版不符合预期:\ngot:\n%s\nwant:\n%s", preview.Reply, want)
	}
}
