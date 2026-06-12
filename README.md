# 城市体育场馆预订与赛事排期平台（纯后端）

体育场馆、场地预订与时段排期管理的纯后端 API 服务，作为 Feature 迭代题的基座工程。

## 技术栈

- Go + Gin
- GORM + MySQL 8（字符集 utf8mb4）
- JWT 鉴权（golang-jwt）、bcrypt 密码哈希

## 启动（Docker）

```bash
docker compose up --build
```

应用启动时会等待 MySQL 就绪、自动建表（GORM AutoMigrate）并灌入种子数据，服务监听 `http://127.0.0.1:7653`。

## 内置账号

唯一管理员（本平台只有 admin 一个角色）：

- 用户名：`admin`
- 密码：`admin123`

## 已实现的基础功能

- 登录签发 JWT、获取当前用户（`/api/auth/login`、`/api/auth/me`）
- 场馆增删改查（`/api/venues`）
- 场地预订查询、登记（带开放时段校验、时段冲突检测、自动算费）与状态流转（`/api/bookings`）
- 仪表盘统计（`/api/dashboard/stats`）
- 健康检查（`/api/health`）

除 `login` 与 `health` 外，接口均需 `Authorization: Bearer <token>`。

## 编码说明

数据库使用 utf8mb4，DSN 显式指定 charset；Gin 的 JSON 响应为 UTF-8，中文不乱码。
