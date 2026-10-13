package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"wechat-mp/internal/config"
	"wechat-mp/internal/handler"
	"wechat-mp/internal/service"
	"wechat-mp/internal/storage"
	"wechat-mp/web"
)

func main() {
	log.Println(">>> 正在启动微信公众号多平台验证码中枢系统...")

	// 1. 加载配置
	cfg := config.Load()
	log.Printf("[Config] 服务端口: %s | 微信Token: %s | Redis地址: %s", cfg.Port, cfg.WechatToken, cfg.RedisAddr)

	// 2. 初始化 Redis 存储
	store, err := storage.NewStorage(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		log.Fatalf("[Redis] 连接失败: %v (提示: 请确认 Docker 中的 Redis 容器已启动)", err)
	}
	defer store.Close()
	log.Println("[Redis] 连接成功！")

	// 3. 业务服务层
	projectService := service.NewProjectService("./config")
	userIdentityService, err := service.NewUserIdentityService("./config")
	if err != nil {
		log.Fatalf("[UserIdentity] 永久用户编号表初始化失败: %v", err)
	}
	recordService := service.NewRecordService("./config", 90, userIdentityService)
	verifyService := service.NewVerifyService(cfg, store, projectService, recordService)
	licenseService := service.NewLicenseService("./config")
	accountService := service.NewAccountService("./config", store, verifyService, cfg, userIdentityService)
	wechatService := service.NewWechatService(cfg, store, verifyService, projectService, userIdentityService)

	// 3.5 数据保险：config/ 目录快照（启动一次 + 每 24 小时一次 + 优雅退出前一次，保留最近 7 份）
	backupService := service.NewBackupService("./config", "./backups/config", 7, 24*time.Hour)
	if _, err := backupService.SnapshotNow(); err != nil {
		log.Printf("[Backup] 启动快照失败: %v", err)
	}
	backupCtx, backupCancel := context.WithCancel(context.Background())
	go backupService.Run(backupCtx)

	// 4. 控制器层
	verifyHandler := handler.NewVerifyHandler(verifyService)
	wechatHandler := handler.NewWechatHandler(wechatService)
	menuHandler := handler.NewMenuHandler(store)
	rulesHandler := handler.NewRulesHandler(projectService, wechatService, accountService)
	replyEditorHandler := handler.NewReplyEditorHandler(projectService, wechatService, accountService)
	consoleHandler := handler.NewConsoleHandler(accountService)
	licenseHandler := handler.NewLicenseHandler(licenseService, accountService)
	projectHandler := handler.NewProjectHandler(projectService, accountService, licenseService)
	recordHandler := handler.NewRecordHandler(recordService, accountService, projectService)
	userIdentityHandler := handler.NewUserIdentityHandler(userIdentityService, accountService)

	// 5. 路由注册
	mux := http.NewServeMux()

	// 微信公众号接口：GET 用于首次握手校验，POST 用于接收消息推送
	mux.HandleFunc("/wechat", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			wechatHandler.VerifyServer(w, r)
		case http.MethodPost:
			wechatHandler.ReceiveMessage(w, r)
		default:
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	})

	// 业务项目 API 接口
	mux.HandleFunc("/api/code/create", verifyHandler.CreateCode)
	mux.HandleFunc("/api/code/check", verifyHandler.CheckCode)
	mux.HandleFunc("/api/code/verify-universal", verifyHandler.VerifyUniversal)
	mux.HandleFunc("/api/records", recordHandler.List)
	mux.HandleFunc("/health", verifyHandler.Health)

	// 控制台与项目管理 API 接口
	mux.HandleFunc("/api/console/login", consoleHandler.Login)
	mux.HandleFunc("/api/console/logout", consoleHandler.Logout)
	mux.HandleFunc("/api/console/dev-status", consoleHandler.DevStatus)
	mux.HandleFunc("/api/console/dev-accounts", consoleHandler.DevAccounts)
	mux.HandleFunc("/api/console/dev-login", consoleHandler.DevLogin)
	mux.HandleFunc("/api/console/bootstrap-status", consoleHandler.BootstrapStatus)
	mux.HandleFunc("/api/console/registration-status", consoleHandler.RegistrationStatus)
	mux.HandleFunc("/api/console/preview-session", consoleHandler.PreviewSession)
	mux.HandleFunc("/api/console/me", consoleHandler.Me)
	mux.HandleFunc("/api/accounts", consoleHandler.ListAccounts)
	mux.HandleFunc("/api/user-identities", userIdentityHandler.List)
	mux.HandleFunc("/api/accounts/set-status", consoleHandler.SetAccountStatus)
	mux.HandleFunc("/api/settings/registration", consoleHandler.RegistrationSettings)
	mux.HandleFunc("/api/licenses", licenseHandler.ListCodes)
	mux.HandleFunc("/api/licenses/generate", licenseHandler.GenerateCodes)
	mux.HandleFunc("/api/licenses/void", licenseHandler.VoidCode)
	mux.HandleFunc("/api/projects", projectHandler.ListProjects)
	mux.HandleFunc("/api/projects/add", projectHandler.AddProject)
	mux.HandleFunc("/api/projects/update", projectHandler.UpdateProject)
	mux.HandleFunc("/api/projects/toggle", projectHandler.ToggleProject)
	mux.HandleFunc("/api/projects/delete", projectHandler.DeleteProject)
	mux.HandleFunc("/api/projects/renew", projectHandler.RenewProject)
	mux.HandleFunc("/api/projects/rotate-key", projectHandler.RotateProjectKey)

	// 旧规则试跑路径保留兼容，但内部已改为无副作用模拟。
	mux.HandleFunc("/api/rules/test", rulesHandler.TestRule)

	// 统一回复规则、常用内容库与草稿预览（仅管理员）。
	mux.HandleFunc("/api/reply-settings", replyEditorHandler.Settings)
	mux.HandleFunc("/api/reply-settings/preview", replyEditorHandler.Preview)
	mux.HandleFunc("/api/verification-settings", replyEditorHandler.VerificationSettings)
	mux.HandleFunc("/api/reply-library", replyEditorHandler.Library)
	mux.HandleFunc("/api/reply-library/delete", replyEditorHandler.DeleteSnippet)
	mux.HandleFunc("/api/reply-library/reorder", replyEditorHandler.ReorderSnippets)

	// 自定义菜单配置接口（安全只读，返回 config/menu.json 正式配置）
	mux.HandleFunc("/api/menu", menuHandler.GetMenu)

	// 微信公众号一体化多功能控制台（支持手机端与 PC 端自适应）
	mux.HandleFunc("/console", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		_, _ = w.Write(web.ConsoleHTML)
	})

	// 兼容原有 /debug 路径，重定向至 /console
	mux.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/console", http.StatusMovedPermanently)
	})

	// 默认访问根路径重定向到 /console
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/console", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	// 6. 优雅启动与退出
	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf(">>> 🚀 服务已成功监听端口 :%s (管理控制台地址: http://localhost:%s/console)", cfg.Port, cfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP 服务异常退出: %v", err)
		}
	}()

	// 监听系统中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println(">>> 正在优雅关闭服务...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server forced shutdown: %v", err)
	}

	// 退出前最后一份数据快照
	backupCancel()
	if _, err := backupService.SnapshotNow(); err != nil {
		log.Printf("[Backup] 退出快照失败: %v", err)
	}

	fmt.Println(">>> 服务已完全停止。")
}
