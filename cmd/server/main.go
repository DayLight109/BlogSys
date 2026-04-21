package main

import (
	"context"
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
	"github.com/lilce/blog-api/internal/service"
)

func main() {
	cfg := config.Load()

	db, err := database.OpenMySQL(cfg.MySQLDSN)
	if err != nil {
		log.Fatalf("mysql: %v", err)
	}
	log.Println("mysql connected")

	// AutoMigrate only for additive, low-risk tables/columns introduced after
	// the initial golang-migrate run. Core schema still lives in /migrations.
	if err := db.AutoMigrate(&model.Setting{}, &model.Post{}); err != nil {
		log.Fatalf("automigrate: %v", err)
	}

	rdb, err := database.OpenRedis(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	log.Println("redis connected")
	_ = rdb

	userRepo := repository.NewUserRepository(db)
	postRepo := repository.NewPostRepository(db)
	commentRepo := repository.NewCommentRepository(db)
	settingRepo := repository.NewSettingRepository(db)

	tokenMgr := auth.NewTokenManager(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	authSvc := service.NewAuthService(userRepo, tokenMgr)
	postSvc := service.NewPostService(postRepo)
	settingSvc := service.NewSettingService(settingRepo)
	commentSvc := service.NewCommentService(commentRepo, postRepo, settingSvc)

	if err := authSvc.EnsureAdminSeed("admin", "admin123"); err != nil {
		log.Fatalf("seed admin: %v", err)
	}
	if err := settingSvc.EnsureDefaults(); err != nil {
		log.Fatalf("seed settings: %v", err)
	}

	if cfg.AppEnv == "prod" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	// Ensure URL-encoded path segments (e.g. Chinese slugs sent as %E5%85%B3…)
	// are decoded before c.Param() — otherwise lookups for posts whose slug
	// contains non-ASCII characters 404 because the DB stores the raw form.
	r.UseRawPath = true
	r.UnescapePathValues = true
	r.Use(gin.Logger(), gin.Recovery(), middleware.CORS(cfg.CORSOrigin))

	healthH := handler.NewHealthHandler()
	authH := handler.NewAuthHandler(authSvc)
	postH := handler.NewPostHandler(postSvc)
	commentH := handler.NewCommentHandler(commentSvc, postSvc)
	tagH := handler.NewTagHandler(postSvc)
	settingH := handler.NewSettingHandler(settingSvc)
	uploadH := handler.NewUploadHandler(cfg.UploadDir, "/uploads")

	api := r.Group("/api")
	{
		api.GET("/health", healthH.Check)

		authG := api.Group("/auth")
		{
			authG.POST("/login", authH.Login)
			authG.POST("/refresh", authH.Refresh)
			authG.POST("/logout", authH.Logout)
			authG.GET("/me", middleware.JWTAuth(tokenMgr), authH.Me)
		}

		posts := api.Group("/posts")
		{
			posts.GET("", postH.ListPublic)
			posts.GET("/:slug", postH.GetBySlug)
			posts.GET("/:slug/neighbors", postH.GetNeighbors)
			posts.GET("/:slug/related", postH.GetRelated)
			posts.GET("/:slug/comments", commentH.ListForSlug)
			posts.POST("/:slug/comments", commentH.SubmitForSlug)
		}

		api.GET("/archive", postH.Archive)
		api.GET("/search", postH.Search)
		api.GET("/tags", tagH.List)
		api.GET("/settings", settingH.GetPublic)

		admin := api.Group("/admin")
		admin.Use(middleware.JWTAuth(tokenMgr), middleware.AdminOnly())
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
			admin.GET("/settings", settingH.GetAdmin)
			admin.PUT("/settings", settingH.Update)
			admin.POST("/upload", uploadH.Create)
		}
	}

	if err := os.MkdirAll(cfg.UploadDir, 0o755); err != nil {
		log.Fatalf("upload dir: %v", err)
	}
	r.Static("/uploads", cfg.UploadDir)

	srv := &http.Server{
		Addr:    ":" + cfg.HTTPPort,
		Handler: r,
	}

	go func() {
		log.Printf("listening on :%s", cfg.HTTPPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
	log.Println("stopped")
}
