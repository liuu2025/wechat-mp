package handler

import (
	"io"
	"log"
	"net/http"

	"wechat-mp/internal/service"
)

type WechatHandler struct {
	wechatService *service.WechatService
}

func NewWechatHandler(svc *service.WechatService) *WechatHandler {
	return &WechatHandler{wechatService: svc}
}

// VerifyServer 处理微信的 GET 接入校验请求
func (h *WechatHandler) VerifyServer(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	signature := q.Get("signature")
	timestamp := q.Get("timestamp")
	nonce := q.Get("nonce")
	echostr := q.Get("echostr")

	log.Printf("[WeChat GET] signature=%s, timestamp=%s, nonce=%s, echostr=%s", signature, timestamp, nonce, echostr)

	if h.wechatService.VerifySignature(signature, timestamp, nonce) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(echostr))
		log.Println("[WeChat GET] 微信接入验证成功！已原样返回 echostr")
	} else {
		log.Println("[WeChat GET] 签名校验失败！请检查 Token 是否与微信后台一致")
		http.Error(w, "forbidden", http.StatusForbidden)
	}
}

// ReceiveMessage 处理微信的 POST 消息与事件推送
func (h *WechatHandler) ReceiveMessage(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("[WeChat POST] 读取报文失败: %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	replyXML, err := h.wechatService.ProcessMessage(r.Context(), body)
	if err != nil {
		log.Printf("[WeChat POST] 消息处理异常: %v", err)
		// 遇到错误时返回 success 防止微信连续重试
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("success"))
		return
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write([]byte(replyXML))
}
