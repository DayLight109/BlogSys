package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lilce/blog-api/internal/auth"
	"github.com/lilce/blog-api/internal/config"
	"github.com/lilce/blog-api/internal/database"
	"github.com/lilce/blog-api/internal/handler"
	"github.com/lilce/blog-api/internal/middleware"
	"github.com/lilce/blog-api/internal/model"
	"github.com/lilce/blog-api/internal/repository"
	"github.com/lilce/blog-api/internal/scheduler"
	"github.com/lilce/blog-api/internal/service"
)

const defaultJWTSecret = "change-me-in-production"

func main() {
	cfg := config.Load()
	switch cfg.AppEnv {
	case "dev", "test", "prod":
	default:
		log.Fatalf("invalid APP_ENV %q; use dev, test, or prod/production", cfg.AppEnv)
	}

	// Refuse weak production secrets. In dev/test, replace the template with an
	// ephemeral random secret so the server never signs tokens with a known key.
	if cfg.JWTSecret == defaultJWTSecret {
		if cfg.IsProd() {
			log.Fatal("JWT_SECRET is the default value — refusing to start in prod. Set a random JWT_SECRET in env.")
		}
		secret, err := randomHex(32)
		if err != nil {
			log.Fatalf("jwt secret generation: %v", err)
		}
		cfg.JWTSecret = secret
		log.Println("JWT_SECRET was not set; generated an ephemeral dev/test secret for this process.")
	}
	if cfg.IsProd() && len([]byte(cfg.JWTSecret)) < 32 {
		log.Fatal("JWT_SECRET must be at least 32 bytes in prod.")
	}

	db, err := database.OpenMySQL(cfg.MySQLDSN, !cfg.IsProd())
	if err != nil {
		log.Fatalf("mysql: %v", err)
	}
	log.Println("mysql connected")

	// AutoMigrate only for additive, low-risk tables/columns introduced after
	// the initial golang-migrate run. Core schema still lives in /migrations.
	if err := db.AutoMigrate(
		&model.Setting{},
		&model.Post{},
		&model.Comment{},
		&model.AuditLog{},
	); err != nil {
		log.Fatalf("automigrate: %v", err)
	}
	if err := database.EnsurePostSearchIndex(db); err != nil {
		log.Fatalf("post search index: %v", err)
	}

	rdb, err := database.OpenRedis(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	log.Println("redis connected")

	userRepo := repository.NewUserRepository(db)
	postRepo := repository.NewPostRepository(db)
	commentRepo := repository.NewCommentRepository(db)
	settingRepo := repository.NewSettingRepository(db)
	auditRepo := repository.NewAuditRepository(db)

	tokenMgr := auth.NewTokenManager(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	authSvc := service.NewAuthService(userRepo, tokenMgr, rdb)
	postSvc := service.NewPostService(postRepo, rdb)
	settingSvc := service.NewSettingService(settingRepo)
	commentSvc := service.NewCommentService(commentRepo, postRepo, settingSvc)

	// Seed admin account on first boot. Prefer env-provided credentials; fall
	// back to a generated random password that's printed once and never again.
	adminUser := os.Getenv("ADMIN_USERNAME")
	if adminUser == "" {
		adminUser = "admin"
	}
	adminPass := os.Getenv("ADMIN_INITIAL_PASSWORD")
	generated := false
	userCount, err := userRepo.Count()
	if err != nil {
		log.Fatalf("count users: %v", err)
	}
	if adminPass == "" && userCount == 0 {
		if cfg.IsProd() {
			log.Fatal("ADMIN_INITIAL_PASSWORD must be set on first prod boot.")
		}
		adminPass, err = randomHex(12)
		if err != nil {
			log.Fatalf("admin password generation: %v", err)
		}
		generated = true
	}
	created, err := authSvc.EnsureAdminSeedIfEmpty(adminUser, adminPass)
	if err != nil {
		log.Fatalf("seed admin: %v", err)
	}
	if created && generated {
		log.Printf("======================================================")
		log.Printf(" Seeded admin account. Save these NOW — shown once:")
		log.Printf("   username: %s", adminUser)
		log.Printf("   password: %s", adminPass)
		log.Printf(" Change it immediately via /admin/settings or API.")
		log.Printf("======================================================")
	}
	if err := settingSvc.EnsureDefaults(); err != nil {
		log.Fatalf("seed settings: %v", err)
	}

	if cfg.IsProd() {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	// Ensure URL-encoded path segments (e.g. Chinese slugs sent as %E5%85%B3…)
	// are decoded before c.Param() — otherwise lookups for posts whose slug
	// contains non-ASCII characters 404 because the DB stores the raw form.
	r.UseRawPath = true
	r.UnescapePathValues = true
	// Cap multipart memory to 10 MB; larger uploads spill to disk. Per-request
	// body cap prevents a slow-POST / huge-JSON denial-of-service.
	r.MaxMultipartMemory = 10 << 20
	r.Use(
		middleware.BodyLimit(10<<20), // 10 MB default — upload handler does its own stricter 5 MB check
		gin.Logger(),
		gin.Recovery(),
		middleware.SecurityHeaders(cfg.IsProd()),
		middleware.CORS(cfg.CORSOrigin),
	)

	// 5 failed-ish attempts per minute per IP on login. Tight but still lets a
	// legit user fat-finger a few times.
	loginLimiter := middleware.NewLimiter(5, time.Minute)
	commentLimiter := middleware.NewLimiter(10, time.Minute)

	// Secure refresh cookie when running behind HTTPS in prod.
	secureCookies := cfg.IsProd()

	healthH := handler.NewHealthHandler()
	authH := handler.NewAuthHandler(authSvc, secureCookies)
	postH := handler.NewPostHandler(postSvc)
	commentH := handler.NewCommentHandler(commentSvc, postSvc)
	tagH := handler.NewTagHandler(postSvc)
	settingH := handler.NewSettingHandler(settingSvc)
	uploadH := handler.NewUploadHandler(cfg.UploadDir, "/uploads")
	auditH := handler.NewAuditHandler(auditRepo)

	api := r.Group("/api")
	{
		api.GET("/health", healthH.Check)

		authG := api.Group("/auth")
		{
			authG.POST("/login", middleware.RateLimit(loginLimiter), authH.Login)
			authG.POST("/refresh", authH.Refresh)
			authG.POST("/logout", authH.Logout)
			authG.GET("/me", middleware.JWTAuth(tokenMgr), authH.Me)
			authG.POST("/password", middleware.JWTAuth(tokenMgr), authH.ChangePassword)
		}

		posts := api.Group("/posts")
		{
			posts.GET("", postH.ListPublic)
			posts.GET("/:slug", postH.GetBySlug)
			posts.GET("/:slug/neighbors", postH.GetNeighbors)
			posts.GET("/:slug/related", postH.GetRelated)
			posts.GET("/:slug/comments", commentH.ListForSlug)
			posts.POST("/:slug/comments", middleware.RateLimit(commentLimiter), commentH.SubmitForSlug)
		}

		api.GET("/archive", postH.Archive)
		api.GET("/search", postH.Search)
		api.GET("/tags", tagH.List)
		api.GET("/settings", settingH.GetPublic)

		admin := api.Group("/admin")
		admin.Use(middleware.JWTAuth(tokenMgr), middleware.AdminOnly(), middleware.Audit(auditRepo))
		{
			ap := admin.Group("/posts")
			{
				ap.GET("", postH.ListAdmin)
				ap.GET("/:id", postH.GetAdminByID)
				ap.POST("", postH.Create)
				ap.PUT("/:id", postH.Update)
				ap.DELETE("/:id", postH.Delete)
			}
			ac := admin.Group("/comments")
			{
				ac.GET("", commentH.ListAdmin)
				ac.POST("", commentH.AdminReply)
				ac.PATCH("/:id", commentH.UpdateStatus)
				ac.DELETE("/:id", commentH.Delete)
			}
			at := admin.Group("/tags")
			{
				at.PATCH("/:name/rename", tagH.Rename)
				at.POST("/merge", tagH.Merge)
				at.DELETE("/:name", tagH.Delete)
			}
			// 回收站:独立 /admin/trash/* 前缀,避免 /admin/posts/trash 与
			// /admin/posts/:id 在 Gin radix tree 上的歧义。
			trash := admin.Group("/trash")
			{
				trash.GET("/posts", postH.ListTrash)
				trash.POST("/posts/:id/restore", postH.Restore)
				trash.DELETE("/posts/:id", postH.Purge)
				trash.GET("/comments", commentH.ListTrash)
				trash.POST("/comments/:id/restore", commentH.Restore)
				trash.DELETE("/comments/:id", commentH.Purge)
			}
			admin.GET("/settings", settingH.GetAdmin)
			admin.PUT("/settings", settingH.Update)
			admin.POST("/upload", uploadH.Create)
			admin.GET("/audit", auditH.List)
		}
	}

	if err := os.MkdirAll(cfg.UploadDir, 0o755); err != nil {
		log.Fatalf("upload dir: %v", err)
	}
	r.Static("/uploads", cfg.UploadDir)

	// scheduled-publish 后台任务:每分钟扫一次 status='scheduled' 已到点的文章。
	publisherCtx, publisherCancel := context.WithCancel(context.Background())
	defer publisherCancel()
	go scheduler.NewPublisher(postRepo).Run(publisherCtx)

	srv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           r,
		ReadHeaderTimeout: 15 * time.Second,
	}

	go func() {
		log.Printf("listening on :%s (env=%s, secure-cookies=%t)", cfg.HTTPPort, cfg.AppEnv, secureCookies)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down...")
	publisherCancel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
	log.Println("stopped")
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
