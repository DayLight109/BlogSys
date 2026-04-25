package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/lilce/blog-api/internal/model"
	"github.com/lilce/blog-api/internal/repository"
)

// Audit 在 admin 路由链上记录所有写操作(POST/PUT/PATCH/DELETE)。
//
// 工作方式:c.Next() 之后取响应状态;读 user 与请求元数据;在 goroutine 里
// 异步落库,避免阻塞响应。注意:gin.Context 在请求结束后会被池复用,所以
// 闭包内必须先把字段拷出来,不能在 goroutine 里再访问 c.* 字段。
func Audit(repo *repository.AuditRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		switch c.Request.Method {
		case "GET", "HEAD", "OPTIONS":
			return
		}

		uid := c.GetUint64(CtxUserID)
		if uid == 0 {
			return
		}

		entry := &model.AuditLog{
			UserID:    uid,
			Username:  c.GetString(CtxUsername),
			Method:    c.Request.Method,
			Path:      c.Request.URL.Path,
			Status:    c.Writer.Status(),
			IP:        c.ClientIP(),
			UserAgent: truncate(c.Request.UserAgent(), 500),
		}

		go func() { _ = repo.Create(entry) }()
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
