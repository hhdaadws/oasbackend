# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Important Rules

- 中文回答
- 修改后端后重新用 Go 编译部署（`build_and_run.sh`），前端修改后 `npm run build`，禁止用 docker 启动
- 修改后端 API 后，最后更新 `docs/api-spec.md`（全部接口）和 `docs/oas2-agent-api-spec.md`（agent 相关接口）

## Project Overview

**oas-cloud-go** — Go cloud backend for OAS (Onikami Account System). Multi-tenant service managing managers, users, task scheduling, and agent-based job execution. Vue 3 SPA frontend. Redis is mandatory; the service will not start without it.

## Common Commands

### Backend

```bash
# 本地编译部署（推荐，日志输出到 E 盘）
bash build_and_run.sh

# 或手动：加载 .env + 运行
cd /e/new_oas/oasbackend && set -a && source .env && set +a && go run ./cmd/server

# 测试（SQLite 内存 + mock Redis，无需外部依赖）
go test ./...
go test ./internal/server -run TestFunctionName    # 单个测试
go test ./internal/scheduler -run TestFunctionName # 调度器测试

# Lint
go vet ./...
```

### Frontend

```bash
cd frontend
npm install
npm run dev      # Dev server at :5173, proxies /api to :7000
npm run build    # Production build to frontend/dist/
```

### Local Dev Notes

- Go 后端不会自动加载 `.env`，需先 `source .env` 或用 `build_and_run.sh`
- `SERVE_FRONTEND=true` 时后端直接服务 `frontend/dist/` 静态文件
- `SERVE_FRONTEND=false` 时用 Vite dev server（端口 5173）开发前端
- 连接云端 PostgreSQL（端口 5492）和 Redis（端口 6399），凭据在 `.env`

## Architecture

### Layout

- `cmd/server/main.go` — 入口：加载配置、连接 DB/Redis、启动 Server
- `internal/server/server.go` — Server 核心结构体，所有 HTTP handlers（~3500 行）
- `internal/server/middleware.go` — JWT/UserToken/ManagerActive/RateLimit 中间件
- `internal/server/types.go` — 所有请求/响应 binding structs（35+ 类型）
- `internal/server/scan_handlers.go` — 扫码登录（QR）流程 handlers
- `internal/server/scan_ws.go` — WebSocket hub，agent 推送扫码状态给用户浏览器
- `internal/server/test_helpers_test.go` — `inMemoryStore` mock + 测试工具函数
- `internal/models/models.go` — 所有 GORM 模型（17 个实体），单文件
- `internal/auth/token.go` — JWT 签发/解析、bcrypt、opaque token 生成、SHA-256 哈希
- `internal/cache/redis.go` — Redis `Store` 接口 + 实现（lease、slot、缓存、限流）
- `internal/config/config.go` — 环境变量配置，支持 `_FILE` 后缀读 Docker secrets
- `internal/scheduler/generator.go` — 后台任务生成器协程，Redis slot 去重
- `internal/taskmeta/templates.go` — 任务模板定义、用户类型任务池、默认配置
- `internal/taskmeta/nexttime.go` — 调度规则计算（daily_reset, interval_6h, coop_window 等）
- `internal/notify/miaotixing.go` — 喵提醒推送通知集成
- `frontend/` — Vue 3 + Vite + Element Plus SPA
- `docs/api-spec.md` — 全部 API 接口文档
- `docs/oas2-agent-api-spec.md` — Agent 相关 API 文档

### API Structure

All endpoints under `/api/v1`. Route groups by role:

- `/api/v1/super/*` — 超管：manager 生命周期、续期密钥、审计日志、博主管理
- `/api/v1/manager/*` — 管理员：激活码、用户 CRUD、资产、任务、对弈答案、总览
- `/api/v1/user/*` — 用户：个人资料、任务、资产、扫码登录、好友、组队御魂、通知配置
- `/api/v1/agent/*` — Agent：任务轮询、lease 管理、心跳、扫码代理、用户配置读写
- Public: `/health`, `/scheduler/status`, `/task-templates`, `/bootstrap/*`

