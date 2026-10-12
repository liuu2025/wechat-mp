package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"wechat-mp/internal/config"
	"wechat-mp/internal/model"
	"wechat-mp/internal/storage"
)

var digitRegex = regexp.MustCompile(`^\d{6}$`)

type previewCodeStateReader interface {
	GetCode(context.Context, string) (*model.CodeRecord, error)
}

// verifyMaxFailures 单 openid 在时间窗口内允许的核销失败次数上限（防 6 位码盲猜爆破）
const verifyMaxFailures = 20

// factoryDefaultRules 出厂默认规则（rules.json 缺失/损坏时的兜底，永远与出厂模板保持一致）
func factoryDefaultRules() *model.RulesConfig {
	return &model.RulesConfig{
		UniversalCode: model.BuiltinRule{Enabled: true, Keywords: []string{"验证码", "取码", "登录", "注册", "通行码", "yzm", "code", "1"}},
		Help:          model.BuiltinRule{Enabled: true, Keywords: []string{"帮助", "help", "?", "？"}},
		Projects:      model.BuiltinRule{Enabled: true, Keywords: []string{"项目", "业务"}},
		Custom:        []model.CustomRule{},
	}
}

// getRulesSafe 读取规则配置，任何异常都退回出厂默认，保证消息管道永不因配置问题哑火
func (s *WechatService) getRulesSafe() *model.RulesConfig {
	if s.projectService != nil {
		if settings, err := s.projectService.GetReplySettings(); err == nil && settings != nil {
			return &settings.Rules
		}
	}
	return factoryDefaultRules()
}

// matchKeyword 精确匹配（大小写不敏感、自动去首尾空白）
func matchKeyword(keywords []string, lowerText string) bool {
	for _, k := range keywords {
		if strings.ToLower(strings.TrimSpace(k)) == lowerText {
			return true
		}
	}
	return false
}

// matchTextRule 消息路由：返回命中的规则标识
// 优先级固定：六位数字（契约锁死）→ 内置规则 → 自定义规则（先配先中）→ 兜底
func (s *WechatService) matchTextRule(rules *model.RulesConfig, text string) string {
	text = strings.TrimSpace(text) // 与 ProcessMessage 的裁剪契约保持一致，单元自身也要健壮
	if digitRegex.MatchString(text) {
		return "verify_code"
	}
	lower := strings.ToLower(text)
	if rules.UniversalCode.Enabled && matchKeyword(rules.UniversalCode.Keywords, lower) {
		return "universal_code"
	}
	if rules.Help.Enabled && matchKeyword(rules.Help.Keywords, lower) {
		return "help"
	}
	if rules.Projects.Enabled && matchKeyword(rules.Projects.Keywords, lower) {
		return "projects"
	}
	for i := range rules.Custom {
		if rules.Custom[i].Enabled && matchKeyword(rules.Custom[i].Keywords, lower) {
			return "custom:" + rules.Custom[i].ID
		}
	}
	return "fallback"
}

// RuleNameZh 规则标识的中文名（控制台试跑展示用）
func RuleNameZh(ruleID string) string {
	switch {
	case ruleID == "verify_code":
		return "发送网页验证码"
	case ruleID == "code_success":
		return "六位数字验证成功"
	case ruleID == "code_invalid":
		return "六位数字无效或已过期"
	case ruleID == "code_used":
		return "六位数字重复核销"
	case ruleID == "code_project_unavailable":
		return "六位数字所属项目不可用"
	case ruleID == "universal_code":
		return "公众号取码"
	case ruleID == "help":
		return "使用指南"
	case ruleID == "projects":
		return "项目清单"
	case ruleID == "fallback":
		return "兜底回复（未命中任何规则）"
	case strings.HasPrefix(ruleID, "custom:"):
		return "自定义回复规则"
	default:
		return ruleID
	}
}

// TestMessage 规则试跑：只做匹配和模拟渲染，不生成或核销真实验证码。
func (s *WechatService) TestMessage(ctx context.Context, message string) (string, string) {
	resp, err := s.PreviewReply(ctx, nil, "", strings.TrimSpace(message), "", "839201", false)
	if err != nil {
		return "fallback", "预览失败：" + err.Error()
	}
	return resp.Matched, resp.Reply
}

