package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"wechat-mp/internal/model"
)

var ErrReplyRevision = errors.New("回复配置已被其他窗口修改，请刷新后再保存")

// 旧文件留在原地作为迁移前的恢复依据；迁移只执行一次。
func (s *ProjectService) ensureReplyEditorInitialized() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(s.replySettingsPath); os.IsNotExist(err) {
		settings := model.ReplySettings{Version: 2, Revision: generateRandomHex(12), Rules: *factoryDefaultRules(), Replies: *s.defaultReplies(), SnippetSections: map[string]model.ReplySnippetSection{}}
		if data, err := os.ReadFile(s.rulesPath); err == nil {
			if err := json.Unmarshal(data, &settings.Rules); err != nil {
				log.Printf("[Replies] 旧规则读取失败: %v", err)
				return
			}
		}
		if data, err := os.ReadFile(s.repliesPath); err == nil {
			if err := json.Unmarshal(data, &settings.Replies); err != nil {
				log.Printf("[Replies] 旧文案读取失败: %v", err)
				return
			}
		}
		for i := range settings.Rules.Custom {
			settings.Rules.Custom[i].AppendFooter = false
		}
		if err := writeReplyJSON(s.replySettingsPath, settings); err != nil {
			log.Printf("[Replies] 配置迁移失败: %v", err)
		}
	}
	if _, err := os.Stat(s.replyLibraryPath); os.IsNotExist(err) {
		library := []model.ReplySnippet{}
		if data, err := os.ReadFile(s.footersPath); err == nil {
			var old []model.FooterItem
			if err := json.Unmarshal(data, &old); err != nil {
				log.Printf("[Replies] 旧内容库读取失败: %v", err)
				return
			}
			for _, item := range old {
				library = append(library, model.ReplySnippet{ID: item.ID, Title: item.Title, Content: item.Content})
			}
		}
		library = append(library, model.ReplySnippet{ID: "snippet_project_list", Title: "动态项目列表", Content: "{project_list}"})
		if err := writeReplyJSON(s.replyLibraryPath, library); err != nil {
			log.Printf("[Replies] 内容库迁移失败: %v", err)
		}
	}
	s.migrateReplyEditorV2Locked()
}

func plainDefaultSnippetContent(content string) string {
	for _, prefix := range []string{"🌟 ", "💬 ", "📢 ", "📌 "} {
		if strings.HasPrefix(content, prefix) {
			return strings.TrimPrefix(content, prefix)
		}
	}
	return content
}

func extractLegacySnippetSuffix(value string, library []model.ReplySnippet) (string, []model.ReplySnippetSelection) {
	cursor := strings.TrimRight(value, "\n")
	selected := []model.ReplySnippetSelection{}
	for {
		matched := false
		for index, item := range library {
			block := fmt.Sprintf("【%02d｜%s】\n%s", index+1, item.Title, item.Content)
			if !strings.HasSuffix(cursor, block) {
				continue
			}
			selected = append(selected, model.ReplySnippetSelection{
				SourceID: item.ID,
				Title:    strings.TrimSpace(item.Title),
				Content:  plainDefaultSnippetContent(item.Content),
			})
			cursor = strings.TrimSuffix(cursor, block)
			cursor = strings.TrimSuffix(cursor, "\n\n")
			matched = true
			break
		}
		if !matched {
			break
		}
	}
	for left, right := 0, len(selected)-1; left < right; left, right = left+1, right-1 {
		selected[left], selected[right] = selected[right], selected[left]
	}
	return strings.TrimRight(cursor, "\n"), selected
}

