package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"wechat-mp/internal/model"
	"wechat-mp/internal/service"
)

// RulesHandler 关键词规则引擎接口（仅管理员；普通用户不可见）
type RulesHandler struct {
	projectService *service.ProjectService
	wechatService  *service.WechatService
	accountService *service.AccountService
}

func NewRulesHandler(ps *service.ProjectService, ws *service.WechatService, as *service.AccountService) *RulesHandler {
	return &RulesHandler{projectService: ps, wechatService: ws, accountService: as}
}

// GetRules 查询当前关键词规则
// GET /api/rules
func (h *RulesHandler) GetRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	if authAdmin(h.accountService, w, r) == nil {
		return
	}

	rules, err := h.projectService.GetRules()
	if err != nil {
		writeError(w, http.StatusInternalServerError, 500, "规则配置读取失败: "+err.Error())
		return
	}

	writeSuccess(w, rules)
}

// UpdateRules 整包保存关键词规则（服务端完整校验）
// POST /api/rules/update
func (h *RulesHandler) UpdateRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	if authAdmin(h.accountService, w, r) == nil {
		return
	}

	var req model.SaveRulesReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "JSON 请求体格式不正确: "+err.Error())
		return
	}
	defer r.Body.Close()

	if err := h.projectService.SaveRules(&req.Rules); err != nil {
		writeError(w, http.StatusBadRequest, 400, "规则保存被拒绝: "+err.Error())
		return
	}

	writeSuccess(w, map[string]string{"message": "规则已保存并即时生效，无需重启服务"})
}

// TestRule 规则试跑：模拟一条用户消息，返回命中规则与实际回复内容
// POST /api/rules/test
func (h *RulesHandler) TestRule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 405, "不支持的请求方法")
		return
	}
	if authAdmin(h.accountService, w, r) == nil {
		return
	}

	var req model.TestRuleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 400, "JSON 请求体格式不正确: "+err.Error())
		return
	}
	defer r.Body.Close()

	message := strings.TrimSpace(req.Message)
	if message == "" {
		writeError(w, http.StatusBadRequest, 400, "缺少必填参数 'message'（模拟的用户消息）")
		return
	}

	ruleID, reply := h.wechatService.TestMessage(r.Context(), message)
	writeSuccess(w, model.TestRuleResp{
		Matched:   ruleID,
		MatchedZh: service.RuleNameZh(ruleID),
		Reply:     reply,
	})
}