type WechatService struct {
	cfg               *config.Config
	storage           *storage.Storage
	verifyService     *VerifyService
	projectService    *ProjectService
	userIdentities    *UserIdentityService
	previewCodeReader previewCodeStateReader
}

func NewWechatService(cfg *config.Config, store *storage.Storage, vSvc *VerifyService, ps *ProjectService, identities ...*UserIdentityService) *WechatService {
	s := &WechatService{
		cfg:            cfg,
		storage:        store,
		verifyService:  vSvc,
		projectService: ps,
	}
	if store != nil {
		s.previewCodeReader = store
	}
	if len(identities) > 0 {
		s.userIdentities = identities[0]
	}
	return s
}

// VerifySignature 校验来自微信服务器的 GET 请求签名
func (s *WechatService) VerifySignature(signature, timestamp, nonce string) bool {
	if s.cfg.WechatToken == "" {
		return false
	}
	strs := []string{s.cfg.WechatToken, timestamp, nonce}
	sort.Strings(strs)

	h := sha1.New()
	h.Write([]byte(strings.Join(strs, "")))
	sha1Str := hex.EncodeToString(h.Sum(nil))

	return sha1Str == signature
}

// ProcessMessage 处理用户微信消息与事件，并返回 XML 字符串
func (s *WechatService) ProcessMessage(ctx context.Context, body []byte) (string, error) {
	var in model.WechatIncomingMsg
	if err := xml.Unmarshal(body, &in); err != nil {
		return "", fmt.Errorf("unmarshal incoming xml failed: %w", err)
	}
	if s.userIdentities != nil && strings.TrimSpace(in.FromUserName) != "" {
		if _, err := s.userIdentities.GetOrCreate(in.FromUserName, time.Now().Unix()); err != nil {
			return "", fmt.Errorf("persist user identity failed: %w", err)
		}
	}

	replyContent := ""

	switch in.MsgType {
	case "text":
		replyContent = s.handleText(ctx, in.FromUserName, strings.TrimSpace(in.Content))
	case "event":
		replyContent = s.handleEvent(ctx, in.FromUserName, in.Event, in.EventKey)
	default:
		// 暂不支持的消息类型（语音、图片、表情包等），返回提示
		replies := s.getRepliesConfig()
		replyContent = replies.MediaMessage
	}

	if replyContent == "" {
		return "success", nil
	}

	reply := model.WechatTextReply{
		ToUserName:   model.CDATA(in.FromUserName),
		FromUserName: model.CDATA(in.ToUserName),
		CreateTime:   time.Now().Unix(),
		MsgType:      model.CDATA("text"),
		Content:      model.CDATA(replyContent),
	}

	outXml, err := xml.MarshalIndent(reply, "", "  ")
	if err != nil {
		return "", err
	}
	return string(outXml), nil
}

func (s *WechatService) getRepliesConfig() *model.RepliesConfig {
	if s.projectService != nil {
		if settings, err := s.projectService.GetReplySettings(); err == nil && settings != nil {
			return &settings.Replies
		}
	}
	return &model.RepliesConfig{
		Welcome:       "您好！欢迎使用验证码中枢。\n👉 方式 1：回复【验证码】获取 6 位通行码填入网站；\n👉 方式 2：输入网站上显示的 6 位数字直接验证；\n👉 回复【帮助】或【项目】可查看支持的业务。",
		CodeSuccess:   "✅ 恭喜！【{project_name}】验证成功。\n您的身份已核销，请回到网页继续操作。",
		CodeInvalid:   "⚠️ 验证码【{code}】不存在或已过期，请在网站刷新重试。",
		CodeUsed:      "ℹ️ 验证码【{code}】已于早前完成核销，请勿重复使用。",
		UniversalCode: "🔑 您的通用通行码为：【{code}】\n\n⏱️ 有效期：3 分钟（一次性有效）\n📌 使用方式：请在您访问的业务网站（注册/登录/绑定）输入框中填入此 6 位数字即可！",
		Projects:      "🌐【当前已支持的业务平台】\n{project_list}\n更多项目持续接入中...",
		Help:          "📖【使用指南】\n👉 方式 1（索取填码）：发送【验证码】或【登录】，即可获取 6 位通行码填入网站！\n👉 方式 2（极速免填）：若网站已显示 6 位数字，直接在聊天框发送该数字，网页自动完成验证！",
		UnknownText:   "🤖 您好，未能识别您的指令。\n👉 电脑登录：直接发送网页显示的 6 位数字；\n👉 索取通行码：回复【验证码】获取 6 位通行码填入网站；\n👉 了解更多：回复【帮助】或【项目】。",
		MediaMessage:  "💡 抱歉，本中枢暂不支持语音/图片/表情包识别，请发送 6 位纯数字验证码或文字指令。",
	}
}

