package config

import "os"

// Config 保存应用运行所需配置。
type Config struct {
	Port          string
	DSN           string
	JWTSecret     string
	AdminUsername string
	AdminPassword string
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Load 从环境变量加载配置。
func Load() Config {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		// 默认连接本地 MySQL，字符集 utf8mb4
		dsn = "venue:venue123@tcp(127.0.0.1:3306)/venue_booking?charset=utf8mb4&parseTime=True&loc=Local"
	}
	return Config{
		Port:          getEnv("APP_PORT", "7653"),
		DSN:           dsn,
		JWTSecret:     getEnv("JWT_SECRET", "venue-booking-admin-dev-secret-change-me"),
		AdminUsername: getEnv("ADMIN_USERNAME", "admin"),
		AdminPassword: getEnv("ADMIN_PASSWORD", "admin123"),
	}
}
