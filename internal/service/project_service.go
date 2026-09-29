package service

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"wechat-mp/internal/model"
)

var (
	ErrProjectNotFound      = errors.New("project not found")
	ErrProjectAlreadyExists = errors.New("project id already exists")
	ErrProjectDisabled      = errors.New("project is currently disabled")
	ErrInvalidPassword      = errors.New("invalid password")
)

type ProjectService struct {
	mu           sync.RWMutex
	projectsPath string
	authPath     string
	footersPath  string
	repliesPath  string
}

func NewProjectService(configDir string) *ProjectService {
	s := &ProjectService{
		projectsPath: filepath.Join(configDir, "projects.json"),
		authPath:     filepath.Join(configDir, "auth.json"),
		footersPath:  filepath.Join(configDir, "footers.json"),
		repliesPath:  filepath.Join(configDir, "replies.json"),
	}

	// 确保存储文件存在，如不存在则初始化默认值
	s.ensureInitialized()
	return s
}

func (s *ProjectService) ensureInitialized() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 确保 replies.json 存在
	if _, err := os.Stat(s.repliesPath); os.IsNotExist(err) {
		initialReplies := model.RepliesConfig{
			Welcome:       "您好！欢迎使用验证码中枢。\n👉 方式 1：输入网站上显示的 6 位数字直接验证；\n👉 方式 2：回复【验证码】获取 6 位通行码填入网站；\n👉 回复【帮助】或【项目】可查看支持的业务。",
			CodeSuccess:   "✅ 恭喜！【{project_name}】验证成功。\n您的身份已核销，请回到网页继续操作。",
			CodeInvalid:   "⚠️ 验证码【{code}】不存在或已过期，请在网站刷新重试。",
			CodeUsed:      "ℹ️ 验证码【{code}】已于早前完成核销，请勿重复使用。",
			UniversalCode: "🔑 您的通用通行码为：【{code}】\n\n⏱️ 有效期：5 分钟（一次性有效）\n📌 使用方式：请在您访问的业务网站（注册/登录/绑定）输入框中填入此 6 位数字即可！",
			Projects:      "🌐【当前已支持的业务平台】\n{project_list}\n更多项目持续接入中...",
			Help:          "📖【使用指南】\n👉 方式 1（极速免填）：若网站已显示 6 位数字，直接在聊天框发送该数字，网页自动完成验证！\n👉 方式 2（索取填码）：发送【验证码】或【登录】，即可获取 6 位通行码填入网站！",
			UnknownText:   "🤖 您好，未能识别您的指令。\n👉 电脑登录：直接发送网页显示的 6 位数字；\n👉 索取通行码：回复【验证码】获取 6 位通行码填入网站；\n👉 了解更多：回复【帮助】或【项目】。",
			MediaMessage:  "💡 抱歉，本中枢暂不支持语音/图片/表情包识别，请发送 6 位纯数字验证码或文字指令。",
		}
		data, _ := json.MarshalIndent(initialReplies, "", "  ")
		_ = os.WriteFile(s.repliesPath, data, 0644)
	}

	// 确保 footers.json 存在
	if _, err := os.Stat(s.footersPath); os.IsNotExist(err) {
		initialFooters := []*model.FooterItem{
			{
				ID:      "ft_nav",
				Title:   "日常工具站推广",
				Content: "🌟 推荐站点：实用工具导航站（https://example.com）",
				Active:  true,
			},
			{
				ID:      "ft_contact",
				Title:   "站长客服微信",
				Content: "💬 站长微信：v_helper_888（问题反馈/商务合作）",
				Active:  false,
			},
			{
				ID:      "ft_notice",
				Title:   "服务维护公告",
				Content: "📢 温馨提示：本平台运行平稳，如遇验证异常请稍后重试。",
				Active:  false,
			},
		}
		data, _ := json.MarshalIndent(initialFooters, "", "  ")
		_ = os.WriteFile(s.footersPath, data, 0644)
	}

	// 确保 auth.json 存在
	if _, err := os.Stat(s.authPath); os.IsNotExist(err) {
		initialAuth := model.AuthConfig{
			Password:  "admin",
			UpdatedAt: time.Now().Unix(),
		}
		data, _ := json.MarshalIndent(initialAuth, "", "  ")
		_ = os.WriteFile(s.authPath, data, 0644)
	}

	// 确保 projects.json 存在
	if _, err := os.Stat(s.projectsPath); os.IsNotExist(err) {
		initialProjects := []*model.ProjectItem{
			{
				ID:        "shop",
				Name:      "商城系统",
				Secret:    "sec_shop_" + generateRandomHex(6),
				Enabled:   true,
				CustomAd:  "🛒 商城新人专享：全场首单 8 折，限时抢购！",
				CreatedAt: time.Now().Unix(),
			},
			{
				ID:        "blog",
				Name:      "个人技术博客",
				Secret:    "sec_blog_" + generateRandomHex(6),
				Enabled:   true,
				CustomAd:  "📚 关注站长博客获取更多架构与技术干货！",
				CreatedAt: time.Now().Unix(),
			},
			{
				ID:        "nav",
				Name:      "工具箱导航",
				Secret:    "sec_nav_" + generateRandomHex(6),
				Enabled:   true,
				CustomAd:  "🚀 实用工具箱：聚合百款精选免费效率工具！",
				CreatedAt: time.Now().Unix(),
			},
		}
		data, _ := json.MarshalIndent(initialProjects, "", "  ")
		_ = os.WriteFile(s.projectsPath, data, 0644)
	}
}