// handleText 处理用户发送的文本内容（按规则引擎路由：六位数字 → 内置规则 → 自定义规则 → 兜底）
func (s *WechatService) handleText(ctx context.Context, openID, text string) string {
	replies := s.getRepliesConfig()
	rules := s.getRulesSafe()
	ruleID := s.matchTextRule(rules, text)

	// 防爆破：六位数字核销尝试按 openid 记失败频次，超限临时拉黑
	if ruleID == "verify_code" {
		if blocked, _ := s.storage.IsVerifyBlocked(ctx, openID, verifyMaxFailures); blocked {
			return "⚠️ 您的验证尝试过于频繁，请稍后再试。"
		}
	}

	switch {
	case ruleID == "verify_code":
		record, err := s.storage.GetCode(ctx, text)
		if err != nil {
			if errors.Is(err, storage.ErrCodeNotFound) {
				s.storage.RecordVerifyFailure(ctx, openID)
				msg := strings.ReplaceAll(replies.CodeInvalid, "{code}", text)
				return msg
			}
			return "系统繁忙，请稍后重试。"
		}

		// 项目有效性校验（停用或到期的项目，其未核销码立即失效）
		if s.projectService != nil {
			if p, err := s.projectService.GetProject(record.Project); err == nil && !IsProjectUsable(p) {
				return "⚠️ 该验证码所属项目已停用或授权到期，无法完成核销。"
			}
		}

		if record.Status == model.StatusVerified || record.Status == model.StatusUsed {
			msg := strings.ReplaceAll(replies.CodeUsed, "{code}", text)
			return msg
		}

		// 执行核销（成功即清空失败计数）
		if _, err := s.verifyService.MarkCodeVerified(ctx, text, openID); err != nil {
			return "核销处理异常，请重试。"
		}
		s.storage.ClearVerifyFailures(ctx, openID)

		projectName := record.Project
		if projectName == "" {
			projectName = "默认项目"
		}

		// 项目补充说明只属于当前项目；公共内容库不会在发送阶段自动追加。
		projectCustomAd := ""
		projectCustomTitle := ""
		projectURL := ""

		if s.projectService != nil {
			if p, err := s.projectService.GetProject(record.Project); err == nil {
				if p.Name != "" {
					projectName = p.Name
				}
				projectURL = strings.TrimSpace(p.URL)
				projectCustomAd = strings.TrimSpace(p.CustomAd)
				projectCustomTitle = strings.TrimSpace(p.CustomAdTitle)
			}
		}

		// 项目补充信息有标题时加上【标题】；若留空则原样输出。
		projectBlock := projectCustomAd
		if projectBlock != "" && projectCustomTitle != "" {
			projectBlock = fmt.Sprintf("【%s】\n%s", projectCustomTitle, projectBlock)
		}

		projectSupplement := ""
		if projectBlock != "" {
			projectSupplement = "\n\n-------------------\n" + projectBlock
		}

		baseMsg := formatSuccessMessage(replies.CodeSuccess, projectName, projectURL)
		return baseMsg + projectSupplement

	case ruleID == "universal_code":
		code, err := s.verifyService.GenerateUniversalCode(ctx, openID)
		if err != nil {
			return "⚠️ 验证码生成繁忙，请稍后再试。"
		}
		msg := strings.ReplaceAll(replies.UniversalCode, "{code}", code)
		msg = strings.ReplaceAll(msg, "{expire_seconds}", fmt.Sprintf("%d", s.cfg.DefaultCodeExpire))
		msg = strings.ReplaceAll(msg, "{expire_minutes}", fmt.Sprintf("%d", s.cfg.DefaultCodeExpire/60))
		return msg

	case ruleID == "help":
		return replies.Help

	case ruleID == "projects":
		return strings.ReplaceAll(replies.Projects, "{project_list}", s.projectListText())

	case strings.HasPrefix(ruleID, "custom:"):
		// 自定义回复规则命中（先配先中已在路由阶段保证）
		for i := range rules.Custom {
			if rules.Custom[i].ID == strings.TrimPrefix(ruleID, "custom:") {
				return strings.ReplaceAll(rules.Custom[i].Reply, "{project_list}", s.projectListText())
			}
		}
		return replies.UnknownText

	default:
		// 默认未知消息兜底回复
		return replies.UnknownText
	}
}

