package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv     string
	HTTPPort   string
	CORSOrigin string

	MySQLDSN string

	RedisAddr     string
	RedisPassword string
	RedisDB       int

	JWTSecret           string
	AccessTokenTTL      time.Duration
	RefreshTokenTTL     time.Duration

	UploadDir    string
	UploadPubURL string
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found, reading environment")
	}

	appEnv := normalizeAppEnv(getEnv("APP_ENV", "dev"))

	return &Config{
		AppEnv:     appEnv,
		HTTPPort:   getEnv("HTTP_PORT", "8080"),
		CORSOrigin: getEnv("CORS_ORIGIN", "http://localhost:3000,http://localhost:5173"),

		MySQLDSN: getEnv("MYSQL_DSN", "root:root@tcp(127.0.0.1:3306)/blog?charset=utf8mb4&parseTime=True&loc=Local"),

		RedisAddr:     getEnv("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       getEnvInt("REDIS_DB", 0),

		JWTSecret:       getEnv("JWT_SECRET", "change-me-in-production"),
		AccessTokenTTL:  time.Duration(getEnvInt("ACCESS_TOKEN_TTL_MIN", 30)) * time.Minute,
		RefreshTokenTTL: time.Duration(getEnvInt("REFRESH_TOKEN_TTL_DAY", 14)) * 24 * time.Hour,

		UploadDir:    getEnv("UPLOAD_DIR", "./uploads"),
		UploadPubURL: getEnv("UPLOAD_PUB_URL", "http://localhost:8080/uploads"),
	}
}

func (c *Config) IsProd() bool {
	return c.AppEnv == "prod"
}

func normalizeAppEnv(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "dev", "development", "local":
		return "dev"
	case "test", "testing":
		return "test"
	case "prod", "production":
		return "prod"
	default:
		return strings.ToLower(strings.TrimSpace(v))
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