// CheckPassword 校验管理密码
func (s *ProjectService) CheckPassword(inputPassword string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.authPath)
	if err != nil {
		return inputPassword == "admin888"
	}
	var auth model.AuthConfig
	if err := json.Unmarshal(data, &auth); err != nil {
		return inputPassword == "admin888"
	}
	return auth.Password == inputPassword
}

// ChangePassword 修改控制台管理密码并写入 auth.json
func (s *ProjectService) ChangePassword(oldPassword, newPassword string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.authPath)
	if err != nil {
		return err
	}
	var auth model.AuthConfig
	if err := json.Unmarshal(data, &auth); err != nil {
		return err
	}

	if auth.Password != oldPassword {
		return ErrInvalidPassword
	}

	auth.Password = newPassword
	auth.UpdatedAt = time.Now().Unix()

	updatedData, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.authPath, updatedData, 0644)
}

// ListProjects 获取全部项目列表
func (s *ProjectService) ListProjects() ([]*model.ProjectItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.projectsPath)
	if err != nil {
		return nil, err
	}
	var projects []*model.ProjectItem
	if err := json.Unmarshal(data, &projects); err != nil {
		return nil, err
	}
	return projects, nil
}

// GetProject 获取单个项目信息
func (s *ProjectService) GetProject(idOrName string) (*model.ProjectItem, error) {
	projects, err := s.ListProjects()
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		if p.ID == idOrName || p.Name == idOrName {
			return p, nil
		}
	}
	return nil, ErrProjectNotFound
}

// AddProject 新增接入项目并保存至 projects.json
func (s *ProjectService) AddProject(req *model.AddProjectReq) (*model.ProjectItem, error) {
	if req.ID == "" || req.Name == "" {
		return nil, fmt.Errorf("id and name are required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.projectsPath)
	if err != nil {
		return nil, err
	}
	var projects []*model.ProjectItem
	if err := json.Unmarshal(data, &projects); err != nil {
		projects = []*model.ProjectItem{}
	}

	for _, p := range projects {
		if p.ID == req.ID {
			return nil, ErrProjectAlreadyExists
		}
	}

	item := &model.ProjectItem{
		ID:            req.ID,
		Name:          req.Name,
		URL:           strings.TrimSpace(req.URL),
		Secret:        "sec_" + req.ID + "_" + generateRandomHex(8),
		Enabled:       true,
		CustomAdTitle: req.CustomAdTitle,
		CustomAd:      req.CustomAd,
		IncludeGlobal: req.IncludeGlobal,
		CreatedAt:     time.Now().Unix(),
	}

	projects = append(projects, item)
	updatedData, err := json.MarshalIndent(projects, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(s.projectsPath, updatedData, 0644); err != nil {
		return nil, err
	}

	return item, nil
}

// ToggleProject 切换项目启用/禁用状态
func (s *ProjectService) ToggleProject(id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.projectsPath)
	if err != nil {
		return err
	}
	var projects []*model.ProjectItem
	if err := json.Unmarshal(data, &projects); err != nil {
		return err
	}

	found := false
	for _, p := range projects {
		if p.ID == id {
			p.Enabled = enabled
			found = true
			break
		}
	}

	if !found {
		return ErrProjectNotFound
	}

	updatedData, err := json.MarshalIndent(projects, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.projectsPath, updatedData, 0644)
}

// UpdateProject 编辑项目信息（名称、定制尾注/推广语）
func (s *ProjectService) UpdateProject(req *model.UpdateProjectReq) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.projectsPath)
	if err != nil {
		return err
	}
	var projects []*model.ProjectItem
	if err := json.Unmarshal(data, &projects); err != nil {
		return err
	}

	found := false
	for _, p := range projects {
		if p.ID == req.ID {
			if req.Name != "" {
				p.Name = req.Name
			}
			p.URL = strings.TrimSpace(req.URL)
			p.CustomAdTitle = req.CustomAdTitle
			p.CustomAd = req.CustomAd
			p.IncludeGlobal = req.IncludeGlobal
			found = true
			break
		}
	}

	if !found {
		return ErrProjectNotFound
	}

	updatedData, err := json.MarshalIndent(projects, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.projectsPath, updatedData, 0644)
}

