package service

import (
	"context"
	"strings"
	"testing"

	"wechat-mp/internal/config"
	"wechat-mp/internal/model"
	"wechat-mp/internal/storage"
)

type fakePreviewCodeReader struct {
	record *model.CodeRecord
	err    error
}

func (f *fakePreviewCodeReader) GetCode(context.Context, string) (*model.CodeRecord, error) {
	return f.record, f.err
}

func newReplyEditorTestService(t *testing.T) (*ProjectService, *WechatService) {
	t.Helper()
	ps := NewProjectService(t.TempDir())
	ws := &WechatService{
		cfg:            &config.Config{DefaultCodeExpire: 180},
		projectService: ps,
	}
	return ps, ws
}

func TestReplySettingsSaveUsesRevisionAndDisablesLegacyFooter(t *testing.T) {
	ps, _ := newReplyEditorTestService(t)
	settings, err := ps.GetReplySettings()
	if err != nil {
		t.Fatal(err)
	}
	originalRevision := settings.Revision
	settings.Replies.Help = "新的帮助回复"
	settings.Rules.Custom = append(settings.Rules.Custom, model.CustomRule{
		ID:           "custom_test",
		Keywords:     []string{"测试规则"},
		Reply:        "测试回复",
		AppendFooter: true,
		Enabled:      true,
	})
	if err := ps.SaveReplySettings(settings); err != nil {
		t.Fatal(err)
	}
	if settings.Revision == originalRevision {
		t.Fatal("保存后 revision 未更新")
	}
	if settings.Rules.Custom[0].AppendFooter {
		t.Fatal("旧 append_footer 开关仍被保留")
	}

	stale := *settings
	stale.Revision = originalRevision
	if err := ps.SaveReplySettings(&stale); err != ErrReplyRevision {
		t.Fatalf("旧 revision 应被拒绝，实际错误: %v", err)
	}
}

