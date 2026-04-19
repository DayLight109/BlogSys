package middleware

import (
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func CORS(origins string) gin.HandlerFunc {
	cfg := cors.DefaultConfig()
	cfg.AllowOrigins = strings.Split(origins, ",")
	cfg.AllowCredentials = true
	cfg.AllowHeaders = []string{"Origin", "Content-Type", "Accept", "Authorization"}
	cfg.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	return cors.New(cfg)
}
