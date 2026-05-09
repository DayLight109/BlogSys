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

	OpenAIAPIKey        string
	OpenAIBaseURL       string
	OpenAIModel         string
	OpenAIModels        []string // optional allow-list of models the client may pick
	OpenAIWebSearchTool string   // legacy — Responses-API tool type name (kept for back-compat, currently unused)
	// OpenAIWebSearchModel is the Chat-Completions search-preview model id we
	// transparently swap to whenever the user toggles web search ON. The
	// regular chat-completions endpoint cannot enable hosted web search via
	// the `tools` field — only via these dedicated model variants paired with
	// a `web_search_options:{}` knob. Examples:
	//   - gpt-4o-search-preview
	//   - gpt-4o-mini-search-preview
	//   - gpt-5-search-api
	OpenAIWebSearchModel string
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

		OpenAIAPIKey:         getEnv("OPENAI_API_KEY", ""),
		OpenAIBaseURL:        normalizeOpenAIBaseURL(getEnv("OPENAI_BASE_URL", "https://api.openai.com/v1")),
		OpenAIModel:          getEnv("OPENAI_MODEL", "gpt-4o-mini"),
		OpenAIModels:         parseList(getEnv("OPENAI_MODELS", "")),
		OpenAIWebSearchTool:  getEnv("OPENAI_WEB_SEARCH_TOOL", ""),
		OpenAIWebSearchModel: getEnv("OPENAI_WEB_SEARCH_MODEL", "gpt-4o-mini-search-preview"),
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

func normalizeOpenAIBaseURL(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimRight(v, "/")
	v = strings.TrimSuffix(v, "/chat/completions")
	return strings.TrimRight(v, "/")
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

// parseList splits a comma-separated env value into trimmed, non-empty parts.
// Empty input returns nil — distinct from an empty slice — so callers can tell
// "feature unconfigured" from "explicitly empty list".
func parseList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