func TestCommonLibraryNeverAutoAppendsToProjectReply(t *testing.T) {
	ps, ws := newReplyEditorTestService(t)
	project, err := ps.AddProject(&model.AddProjectReq{
		ID:            "demo",
		Name:          "演示项目",
		CustomAdTitle: "项目须知",
		CustomAd:      "只属于演示项目的补充信息",
		IncludeGlobal: true,
	}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if project.IncludeGlobal {
		t.Fatal("新项目不应继续启用公共尾注")
	}

	settings, err := ps.GetReplySettings()
	if err != nil {
		t.Fatal(err)
	}
	preview, err := ws.PreviewReply(context.Background(), settings, "code_success", "", project.ID, "839201", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview.Reply, "只属于演示项目的补充信息") {
		t.Fatalf("项目补充信息未显示: %q", preview.Reply)
	}
	if !strings.Contains(preview.Reply, "项目须知\n\n只属于演示项目的补充信息") {
		t.Fatalf("项目补充标题和内容没有用空行分区: %q", preview.Reply)
	}
	if strings.Contains(preview.Reply, "-------------------") || strings.Contains(preview.Reply, "【项目须知】") {
		t.Fatalf("项目补充信息仍含写死的横线或括号: %q", preview.Reply)
	}
	if strings.Contains(preview.Reply, "推荐站点") || strings.Contains(preview.Reply, "站长微信") {
		t.Fatalf("公共内容被错误追加到项目回复: %q", preview.Reply)
	}
}

func TestPreviewUsesExampleDataWithoutStorage(t *testing.T) {
	ps, ws := newReplyEditorTestService(t)
	settings, err := ps.GetReplySettings()
	if err != nil {
		t.Fatal(err)
	}
	preview, err := ws.PreviewReply(context.Background(), settings, "", "验证码", "", "246810", false)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Matched != "universal_code" {
		t.Fatalf("命中规则错误: %s", preview.Matched)
	}
	if !strings.Contains(preview.Reply, "246810") {
		t.Fatalf("示例验证码未渲染: %q", preview.Reply)
	}
	if !strings.Contains(preview.Reply, "3 分钟") || strings.Contains(preview.Reply, "{expire_time}") {
		t.Fatalf("有效期动态变量未完整渲染: %q", preview.Reply)
	}
}

func TestLiveCodePreviewUsesRealNotFoundStateAndDraftReply(t *testing.T) {
	ps, ws := newReplyEditorTestService(t)
	settings, err := ps.GetReplySettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.Replies.CodeInvalid = "草稿提示：验证码 {code} 当前无效"
	ws.previewCodeReader = &fakePreviewCodeReader{err: storage.ErrCodeNotFound}

	preview, err := ws.PreviewReply(context.Background(), settings, "", "123456", "", "123456", true)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Matched != "code_invalid" || preview.CodeState != "not_found_or_expired" || !preview.LiveCodeState {
		t.Fatalf("不存在的真实验证码分支错误: %#v", preview)
	}
	if preview.Reply != "草稿提示：验证码 123456 当前无效" {
		t.Fatalf("真实状态试跑没有使用当前草稿文案: %q", preview.Reply)
	}
}

func TestLiveCodePreviewPendingUsesRealProjectWithoutConsuming(t *testing.T) {
	ps, ws := newReplyEditorTestService(t)
	project, err := ps.AddProject(&model.AddProjectReq{
		ID: "live_demo", Name: "真实测试项目", URL: "https://example.com/live",
		CustomAdTitle: "项目提示", CustomAd: "真实项目补充信息",
	}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := ps.GetReplySettings()
	if err != nil {
		t.Fatal(err)
	}
	record := &model.CodeRecord{Code: "123456", Project: project.ID, Status: model.StatusPending}
	ws.previewCodeReader = &fakePreviewCodeReader{record: record}

	preview, err := ws.PreviewReply(context.Background(), settings, "", "123456", "", "123456", true)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Matched != "code_success" || preview.CodeState != "pending" {
		t.Fatalf("待核销真实验证码没有进入成功预览: %#v", preview)
	}
	if !strings.Contains(preview.Reply, "真实测试项目") || !strings.Contains(preview.Reply, "真实项目补充信息") {
		t.Fatalf("成功预览没有使用真实项目资料: %q", preview.Reply)
	}
	if record.Status != model.StatusPending || record.OpenID != "" || record.VerifiedAt != 0 {
		t.Fatalf("只读试跑意外修改了真实验证码: %#v", record)
	}
}

func TestLiveCodePreviewUsesRealUsedAndProjectUnavailableBranches(t *testing.T) {
	ps, ws := newReplyEditorTestService(t)
	project, err := ps.AddProject(&model.AddProjectReq{ID: "state_demo", Name: "状态项目"}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := ps.GetReplySettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.Replies.CodeUsed = "草稿重复提示 {code}"

	ws.previewCodeReader = &fakePreviewCodeReader{record: &model.CodeRecord{Code: "123456", Project: project.ID, Status: model.StatusVerified}}
	used, err := ws.PreviewReply(context.Background(), settings, "", "123456", "", "123456", true)
	if err != nil {
		t.Fatal(err)
	}
	if used.Matched != "code_used" || used.CodeState != "used" || used.Reply != "草稿重复提示 123456" {
		t.Fatalf("已核销真实验证码分支错误: %#v", used)
	}

	if err := ps.ToggleProject(project.ID, false); err != nil {
		t.Fatal(err)
	}
	unavailable, err := ws.PreviewReply(context.Background(), settings, "", "123456", "", "123456", true)
	if err != nil {
		t.Fatal(err)
	}
	if unavailable.Matched != "code_project_unavailable" || unavailable.CodeState != "project_unavailable" {
		t.Fatalf("停用项目的真实验证码分支错误: %#v", unavailable)
	}
}

func TestReplySnippetIsCopiedNotReferenced(t *testing.T) {
	ps, _ := newReplyEditorTestService(t)
	item := &model.ReplySnippet{Title: "客服", Content: "客服联系方式 A"}
	if err := ps.SaveReplySnippet(item); err != nil {
		t.Fatal(err)
	}
	settings, err := ps.GetReplySettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.Replies.Help = item.Content
	if err := ps.SaveReplySettings(settings); err != nil {
		t.Fatal(err)
	}
	if err := ps.DeleteReplySnippet(item.ID); err != nil {
		t.Fatal(err)
	}
	after, err := ps.GetReplySettings()
	if err != nil {
		t.Fatal(err)
	}
	if after.Replies.Help != "客服联系方式 A" {
		t.Fatalf("删除内容库条目影响了已保存回复: %q", after.Replies.Help)
	}
}

func TestReplySnippetReorderPersists(t *testing.T) {
	ps, _ := newReplyEditorTestService(t)
	first := &model.ReplySnippet{Title: "第一条", Content: "A"}
	second := &model.ReplySnippet{Title: "第二条", Content: "B"}
	if err := ps.SaveReplySnippet(first); err != nil {
		t.Fatal(err)
	}
	if err := ps.SaveReplySnippet(second); err != nil {
		t.Fatal(err)
	}
	library, err := ps.ListReplySnippets()
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(library))
	ids = append(ids, second.ID, first.ID)
	for _, item := range library {
		if item.ID != first.ID && item.ID != second.ID {
			ids = append(ids, item.ID)
		}
	}
	if err := ps.ReorderReplySnippets(ids); err != nil {
		t.Fatal(err)
	}
	after, err := ps.ListReplySnippets()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) < 2 || after[0].ID != second.ID || after[1].ID != first.ID {
		t.Fatalf("排序未持久化: %#v", after)
	}
}

func TestSnippetSectionUsesLocalNumbersAndEditableTitle(t *testing.T) {
	section := model.ReplySnippetSection{
		Title: "其他说明",
		Items: []model.ReplySnippetSelection{
			{SourceID: "item_3", Title: "服务维护公告", Content: "温馨提示：系统运行平稳。"},
			{SourceID: "item_4", Title: "管理员尾注", Content: "管理员尾注内容"},
		},
	}
	want := "项目清单\n\n其他说明\n\n1. 服务维护公告\n   温馨提示：系统运行平稳。\n\n2. 管理员尾注\n   管理员尾注内容"
	if got := formatReplySnippetSection("项目清单", section); got != want {
		t.Fatalf("常用内容分区排版错误:\ngot:\n%s\nwant:\n%s", got, want)
	}
	section.Title = ""
	got := formatReplySnippetSection("项目清单", section)
	if strings.Contains(got, "其他说明") || !strings.Contains(got, "项目清单\n\n1. 服务维护公告") {
		t.Fatalf("标题留空时应只用空行分隔: %q", got)
	}
}

func TestPreviewRendersSavedSnippetSelectionWithoutLibraryDependency(t *testing.T) {
	ps, ws := newReplyEditorTestService(t)
	settings, err := ps.GetReplySettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.Replies.Projects = "当前已支持的业务平台\n\n{project_list}"
	settings.SnippetSections["projects"] = model.ReplySnippetSection{
		Title: "补充信息",
		Items: []model.ReplySnippetSelection{
			{SourceID: "ft_notice", Title: "服务维护公告", Content: "温馨提示：系统运行平稳。"},
			{SourceID: "ft_admin", Title: "管理员尾注", Content: "管理员尾注内容"},
		},
	}
	if err := ps.SaveReplySettings(settings); err != nil {
		t.Fatal(err)
	}
	preview, err := ws.PreviewReply(context.Background(), nil, "projects", "", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"\n\n补充信息\n\n1. 服务维护公告", "\n\n2. 管理员尾注"} {
		if !strings.Contains(preview.Reply, expected) {
			t.Fatalf("预览未渲染已保存常用内容: %q", preview.Reply)
		}
	}
	if strings.ContainsAny(preview.Reply, "🌐📌📢") || strings.Contains(preview.Reply, "【03｜") {
		t.Fatalf("预览仍含旧图标或库内固定序号: %q", preview.Reply)
	}
	if actual := ws.handleText(context.Background(), "test-openid", "项目"); actual != preview.Reply {
		t.Fatalf("真实消息与预览的常用内容排版不一致:\nactual:\n%s\npreview:\n%s", actual, preview.Reply)
	}
}

func TestReplyEditorV2MigratesExactLegacySnippetSuffix(t *testing.T) {
	dir := t.TempDir()
	ps := NewProjectService(dir)
	library := []model.ReplySnippet{
		{ID: "item_1", Title: "内容1", Content: "正文1"},
		{ID: "item_2", Title: "内容2", Content: "正文2"},
		{ID: "item_3", Title: "服务维护公告", Content: "📢 温馨提示：请稍后重试。"},
		{ID: "item_4", Title: "管理员尾注", Content: "管理员尾注内容"},
	}
	if err := writeReplyJSON(ps.replyLibraryPath, library); err != nil {
		t.Fatal(err)
	}
	settings, err := ps.GetReplySettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.Version = 1
	settings.SnippetSections = nil
	settings.Replies.CodeSuccess = "✅ 恭喜！【{project_name}】验证成功。\n网址：{project_url}\n您的身份已核销。"
	settings.Replies.Projects = "🌐【当前已支持的业务平台】\n{project_list}\n\n【03｜服务维护公告】\n📢 温馨提示：请稍后重试。\n\n【04｜管理员尾注】\n管理员尾注内容"
	if err := writeReplyJSON(ps.replySettingsPath, settings); err != nil {
		t.Fatal(err)
	}

	migrated := NewProjectService(dir)
	got, err := migrated.GetReplySettings()
	if err != nil {
		t.Fatal(err)
	}
	section := got.SnippetSections["projects"]
	if got.Version != 2 || len(section.Items) != 2 || section.Items[0].SourceID != "item_3" || section.Items[1].SourceID != "item_4" {
		t.Fatalf("旧式常用内容未正确迁移: %#v", got)
	}
	if strings.Contains(got.Replies.Projects, "【03｜") || strings.Contains(got.Replies.Projects, "🌐") {
		t.Fatalf("旧区块或项目标题图标未移除: %q", got.Replies.Projects)
	}
	if strings.Contains(got.Replies.CodeSuccess, "✅") || strings.Contains(got.Replies.CodeSuccess, "【{project_name}】") {
		t.Fatalf("核销成功旧默认图标或括号未移除: %q", got.Replies.CodeSuccess)
	}
	if section.Items[0].Content != "温馨提示：请稍后重试。" {
		t.Fatalf("迁移副本未去掉默认图标: %#v", section.Items[0])
	}
}