func (s *WechatService) projectListText() string {
	listStr := "暂无已启用的业务平台"
	if s.projectService == nil {
		return listStr
	}
	projects, err := s.projectService.ListProjects()
	if err != nil {
		return listStr
	}
	var lines []string
	for _, p := range projects {
		if !IsProjectUsable(p) {
			continue
		}
		if strings.TrimSpace(p.URL) != "" {
			lines = append(lines, fmt.Sprintf("• %s：%s", p.Name, strings.TrimSpace(p.URL)))
		} else {
			lines = append(lines, fmt.Sprintf("• %s (%s)", p.Name, p.ID))
		}
	}
	if len(lines) > 0 {
		return strings.Join(lines, "\n")
	}
	return listStr
}

// PreviewReply 使用草稿规则渲染回复。useLiveCodeState 仅只读查询验证码真实状态，绝不核销。
func (s *WechatService) PreviewReply(ctx context.Context, draft *model.ReplySettings, field, message, projectID, code string, useLiveCodeState bool) (*model.ReplyPreviewResp, error) {
	settings := draft
	if settings == nil {
		if s.projectService == nil {
			return nil, errors.New("回复配置服务不可用")
		}
		current, err := s.projectService.GetReplySettings()
		if err != nil {
			return nil, err
		}
		settings = current
	}
	if code == "" {
		code = "839201"
	}

	matched := field
	if matched == "" {
		matched = s.matchTextRule(&settings.Rules, message)
	}
	if matched == "verify_code" {
		if useLiveCodeState {
			liveCode := strings.TrimSpace(message)
			if !digitRegex.MatchString(liveCode) {
				liveCode = code
			}
			return s.previewLiveVerification(ctx, settings, liveCode)
		}
		matched = "code_invalid"
	}

	reply := ""
	switch {
	case matched == "code_success":
		reply = s.renderPreviewSuccess(settings.Replies.CodeSuccess, projectID, "示例项目", "https://example.com")
	case matched == "verify_code" || matched == "code_invalid":
		matched = "code_invalid"
		reply = strings.ReplaceAll(settings.Replies.CodeInvalid, "{code}", code)
	case matched == "code_used":
		reply = strings.ReplaceAll(settings.Replies.CodeUsed, "{code}", code)
	case matched == "universal_code":
		reply = strings.ReplaceAll(settings.Replies.UniversalCode, "{code}", code)
		reply = strings.ReplaceAll(reply, "{expire_seconds}", fmt.Sprintf("%d", s.cfg.DefaultCodeExpire))
		reply = strings.ReplaceAll(reply, "{expire_minutes}", fmt.Sprintf("%d", s.cfg.DefaultCodeExpire/60))
	case matched == "projects":
		reply = strings.ReplaceAll(settings.Replies.Projects, "{project_list}", s.projectListText())
	case matched == "help":
		reply = settings.Replies.Help
	case matched == "welcome":
		reply = settings.Replies.Welcome
	case matched == "media_message":
		reply = settings.Replies.MediaMessage
	case matched == "fallback" || matched == "unknown_text":
		matched = "fallback"
		reply = settings.Replies.UnknownText
	case strings.HasPrefix(matched, "custom:"):
		for _, rule := range settings.Rules.Custom {
			if rule.ID == strings.TrimPrefix(matched, "custom:") {
				reply = strings.ReplaceAll(rule.Reply, "{project_list}", s.projectListText())
				break
			}
		}
	default:
		return nil, fmt.Errorf("不支持的预览场景: %s", matched)
	}

	return &model.ReplyPreviewResp{Matched: matched, MatchedZh: RuleNameZh(matched), Reply: reply}, nil
}

