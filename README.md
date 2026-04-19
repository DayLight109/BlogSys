# blog-api

个人博客系统的 Go 后端服务。

## 环境要求

- Go 1.23+
- MySQL 5.7+(本机默认 `localhost:3306`,账号 `root/root`,库名 `blog`)
- Redis 7+(本机默认 `localhost:6379`)

## 快速开始

```bash
# 1. 创建数据库(一次性)
mysql -uroot -proot -e "CREATE DATABASE IF NOT EXISTS blog DEFAULT CHARSET utf8mb4 COLLATE utf8mb4_unicode_ci;"

# 2. 复制环境变量(仓库里的 .env 已填好本地开发默认值,生产环境请用 .env.example 为模板)
cp .env.example .env

# 3. 拉依赖
go mod tidy

# 4. 启动
go run ./cmd/server
```

启动后访问:

- `GET http://localhost:8080/api/health` — 返回 `{"status":"ok","service":"blog-api"}`

## 目录结构

```
api/
├── cmd/server/main.go      # 入口
├── internal/
│   ├── config/             # .env 加载与配置结构
│   ├── database/           # MySQL/Redis 连接
│   ├── handler/            # Gin 路由处理函数
│   ├── service/            # 业务逻辑层
│   ├── repository/         # GORM 数据访问层
│   ├── model/              # 数据模型
│   └── middleware/         # JWT、CORS、限流等中间件
├── migrations/             # golang-migrate SQL 迁移文件
├── .env.example
├── .env                    # 本地实际配置(已 .gitignore)
└── go.mod
```
