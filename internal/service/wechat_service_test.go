package service

import (
	"strings"
	"testing"

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