func (s *WechatService) previewLiveVerification(ctx context.Context, settings *model.ReplySettings, code string) (*model.ReplyPreviewResp, error) {
	reader := s.previewCodeReader
	if reader == nil && s.storage != nil {
		reader = s.storage
	}
	if reader == nil {
		return nil, errors.New("验证码存储不可用")
	}
	record, err := reader.GetCode(ctx, code)
	if errors.Is(err, storage.ErrCodeNotFound) {
		return &model.ReplyPreviewResp{
			Matched:       "code_invalid",
			MatchedZh:     RuleNameZh("code_invalid"),
			Reply:         strings.ReplaceAll(settings.Replies.CodeInvalid, "{code}", code),
			LiveCodeState: true,
			CodeState:     "not_found_or_expired",
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取验证码真实状态失败: %w", err)
	}

	// 与正式消息处理顺序一致：先判断项目是否可用，再判断是否已核销。
	if s.projectService != nil {
		if project, projectErr := s.projectService.GetProject(record.Project); projectErr == nil && !IsProjectUsable(project) {
			return &model.ReplyPreviewResp{
				Matched:       "code_project_unavailable",
				MatchedZh:     RuleNameZh("code_project_unavailable"),
				Reply:         "⚠️ 该验证码所属项目已停用或授权到期，无法完成核销。",
				LiveCodeState: true,
				CodeState:     "project_unavailable",
			}, nil
		}
	}

	if record.Status == model.StatusVerified || record.Status == model.StatusUsed {
		return &model.ReplyPreviewResp{
			Matched:       "code_used",
			MatchedZh:     RuleNameZh("code_used"),
			Reply:         strings.ReplaceAll(settings.Replies.CodeUsed, "{code}", code),
			LiveCodeState: true,
			CodeState:     "used",
		}, nil
	}

	fallbackName := record.Project
	if fallbackName == "" {
		fallbackName = "默认项目"
	}
	return &model.ReplyPreviewResp{
		Matched:       "code_success",
		MatchedZh:     RuleNameZh("code_success"),
		Reply:         s.renderPreviewSuccess(settings.Replies.CodeSuccess, record.Project, fallbackName, ""),
		LiveCodeState: true,
		CodeState:     "pending",
	}, nil
}

func (s *WechatService) renderPreviewSuccess(template, projectID, fallbackName, fallbackURL string) string {
	name, url, title, supplement := fallbackName, fallbackURL, "", ""
	if s.projectService != nil && projectID != "" {
		if project, err := s.projectService.GetProject(projectID); err == nil {
			name, url, title, supplement = project.Name, project.URL, project.CustomAdTitle, project.CustomAd
		}
	}
	reply := formatSuccessMessage(template, name, url)
	if strings.TrimSpace(supplement) != "" {
		block := strings.TrimSpace(supplement)
		if strings.TrimSpace(title) != "" {
			block = fmt.Sprintf("【%s】\n%s", strings.TrimSpace(title), block)
		}
		reply += "\n\n-------------------\n" + block
	}
	return reply
}

// formatSuccessMessage 处理验证成功通知模板中的变量替换与安全消融
func formatSuccessMessage(template, projectName, projectURL string) string {
	msg := strings.ReplaceAll(template, "{project_name}", projectName)
	if strings.TrimSpace(projectURL) != "" {
		return strings.ReplaceAll(msg, "{project_url}", strings.TrimSpace(projectURL))
	}

	// 项目未配置网址时，执行安全微创消融，绝不波及主句
	lines := strings.Split(msg, "\n")
	var cleanLines []string
	for _, line := range lines {
		if strings.Contains(line, "{project_url}") {
			trimmed := strings.TrimSpace(line)
			// 1. 如果该行纯粹是介绍网址的短行，整行安全抽掉
			isStandalone := false
			prefixes := []string{
				"{project_url}", "网址：{project_url}", "业务网址：{project_url}",
				"官网：{project_url}", "官方网址：{project_url}", "项目网址：{project_url}",
				"网站：{project_url}", "访问地址：{project_url}", "访问网址：{project_url}",
				"链接：{project_url}", "官方网站：{project_url}",
			}
			for _, pfx := range prefixes {
				if trimmed == pfx || trimmed == "• "+pfx || trimmed == "👉 "+pfx || trimmed == "🔗 "+pfx {
					isStandalone = true
					break
				}
			}
			if isStandalone {
				continue
			}

			// 2. 如果在一句长句中，就近微创消融修饰词与变量，保全主句
			l := strings.ReplaceAll(line, "，业务网址：{project_url}", "")
			l = strings.ReplaceAll(l, "，访问网址：{project_url}", "")
			l = strings.ReplaceAll(l, "，官方网址：{project_url}", "")
			l = strings.ReplaceAll(l, "，网址：{project_url}", "")
			l = strings.ReplaceAll(l, "（业务网址：{project_url}）", "")
			l = strings.ReplaceAll(l, "（网址：{project_url}）", "")
			l = strings.ReplaceAll(l, "({project_url})", "")
			l = strings.ReplaceAll(l, "（{project_url}）", "")
			l = strings.ReplaceAll(l, "{project_url}", "")
			cleanLines = append(cleanLines, l)
		} else {
			cleanLines = append(cleanLines, line)
		}
	}
	return strings.Join(cleanLines, "\n")
}

// handleEvent 处理关注事件或菜单点击事件
func (s *WechatService) handleEvent(ctx context.Context, openID, event, eventKey string) string {
	replies := s.getRepliesConfig()
	switch event {
	case "subscribe": // 用户新关注公众号
		return replies.Welcome
	case "unsubscribe": // 用户取消关注（微信不需要回复）
		return ""
	case "CLICK": // 用户点击了自定义菜单
		if strings.HasPrefix(eventKey, "GET_CODE_") {
			projectName := strings.TrimPrefix(eventKey, "GET_CODE_")
			resp, err := s.verifyService.GenerateCode(ctx, &model.CreateCodeReq{
				Project:       projectName,
				SessionTag:    "menu:" + openID,
				ExpireSeconds: s.cfg.DefaultCodeExpire,
				Source:        RecordSourceWechat,
			}, "menu:"+openID, true)
			if err != nil {
				return "生成验证码异常，请稍后重试。"
			}
			return fmt.Sprintf("🔑【%s】专属验证码已生成：\n\n👉【%s】👈\n\n(有效时间 3 分钟，请在对应网页输入完成验证)", projectName, resp.Code)
		}

		switch eventKey {
		case "HELP_INFO":
			return `📖【使用指引】
1. 网站登录：如果在电脑网站上看到了 6 位数字验证码，直接发给本公众号即可瞬间完成核销登录；
2. 主动取码：也可以点击下方菜单获取专属验证码后填入网站；
3. 遇到问题：请核对网站最新生成的验证码。`
		case "AD_INFO":
			return `🌟【站长精选推荐】
1. 实用导航：https://example.com/nav
2. 开源工具箱：https://example.com/tools
3. 商务合作与技术咨询请联系管理员。`
		default:
			return fmt.Sprintf("您点击了菜单【%s】。\n如需验证，可直接输入 6 位数字验证码，或在网页上使用。", eventKey)
		}
	default:
		return ""
	}
}