func (s *ProjectService) migrateReplyEditorV2Locked() {
	settingsData, err := os.ReadFile(s.replySettingsPath)
	if err != nil {
		return
	}
	var settings model.ReplySettings
	if err := json.Unmarshal(settingsData, &settings); err != nil {
		return
	}
	libraryData, err := os.ReadFile(s.replyLibraryPath)
	if err != nil {
		return
	}
	var library []model.ReplySnippet
	if err := json.Unmarshal(libraryData, &library); err != nil {
		return
	}

	settingsChanged := settings.Version < 2 || settings.SnippetSections == nil
	if settings.SnippetSections == nil {
		settings.SnippetSections = map[string]model.ReplySnippetSection{}
	}
	migrateField := func(key string, value *string) {
		clean, items := extractLegacySnippetSuffix(*value, library)
		if len(items) == 0 {
			return
		}
		*value = clean
		settings.SnippetSections[key] = model.ReplySnippetSection{Title: "补充信息", Items: items}
		settingsChanged = true
	}
	migrateField("welcome", &settings.Replies.Welcome)
	migrateField("code_success", &settings.Replies.CodeSuccess)
	migrateField("code_invalid", &settings.Replies.CodeInvalid)
	migrateField("code_used", &settings.Replies.CodeUsed)
	migrateField("universal_code", &settings.Replies.UniversalCode)
	migrateField("projects", &settings.Replies.Projects)
	migrateField("help", &settings.Replies.Help)
	migrateField("unknown_text", &settings.Replies.UnknownText)
	migrateField("media_message", &settings.Replies.MediaMessage)
	for i := range settings.Rules.Custom {
		migrateField("custom:"+settings.Rules.Custom[i].ID, &settings.Rules.Custom[i].Reply)
	}

	plainProjects := strings.Replace(settings.Replies.Projects, "🌐【当前已支持的业务平台】\n", "当前已支持的业务平台\n\n", 1)
	if plainProjects != settings.Replies.Projects {
		settings.Replies.Projects = plainProjects
		settingsChanged = true
	}
	plainSuccess := strings.Replace(settings.Replies.CodeSuccess, "✅ 恭喜！【{project_name}】验证成功。", "恭喜！{project_name}验证成功。", 1)
	if plainSuccess != settings.Replies.CodeSuccess {
		settings.Replies.CodeSuccess = plainSuccess
		settingsChanged = true
	}

	libraryChanged := false
	for i := range library {
		clean := plainDefaultSnippetContent(library[i].Content)
		if clean != library[i].Content {
			library[i].Content = clean
			libraryChanged = true
		}
	}
	if libraryChanged {
		if _, err := os.Stat(s.replyLibraryPath + ".v1.bak"); os.IsNotExist(err) {
			_ = os.WriteFile(s.replyLibraryPath+".v1.bak", libraryData, 0644)
		}
		if err := writeReplyJSON(s.replyLibraryPath, library); err != nil {
			log.Printf("[Replies] 常用内容纯文字迁移失败: %v", err)
		}
	}
	if settingsChanged {
		if _, err := os.Stat(s.replySettingsPath + ".v1.bak"); os.IsNotExist(err) {
			_ = os.WriteFile(s.replySettingsPath+".v1.bak", settingsData, 0644)
		}
		settings.Version = 2
		settings.Revision = generateRandomHex(12)
		if err := writeReplyJSON(s.replySettingsPath, settings); err != nil {
			log.Printf("[Replies] 常用内容分区迁移失败: %v", err)
		}
	}
}

// 同目录临时文件 + 原子替换，防止中途退出留下半份配置。
func writeReplyJSON(path string, value interface{}) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".reply-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(name, path); err == nil {
		return nil
	}

	// Windows 不能直接用 Rename 覆盖现有文件，先把旧文件挪到同目录再替换。
	oldPath := path + ".replace-old"
	_ = os.Remove(oldPath)
	if err := os.Rename(path, oldPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Rename(oldPath, path)
		return err
	}
	_ = os.Remove(oldPath)
	return nil
}

func (s *ProjectService) GetReplySettings() (*model.ReplySettings, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readReplySettingsLocked()
}

func (s *ProjectService) readReplySettingsLocked() (*model.ReplySettings, error) {
	data, err := os.ReadFile(s.replySettingsPath)
	if err != nil {
		return nil, err
	}
	var settings model.ReplySettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}
	if settings.Rules.Custom == nil {
		settings.Rules.Custom = []model.CustomRule{}
	}
	if settings.SnippetSections == nil {
		settings.SnippetSections = map[string]model.ReplySnippetSection{}
	}
	return &settings, nil
}

func (s *ProjectService) SaveReplySettings(settings *model.ReplySettings) error {
	if err := validateReplySettings(settings); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.readReplySettingsLocked()
	if err != nil {
		return err
	}
	if settings.Revision != current.Revision {
		return ErrReplyRevision
	}
	if err := writeReplyJSON(s.replySettingsPath+".bak", current); err != nil {
		return err
	}
	settings.Version = 2
	settings.Revision = generateRandomHex(12)
	for i := range settings.Rules.Custom {
		settings.Rules.Custom[i].AppendFooter = false
	}
	return writeReplyJSON(s.replySettingsPath, settings)
}

