package config

import (
	"os"
	"strconv"
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
	DefaultCodeExpire int // 验证码默认有效秒数（如 300 秒）
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