### Authentication

- **Super/Manager/Agent**: JWT (HS256), role-based middleware (`requireJWT(roles...)`)
- **Users**: Opaque tokens (180 天 TTL), SHA-256 哈希存储在 `user_tokens` 表, Redis 缓存 2 分钟
- Agent JWT 额外校验 Redis session（`requireJWT` 内检查）
- Manager 过期检查有 Redis 1 分钟缓存（`requireManagerActive`）

### User Types

五种用户类型，各有独立任务池：`daily`, `duiyi`, `shuaka`, `foster`, `jingzhi`。激活码按类型创建，注册时强制绑定用户类型。任务模板在 `internal/taskmeta/templates.go` 定义每个类型的默认任务和配置。

### Task Scheduler

后台协程 (`internal/scheduler/generator.go`)，默认 10s 间隔：

1. 过期旧的对弈竞猜 jobs（时间窗口已过）
2. 全局重置过期的 job lease（leased/running → pending）
3. 北京时间 00:00–05:59 跳过生成（休息窗口）
4. 批量预加载活跃用户 + 任务配置 + 活跃 job 计数（避免 N+1）
5. 并行处理每个用户（worker pool + semaphore channel）
6. 生成组队御魂配对 jobs

**去重 slot 格式**：
- `daily:YYYYMMDD:HHMM` (26h TTL) — HH:MM 定时任务
- `datetime:YYYYMMDDHHmm` (24h TTL) — 指定日期时间任务
- `rolling:YYYYMMDDHHmm` (90s TTL) — 无 next_time 的滚动生成
- `team_yuhun:requestID` (24h TTL) — 组队御魂

### Key Patterns

- **Server struct**: 中心对象，持有 config、DB、Redis、scheduler、token manager、Gin router、audit/notify channels、WebSocket hub
- **Response format**: 成功 `{"data": ...}` 或直接返回对象字段；列表 `{"items": [...], "total": N}`；错误 `{"detail": "message"}`
- **Job lifecycle**: `pending → leased → running → success/failed/timeout_requeued`
- **Task config 三层合并**: `defaults(enabled:false) + existing + submitted patch`，用 `deepMerge()` 递归合并，patch 优先
- **Audit logging**: 异步 channel (buffer 1024) + 单个 auditWorker 协程写 DB，有溢出保护
- **Notification**: `notifyCh` (buffer 1024) + 8 个 notifyWorker 协程，per-code 15s 限流
- **Lease 所有权**: Redis SetNX 获取，Lua 脚本原子检查 owner 后才能 release/refresh
- **JSONB fields**: User 的 assets、rest_config、lineup_config、shikigami_config 等用 `datatypes.JSONMap`
- **User token cache**: Redis hash, 2 分钟 TTL，`last_used_at` 最多每 5 分钟更新一次

### Tests

测试使用 SQLite 内存数据库 + `inMemoryStore` mock Redis（`test_helpers_test.go`），无需外部依赖：

- `setupTestServer(t)` — 创建带 mock store 的 Server（scheduler 禁用）
- `doJSONRequest(t, handler, method, path, body, token)` — HTTP 测试辅助
- `createActiveManager(t, db, username, password)` — 创建有效期 30 天的 manager
- 测试文件按功能分：`permission_test.go`, `expiry_test.go`, `concurrency_test.go` 等

### Frontend

- 手动 SPA 路由（`App.vue`，无 Vue Router），用 `window.history.pushState`
- 页面：`/login`, `/manager`, `/user`, `/super-admin-login`, `/super-admin`
- 组件按角色组织：`components/manager/`, `components/user/`, `components/super/`, `components/shared/`
- Composables：`useBatchSelection`, `useDebouncedFilter`, `usePagination`, `useTaskTemplates`
- Vite 配置：`unplugin-auto-import` + `unplugin-vue-components` 自动导入 Element Plus
