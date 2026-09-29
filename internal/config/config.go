package config

import (
	"os"
	"strconv"
	"strings"
)

// Config 统一应用配置
type Config struct {
	Port              string
	WechatToken       string
	WechatAppID       string
	WechatAppSecret   string
	RedisAddr         string
	RedisPassword     string
	RedisDB           int
	DefaultCodeExpire int    // 验证码默认有效秒数（如 300 秒）
	AdContent         string // 全局追加广告/推广文案
	WelcomeMessage    string // 新关注公众号欢迎语
}

// Load 从系统环境变量读取配置，提供稳妥的默认值
func Load() *Config {
	return &Config{
		Port:              getEnv("PORT", "5000"),
		WechatToken:       getEnv("WECHAT_TOKEN", "mywechat2025"),
		WechatAppID:       getEnv("WECHAT_APPID", ""),
		WechatAppSecret:   getEnv("WECHAT_APPSECRET", ""),
		RedisAddr:         getEnv("REDIS_ADDR", "redis:6379"),
		RedisPassword:     getEnv("REDIS_PASSWORD", ""),
		RedisDB:           getEnvAsInt("REDIS_DB", 0),
		DefaultCodeExpire: getEnvAsInt("CODE_EXPIRE_SECONDS", 300),
		AdContent: strings.ReplaceAll(getEnv("AD_CONTENT", `------------------
🌟 推荐站点：实用工具导航站（https://example.com）`), "\\n", "\n"),
		WelcomeMessage: strings.ReplaceAll(getEnv("WELCOME_MSG", `您好！欢迎关注。
👉 网站验证：请直接发送网站上显示的 6 位数字验证码；
👉 帮助指南：回复【帮助】或【项目】查看支持的业务。`), "\\n", "\n"),
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvAsInt(key string, defaultVal int) int {
	valStr := os.Getenv(key)
	if valStr == "" {
		return defaultVal
	}
	val, err := strconv.Atoi(valStr)
	if err != nil {
		return defaultVal
	}
	return val
}