// DeleteProject 删除指定项目
func (s *ProjectService) DeleteProject(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.projectsPath)
	if err != nil {
		return err
	}
	var projects []*model.ProjectItem
	if err := json.Unmarshal(data, &projects); err != nil {
		return err
	}

	filtered := make([]*model.ProjectItem, 0, len(projects))
	found := false
	for _, p := range projects {
		if p.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, p)
	}

	if !found {
		return ErrProjectNotFound
	}

	updatedData, err := json.MarshalIndent(filtered, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.projectsPath, updatedData, 0644)
}

// ListFooters 获取所有默认尾注模版
func (s *ProjectService) ListFooters() ([]*model.FooterItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.footersPath)
	if err != nil {
		return nil, err
	}
	var footers []*model.FooterItem
	if err := json.Unmarshal(data, &footers); err != nil {
		return nil, err
	}
	return footers, nil
}

// GetActiveFootersContent 获取当前所有已勾选激活的默认尾注拼接内容（带【模版标题】）
func (s *ProjectService) GetActiveFootersContent() string {
	footers, err := s.ListFooters()
	if err != nil || len(footers) == 0 {
		return ""
	}

	var activeBlocks []string
	for _, f := range footers {
		if f.Active && strings.TrimSpace(f.Content) != "" {
			block := strings.TrimSpace(f.Content)
			title := strings.TrimSpace(f.Title)
			if title != "" {
				block = fmt.Sprintf("【%s】\n%s", title, block)
			}
			activeBlocks = append(activeBlocks, block)
		}
	}

	if len(activeBlocks) == 0 {
		return ""
	}
	return strings.Join(activeBlocks, "\n\n")
}

// AddFooter 新增尾注模版
func (s *ProjectService) AddFooter(req *model.AddFooterReq) (*model.FooterItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.footersPath)
	if err != nil {
		return nil, err
	}
	var footers []*model.FooterItem
	_ = json.Unmarshal(data, &footers)

	item := &model.FooterItem{
		ID:      "ft_" + generateRandomHex(4),
		Title:   req.Title,
		Content: req.Content,
		Active:  req.Active,
	}
	if item.Title == "" {
		item.Title = "自定义尾注"
	}

	footers = append(footers, item)
	updatedData, err := json.MarshalIndent(footers, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(s.footersPath, updatedData, 0644); err != nil {
		return nil, err
	}
	return item, nil
}

// UpdateFooter 修改尾注模版
func (s *ProjectService) UpdateFooter(req *model.UpdateFooterReq) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.footersPath)
	if err != nil {
		return err
	}
	var footers []*model.FooterItem
	if err := json.Unmarshal(data, &footers); err != nil {
		return err
	}

	found := false
	for _, f := range footers {
		if f.ID == req.ID {
			f.Title = req.Title
			f.Content = req.Content
			f.Active = req.Active
			found = true
			break
		}
	}

	if !found {
		return errors.New("footer not found")
	}

	updatedData, err := json.MarshalIndent(footers, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.footersPath, updatedData, 0644)
}