var replyVariablePattern = regexp.MustCompile(`\{([a-z_]+)\}`)

func validateReplySettings(settings *model.ReplySettings) error {
	if settings == nil {
		return errors.New("缺少回复配置")
	}
	if err := validateRules(&settings.Rules); err != nil {
		return err
	}
	if strings.TrimSpace(settings.Replies.CodeSuccess) == "" {
		return errors.New("验证成功回复不能为空")
	}
	if settings.Rules.UniversalCode.Enabled && !strings.Contains(settings.Replies.UniversalCode, "{code}") {
		return errors.New("公众号取码回复必须包含 {code}，否则用户无法获取验证码")
	}
	fields := map[string]string{
		"welcome": settings.Replies.Welcome, "code_success": settings.Replies.CodeSuccess,
		"code_invalid": settings.Replies.CodeInvalid, "code_used": settings.Replies.CodeUsed,
		"universal_code": settings.Replies.UniversalCode, "projects": settings.Replies.Projects,
		"help": settings.Replies.Help, "unknown_text": settings.Replies.UnknownText, "media_message": settings.Replies.MediaMessage,
	}
	for field, content := range fields {
		if strings.TrimSpace(content) == "" {
			return fmt.Errorf("%s 回复不能为空", field)
		}
		if err := validateReplyVariables(field, content); err != nil {
			return err
		}
	}
	for _, rule := range settings.Rules.Custom {
		if err := validateReplyVariables("custom", rule.Reply); err != nil {
			return err
		}
	}
	return validateSnippetSections(settings)
}

func formatReplySnippetSection(base string, section model.ReplySnippetSection) string {
	if len(section.Items) == 0 {
		return base
	}
	parts := make([]string, 0, len(section.Items))
	for index, item := range section.Items {
		contentLines := strings.Split(strings.TrimSpace(item.Content), "\n")
		for i := range contentLines {
			contentLines[i] = "   " + contentLines[i]
		}
		parts = append(parts, fmt.Sprintf("%d. %s\n%s", index+1, strings.TrimSpace(item.Title), strings.Join(contentLines, "\n")))
	}
	sectionText := strings.Join(parts, "\n\n")
	if title := strings.TrimSpace(section.Title); title != "" {
		separator := "\n\n"
		if section.CompactTitle {
			separator = "\n"
		}
		sectionText = title + separator + sectionText
	}
	base = strings.TrimRight(base, "\n")
	if strings.TrimSpace(base) == "" {
		return sectionText
	}
	return base + "\n\n" + sectionText
}

func replyBaseForSection(settings *model.ReplySettings, key string) (string, string, bool) {
	switch key {
	case "welcome":
		return settings.Replies.Welcome, key, true
	case "code_success":
		return settings.Replies.CodeSuccess, key, true
	case "code_invalid":
		return settings.Replies.CodeInvalid, key, true
	case "code_used":
		return settings.Replies.CodeUsed, key, true
	case "universal_code":
		return settings.Replies.UniversalCode, key, true
	case "projects":
		return settings.Replies.Projects, key, true
	case "help":
		return settings.Replies.Help, key, true
	case "unknown_text":
		return settings.Replies.UnknownText, key, true
	case "media_message":
		return settings.Replies.MediaMessage, key, true
	}
	if strings.HasPrefix(key, "custom:") {
		id := strings.TrimPrefix(key, "custom:")
		for _, rule := range settings.Rules.Custom {
			if rule.ID == id {
				return rule.Reply, "custom", true
			}
		}
	}
	return "", "", false
}