// ToggleFooter 切换单个尾注激活状态
func (s *ProjectService) ToggleFooter(id string, active bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.footersPath)
	if err != nil {
		return err
	}
	var footers []*model.FooterItem
	if err := json.Unmarshal(data, &footers); err != nil {
		return err
	}

	found := false
	for _, f := range footers {
		if f.ID == id {
			f.Active = active
			found = true
			break
		}
	}

	if !found {
		return errors.New("footer not found")
	}

	updatedData, err := json.MarshalIndent(footers, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.footersPath, updatedData, 0644)
}

// SetFootersActive 批量指定激活的尾注（支持单选、多选、全不选）
func (s *ProjectService) SetFootersActive(activeIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.footersPath)
	if err != nil {
		return err
	}
	var footers []*model.FooterItem
	if err := json.Unmarshal(data, &footers); err != nil {
		return err
	}

	idMap := make(map[string]bool)
	for _, id := range activeIDs {
		idMap[id] = true
	}

	for _, f := range footers {
		f.Active = idMap[f.ID]
	}

	updatedData, err := json.MarshalIndent(footers, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.footersPath, updatedData, 0644)
}

// DeleteFooter 删除尾注模版
func (s *ProjectService) DeleteFooter(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.footersPath)
	if err != nil {
		return err
	}
	var footers []*model.FooterItem
	if err := json.Unmarshal(data, &footers); err != nil {
		return err
	}

	filtered := make([]*model.FooterItem, 0, len(footers))
	found := false
	for _, f := range footers {
		if f.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, f)
	}

	if !found {
		return errors.New("footer not found")
	}

	updatedData, err := json.MarshalIndent(filtered, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.footersPath, updatedData, 0644)
}

// GetReplies 获取全场景自动回复配置
func (s *ProjectService) GetReplies() (*model.RepliesConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.repliesPath)
	if err != nil {
		return s.defaultReplies(), nil
	}
	var replies model.RepliesConfig
	if err := json.Unmarshal(data, &replies); err != nil {
		return s.defaultReplies(), nil
	}
	return &replies, nil
}

// UpdateReplies 更新全场景自动回复配置
func (s *ProjectService) UpdateReplies(req *model.RepliesConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.repliesPath, data, 0644)
}

func (s *ProjectService) defaultReplies() *model.RepliesConfig {
	return &model.RepliesConfig{
		Welcome:       "您好！欢迎使用验证码中枢。\n👉 方式 1：输入网站上显示的 6 位数字直接验证；\n👉 方式 2：回复【验证码】获取 6 位通行码填入网站；\n👉 回复【帮助】或【项目】可查看支持的业务。",
		CodeSuccess:   "✅ 恭喜！【{project_name}】验证成功。\n您的身份已核销，请回到网页继续操作。",
		CodeInvalid:   "⚠️ 验证码【{code}】不存在或已过期，请在网站刷新重试。",
		CodeUsed:      "ℹ️ 验证码【{code}】已于早前完成核销，请勿重复使用。",
		UniversalCode: "🔑 您的通用通行码为：【{code}】\n\n⏱️ 有效期：5 分钟（一次性有效）\n📌 使用方式：请在您访问的业务网站（注册/登录/绑定）输入框中填入此 6 位数字即可！",
		Projects:      "🌐【当前已支持的业务平台】\n{project_list}\n更多项目持续接入中...",
		Help:          "📖【使用指南】\n👉 方式 1（极速免填）：若网站已显示 6 位数字，直接在聊天框发送该数字，网页自动完成验证！\n👉 方式 2（索取填码）：发送【验证码】或【登录】，即可获取 6 位通行码填入网站！",
		UnknownText:   "🤖 您好，未能识别您的指令。\n👉 电脑登录：直接发送网页显示的 6 位数字；\n👉 索取通行码：回复【验证码】获取 6 位通行码填入网站；\n👉 了解更多：回复【帮助】或【项目】。",
		MediaMessage:  "💡 抱歉，本中枢暂不支持语音/图片/表情包识别，请发送 6 位纯数字验证码或文字指令。",
	}
}

func generateRandomHex(length int) string {
	b := make([]byte, length)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