func validateSnippetSections(settings *model.ReplySettings) error {
	for key, section := range settings.SnippetSections {
		base, variableField, ok := replyBaseForSection(settings, key)
		if !ok {
			return fmt.Errorf("常用内容关联了不存在的回复场景: %s", key)
		}
		if len([]rune(section.Title)) > 80 {
			return fmt.Errorf("%s 的常用内容分区标题最多 80 字", key)
		}
		seen := map[string]bool{}
		for _, item := range section.Items {
			if strings.TrimSpace(item.SourceID) == "" || strings.TrimSpace(item.Title) == "" || strings.TrimSpace(item.Content) == "" {
				return fmt.Errorf("%s 的常用内容缺少编号、名称或正文", key)
			}
			if seen[item.SourceID] {
				return fmt.Errorf("%s 重复选择了同一条常用内容", key)
			}
			seen[item.SourceID] = true
			if len([]rune(item.Title)) > 80 || len([]rune(item.Content)) > 4000 {
				return fmt.Errorf("%s 的常用内容名称或正文过长", key)
			}
			if err := validateReplyVariables(variableField, item.Content); err != nil {
				return err
			}
		}
		if len([]rune(formatReplySnippetSection(base, section))) > 4000 {
			return fmt.Errorf("%s 合并常用内容后超过 4000 字上限", key)
		}
	}
	return nil
}

func validateReplyVariables(field, content string) error {
	if len([]rune(content)) > 4000 {
		return fmt.Errorf("%s 回复超过 4000 字上限", field)
	}
	allowed := map[string]bool{}
	switch field {
	case "code_success":
		allowed["project_name"] = true
		allowed["project_url"] = true
	case "code_invalid", "code_used":
		allowed["code"] = true
	case "universal_code":
		allowed["code"] = true
		allowed["expire_seconds"] = true
		allowed["expire_minutes"] = true
		allowed["expire_time"] = true
	case "projects", "custom":
		allowed["project_list"] = true
	}
	for _, variable := range replyVariablePattern.FindAllStringSubmatch(content, -1) {
		if !allowed[variable[1]] {
			return fmt.Errorf("%s 不支持变量 %s，请使用本场景提供的变量", field, variable[0])
		}
	}
	return nil
}

func (s *ProjectService) ListReplySnippets() ([]model.ReplySnippet, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, err := os.ReadFile(s.replyLibraryPath)
	if err != nil {
		return nil, err
	}
	library := []model.ReplySnippet{}
	err = json.Unmarshal(data, &library)
	return library, err
}

func (s *ProjectService) SaveReplySnippet(item *model.ReplySnippet) error {
	item.Title = strings.TrimSpace(item.Title)
	if item.Title == "" || strings.TrimSpace(item.Content) == "" {
		return errors.New("请填写名称和内容")
	}
	if len([]rune(item.Title)) > 80 || len([]rune(item.Content)) > 4000 {
		return errors.New("名称最多 80 字，内容最多 4000 字")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.replyLibraryPath)
	if err != nil {
		return err
	}
	var library []model.ReplySnippet
	if err := json.Unmarshal(data, &library); err != nil {
		return err
	}
	if item.ID != "" {
		for i := range library {
			if library[i].ID == item.ID {
				library[i] = *item
				return writeReplyJSON(s.replyLibraryPath, library)
			}
		}
		return errors.New("常用内容不存在，请刷新")
	}
	item.ID = "snippet_" + generateRandomHex(8)
	library = append(library, *item)
	return writeReplyJSON(s.replyLibraryPath, library)
}

func (s *ProjectService) DeleteReplySnippet(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.replyLibraryPath)
	if err != nil {
		return err
	}
	var library []model.ReplySnippet
	if err := json.Unmarshal(data, &library); err != nil {
		return err
	}
	for i := range library {
		if library[i].ID == id {
			library = append(library[:i], library[i+1:]...)
			return writeReplyJSON(s.replyLibraryPath, library)
		}
	}
	return errors.New("常用内容不存在")
}

// ReorderReplySnippets 以数组顺序作为唯一排序依据；显示序号由当前位置动态生成。
func (s *ProjectService) ReorderReplySnippets(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.replyLibraryPath)
	if err != nil {
		return err
	}
	var library []model.ReplySnippet
	if err := json.Unmarshal(data, &library); err != nil {
		return err
	}
	if len(ids) != len(library) {
		return errors.New("排序数据与当前内容数量不一致，请刷新后重试")
	}
	byID := make(map[string]model.ReplySnippet, len(library))
	for _, item := range library {
		byID[item.ID] = item
	}
	ordered := make([]model.ReplySnippet, 0, len(library))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		item, ok := byID[id]
		if !ok || seen[id] {
			return errors.New("排序数据包含不存在或重复的内容")
		}
		seen[id] = true
		ordered = append(ordered, item)
	}
	return writeReplyJSON(s.replyLibraryPath, ordered)
}
