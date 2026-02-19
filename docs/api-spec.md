# OAS Cloud API 规范文档

> 本文档描述 oasbackend（Go 云端后端）暴露的所有 HTTP API，以及 Oas2.0（Python 本地客户端）在云端模式下的调用方式。

## 1. 通用约定

### Base URL

```
http://<host>:7000
```

所有业务接口前缀为 `/api/v1`。

### 响应格式

| 场景 | 格式 |
|------|------|
| 成功 | `{"data": ...}` 或直接返回 JSON 对象 |
| 错误 | `{"detail": "错误信息"}` |

HTTP 状态码：`200` 成功，`201` 创建成功，`400` 参数错误，`401` 认证失败，`403` 权限不足，`404` 资源不存在，`409` 冲突。

### 认证机制

| 类型 | 适用角色 | Header 格式 | 有效期 |
|------|---------|------------|--------|
| JWT | super / manager / agent | `Authorization: Bearer <token>` | 可配置（默认 24h） |
| Opaque Token | user | `Authorization: Bearer <token>` | 180 天 |

### 分页参数

支持分页的 GET 端点统一使用：

| 参数 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `page` | int | 1 | 页码 |
| `page_size` | int | 20 | 每页条数（上限 100） |

分页响应格式：
```json
{
  "data": { "items": [...], "total": 100, "page": 1, "page_size": 20 }
}
```

---

## 2. 公共端点（无认证）

### GET /health

健康检查。

**响应：**
```json
// 正常
{"status": "ok", "redis": "up"}
// Redis 不可用
{"status": "degraded", "redis": "down"}  // HTTP 503
```

---

### GET /api/v1/bootstrap/status

检查 Super Admin 是否已初始化。

**响应：**
```json
{"initialized": true}
```

---

### POST /api/v1/bootstrap/init

初始化 Super Admin 账号（仅可调用一次）。

**请求：**
```json
{
  "username": "admin",       // 3-64 字符
  "password": "secret123"    // 6-128 字符
}
```

**响应 201：**
```json
{"data": {"message": "super admin created"}}
```

---

### GET /api/v1/scheduler/status

获取调度器状态和快照。

**响应：**
```json
{
  "scheduler": { "running": true, "tick_interval": "30s", ... },
  "snapshot": { ... }
}
```

---

### GET /api/v1/task-templates

获取任务模板列表。

| 参数 | 类型 | 说明 |
|------|------|------|
| `user_type` | string | 可选，按用户类型过滤（daily/duiyi/shuaka） |

**响应：**
```json
{
  "data": {
    "pools": { "daily": [...], "duiyi": [...], "shuaka": [...] },
    "templates": { "signin": {...}, "explore": {...}, ... }
  }
}
```

---

## 3. Super Admin 端点

> 认证：JWT（role=super）

### POST /api/v1/super/auth/login

Super Admin 登录。

**请求：**
```json
{
  "username": "admin",
  "password": "secret123"
}
```

**响应：**
```json
{"token": "<jwt>"}
```

---

### POST /api/v1/super/manager-renewal-keys

创建 Manager 续费密钥。

**请求：**
```json
{
  "duration_days": 30    // 1-3650
}
```

**响应 201：**
```json
{
  "data": {
    "id": 1,
    "code": "abc123def456",
    "duration_days": 30,
    "status": "unused",
    "created_at": "2025-01-01T00:00:00Z"
  }
}
```

---

### GET /api/v1/super/manager-renewal-keys

列出续费密钥（分页）。

| 参数 | 类型 | 说明 |
|------|------|------|
| `status` | string | 可选，按状态过滤（unused/used/revoked） |
| `keyword` | string | 可选，搜索 code |
| `page` | int | 页码 |
| `page_size` | int | 每页条数 |

**响应：**
```json
{
  "data": {
    "items": [
      {
        "id": 1,
        "code": "abc123",
        "duration_days": 30,
        "status": "unused",
        "used_by_manager_id": null,
        "used_at": null,
        "created_at": "2025-01-01T00:00:00Z"
      }
    ],
    "total": 10,
    "page": 1,
    "page_size": 20
  }
}
```

---

### PATCH /api/v1/super/manager-renewal-keys/:id/status

更新续费密钥状态。

**请求：**
```json
{"status": "revoked"}
```

**响应：**
```json
{"data": {"id": 1, "status": "revoked"}}
```

---

### DELETE /api/v1/super/manager-renewal-keys/:id

删除续费密钥。

**响应：**
```json
{"data": {"message": "deleted"}}
```

---

### POST /api/v1/super/manager-renewal-keys/batch-revoke

批量撤销续费密钥。

**请求：**
```json
{"key_ids": [1, 2, 3]}    // 1-500 个
```

**响应：**
```json
{"data": {"revoked": 3}}
```

---

### POST /api/v1/super/manager-renewal-keys/batch-delete

批量删除续费密钥。

**请求：**
```json
{"ids": [1, 2, 3]}    // 1-500 个
```

**响应：**
```json
{"data": {"deleted": 3}}
```

---

### GET /api/v1/super/managers

列出 Manager（分页）。

| 参数 | 类型 | 说明 |
|------|------|------|
| `keyword` | string | 可选，搜索 username/alias |
| `page` | int | 页码 |
| `page_size` | int | 每页条数 |

**响应：**
```json
{
  "data": {
    "items": [
      {
        "id": 1,
        "username": "mgr1",
        "alias": "管理员A",
        "expires_at": "2025-12-31T23:59:59Z",
        "user_count": 50,
        "created_at": "2025-01-01T00:00:00Z"
      }
    ],
    "total": 5,
    "page": 1,
    "page_size": 20
  }
}
```

---

### PATCH /api/v1/super/managers/:id/lifecycle

更新 Manager 有效期。

**请求：**
```json
{
  "expires_at": "2026-06-30T23:59:59Z",   // 可选，直接设置过期时间
  "extend_days": 30                         // 可选，延长天数
}
```

**响应：**
```json
{"data": {"id": 1, "expires_at": "2026-06-30T23:59:59Z"}}
```

---

### POST /api/v1/super/managers/batch-lifecycle

批量更新 Manager 有效期。

**请求：**
```json
{
  "manager_ids": [1, 2, 3],     // 1-200 个
  "expires_at": "2026-06-30T23:59:59Z",
  "extend_days": 30
}
```

**响应：**
```json
{"data": {"updated": 3}}
```

### PATCH /api/v1/super/managers/:id/password

重置 Manager 密码。

**请求：**
```json
{
  "password": "newpassword123"
}
```

**响应：**
```json
{"data": {"message": "password reset"}}
```

### GET /api/v1/super/audit-logs

查询超管视角的操作审计日志。包含超管自身操作以及管理员使用续费密钥的记录。

**Query 参数：**
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| action | string | 否 | 按操作类型精确过滤 |
| keyword | string | 否 | 按操作者用户名模糊搜索 |
| page | int | 否 | 页码，默认 1 |
| page_size | int | 否 | 每页条数，默认 50，最大 200 |

**支持的 action 值：**
- `create_manager_renewal_key` — 创建续费密钥
- `patch_manager_renewal_key_status` — 撤销续费密钥
- `batch_revoke_renewal_keys` — 批量撤销密钥
- `delete_manager_renewal_key` — 删除续费密钥
- `batch_delete_renewal_keys` — 批量删除密钥
- `patch_manager_lifecycle` — 修改管理员有效期
- `reset_manager_password` — 重置管理员密码
- `batch_manager_lifecycle` — 批量修改管理员有效期
- `redeem_manager_renewal_key` — 管理员使用续费密钥

**响应：**
```json
{
  "items": [
    {
      "id": 1,
      "actor_type": "super",
      "actor_id": 1,
      "actor_name": "admin",
      "action": "create_manager_renewal_key",
      "target_type": "manager_renewal_key",
      "target_id": 5,
      "detail": {"duration_days": 30},
      "ip": "192.168.1.1",
      "created_at": "2026-02-19T10:30:00Z"
    }
  ],
  "total": 150,
  "page": 1,
  "page_size": 50
}
```

---

## 4. Manager 端点

> 认证：JWT（role=manager）。带 `*` 标记的端点额外要求 Manager 未过期。

### POST /api/v1/manager/auth/register

Manager 注册（使用续费密钥中的 code）。

**请求：**
```json
{
  "username": "mgr1",
  "password": "secret123"
}
```

**响应 201：**
```json
{"token": "<jwt>"}
```

---

### POST /api/v1/manager/auth/login

Manager 登录。

**请求：**
```json
{
  "username": "mgr1",
  "password": "secret123"
}
```

**响应：**
```json
{"token": "<jwt>"}
```

---

### GET /api/v1/manager/auth/me

获取当前 Manager 信息。

**响应：**
```json
{
  "data": {
    "id": 1,
    "username": "mgr1",
    "alias": "管理员A",
    "expires_at": "2025-12-31T23:59:59Z",
    "created_at": "2025-01-01T00:00:00Z"
  }
}
```

---

### POST /api/v1/manager/auth/redeem-renewal-key

Manager 使用续费密钥续期。

**请求：**
```json
{"code": "abc123def456"}
```

**响应：**
```json
{"data": {"expires_at": "2026-06-30T23:59:59Z", "extended_days": 30}}
```

---

### PUT /api/v1/manager/me/alias *

更新 Manager 别名。

**请求：**
```json
{"alias": "新别名"}    // 最长 64 字符
```

**响应：**
```json
{"data": {"alias": "新别名"}}
```

---

### GET /api/v1/manager/overview *

获取 Manager 仪表盘概览。

**响应：**
```json
{
  "data": {
    "user_stats": { "total": 100, "active": 80, "expired": 20 },
    "job_stats": { "pending": 5, "running": 3, "success_24h": 50, "failed_24h": 2 },
    "recent_failures": [...]
  }
}
```

---

### GET /api/v1/manager/task-pool *

查询任务池（分页）。

| 参数 | 类型 | 说明 |
|------|------|------|
| `status` | string | 可选，按任务状态过滤 |
| `keyword` | string | 可选，搜索关键字 |
| `page` | int | 页码 |
| `page_size` | int | 每页条数 |

**响应：**
```json
{
  "data": {
    "items": [
      {
        "id": 1,
        "user_id": 5,
        "task_type": "signin",
        "status": "pending",
        "priority": 50,
        "scheduled_at": "2025-01-01T00:00:00Z",
        "attempts": 0,
        "max_attempts": 3
      }
    ],
    "total": 100,
    "page": 1,
    "page_size": 20
  }
}
```

---

### POST /api/v1/manager/activation-codes *

创建激活码。

**请求：**
```json
{
  "duration_days": 30,               // 1-3650
  "user_type": "daily"               // daily | duiyi | shuaka
}
```

**响应 201：**
```json
{
  "data": {
    "id": 1,
    "code": "xyz789",
    "duration_days": 30,
    "user_type": "daily",
    "status": "unused",
    "created_at": "2025-01-01T00:00:00Z"
  }
}
```

---

### GET /api/v1/manager/activation-codes *

列出激活码（分页）。

| 参数 | 类型 | 说明 |
|------|------|------|
| `status` | string | 可选（unused/used/revoked） |
| `user_type` | string | 可选（daily/duiyi/shuaka） |
| `keyword` | string | 可选，搜索 code |
| `page` | int | 页码 |
| `page_size` | int | 每页条数 |

---

### PATCH /api/v1/manager/activation-codes/:id/status *

更新激活码状态。

**请求：**
```json
{"status": "revoked"}
```

---

### DELETE /api/v1/manager/activation-codes/:id *

删除激活码。

---

### POST /api/v1/manager/activation-codes/batch-revoke *

批量撤销激活码。

**请求：**
```json
{"code_ids": [1, 2, 3]}    // 1-500 个
```

---

### POST /api/v1/manager/activation-codes/batch-delete *

批量删除激活码。

**请求：**
```json
{"code_ids": [1, 2, 3]}
```

---

### POST /api/v1/manager/users/quick-create *

直接创建用户（无需激活码）。

**请求：**
```json
{
  "duration_days": 30,
  "user_type": "daily"
}
```

**响应 201：**
```json
{
  "data": {
    "id": 1,
    "account_no": "1234567890",
    "user_type": "daily",
    "status": "active",
    "expires_at": "2025-02-01T00:00:00Z"
  }
}
```

---

### GET /api/v1/manager/users *

列出用户（分页）。

| 参数 | 类型 | 说明 |
|------|------|------|
| `status` | string | 可选（active/expired/disabled） |
| `user_type` | string | 可选（daily/duiyi/shuaka） |
| `keyword` | string | 可选，搜索 account_no/username |
| `login_id` | string | 可选，精确匹配 login_id |
| `page` | int | 页码 |
| `page_size` | int | 每页条数 |

**响应：**
```json
{
  "data": {
    "items": [
      {
        "id": 1,
        "account_no": "1234567890",
        "login_id": "1",
        "user_type": "daily",
        "status": "active",
        "archive_status": "normal",
        "server": "",
        "username": "",
        "expires_at": "2025-12-31T23:59:59Z",
        "created_at": "2025-01-01T00:00:00Z"
      }
    ],
    "total": 50,
    "page": 1,
    "page_size": 20
  }
}
```

---

### PATCH /api/v1/manager/users/:user_id/lifecycle *

更新用户生命周期。

**请求：**
```json
{
  "expires_at": "2026-06-30T23:59:59Z",   // 可选
  "extend_days": 30,                       // 可选
  "status": "active",                      // 可选（active/disabled）
  "archive_status": "normal"               // 可选
}
```

---

### POST /api/v1/manager/users/batch-lifecycle *

批量更新用户生命周期。

**请求：**
```json
{
  "user_ids": [1, 2, 3],            // 1-500 个
  "expires_at": "2026-06-30T23:59:59Z",
  "extend_days": 30,
  "status": "active"
}
```

---

### GET /api/v1/manager/users/:user_id/assets *

获取用户资产。

**响应：**
```json
{
  "data": {
    "assets": {
      "level": 150,
      "stamina": 500,
      "gouyu": 2500,
      "lanpiao": 100,
      "gold": 50000,
      "gongxun": 800,
      "xunzhang": 200
    }
  }
}
```

---

### PUT /api/v1/manager/users/:user_id/assets *

设置用户资产。

**请求：**
```json
{
  "assets": {
    "level": 150,
    "gouyu": 3000
  }
}
```

---

### POST /api/v1/manager/users/batch-assets *

批量设置用户资产。

**请求：**
```json
{
  "user_ids": [1, 2, 3],
  "assets": {"gouyu": 3000}
}
```

---

### GET /api/v1/manager/users/:user_id/tasks *

获取用户任务配置。

**响应：**
```json
{
  "data": {
    "task_config": {
      "signin": {"enabled": true, "priority": 50, "next_time": "2025-01-02 00:01"},
      "explore": {"enabled": true, "priority": 30}
    },
    "version": 3
  }
}
```

---

### PUT /api/v1/manager/users/:user_id/tasks *

更新用户任务配置（合并策略：defaults + existing + submitted）。

**请求：**
```json
{
  "task_config": {
    "signin": {"enabled": true, "priority": 60},
    "explore": {"enabled": false}
  }
}
```

---

### GET /api/v1/manager/users/:user_id/logs *

获取用户审计日志（分页）。

| 参数 | 类型 | 说明 |
|------|------|------|
| `page` | int | 页码 |
| `page_size` | int | 每页条数 |

---

### DELETE /api/v1/manager/users/:user_id/logs *

删除用户日志和相关的任务事件。

---

### DELETE /api/v1/manager/users/:user_id *

删除单个下属用户及其所有关联数据（任务、日志、Token、任务配置）。

**响应 200：**
```json
{"message": "user deleted"}
```

**错误码：**
- 404：用户不存在
- 403：该用户不在此管理员下

---

### POST /api/v1/manager/users/batch-delete *

批量删除下属用户及其所有关联数据。

**请求：**
```json
{
  "user_ids": [1, 2, 3]
}
```

**响应 200：**
```json
{"deleted": 3, "requested": 3}
```

---

## 5. User 端点

> 认证：Opaque Token（通过注册或登录获取）

### POST /api/v1/user/auth/register-by-code

用户通过激活码注册。

**请求：**
```json
{"code": "xyz789"}    // 6-64 字符
```

**响应 201：**
```json
{
  "data": {
    "token": "<opaque_token>",
    "account_no": "1234567890",
    "user_type": "daily",
    "expires_at": "2025-12-31T23:59:59Z",
    "token_exp": "2026-06-30T23:59:59Z"
  }
}
```

---

### POST /api/v1/user/auth/login

用户登录。

**请求：**
```json
{
  "account_no": "1234567890",   // 6-64 字符
  "device_info": "iPhone 15"    // 可选
}
```

**响应：**
```json
{
  "data": {
    "token": "<opaque_token>",
    "account_no": "1234567890",
    "user_type": "daily",
    "token_exp": "2026-06-30T23:59:59Z"
  }
}
```

---

### POST /api/v1/user/auth/logout

登出（撤销当前 token）。

**响应：**
```json
{"data": {"message": "logged out"}}
```

---

### POST /api/v1/user/auth/redeem-code

用户兑换激活码（续期/切换类型）。

**请求：**
```json
{"code": "newcode123"}
```

**响应：**
```json
{
  "data": {
    "expires_at": "2026-06-30T23:59:59Z",
    "user_type": "duiyi",
    "extended_days": 30
  }
}
```

---

### GET /api/v1/user/me/profile

获取用户资料。

**响应：**
```json
{
  "data": {
    "id": 1,
    "account_no": "1234567890",
    "login_id": "1",
    "user_type": "daily",
    "status": "active",
    "archive_status": "normal",
    "server": "不知火服",
    "username": "游戏昵称",
    "expires_at": "2025-12-31T23:59:59Z",
    "created_at": "2025-01-01T00:00:00Z"
  }
}
```

---

### PUT /api/v1/user/me/profile

更新用户资料。

**请求：**
```json
{
  "server": "不知火服",     // 可选
  "username": "游戏昵称"    // 可选
}
```

---

### GET /api/v1/user/me/assets

获取用户资产。

**响应：**
```json
{
  "data": {
    "assets": { "level": 150, "stamina": 500, ... }
  }
}
```

---

### GET /api/v1/user/me/tasks

获取用户任务配置。

**响应：**
```json
{
  "data": {
    "task_config": { "signin": {"enabled": true, ...}, ... },
    "version": 3
  }
}
```

---

### PUT /api/v1/user/me/tasks

更新用户任务配置（合并策略）。

**请求：**
```json
{
  "task_config": {
    "signin": {"enabled": true, "priority": 60}
  }
}
```

---

### GET /api/v1/user/me/logs

获取用户审计日志（分页）。

---

## 6. Agent 端点（Oas2.0 客户端使用）

> 如需专门面向 Oas2.0 开发的简化版文档，请参阅 [Oas2.0 Agent API 对接文档](./oas2-agent-api-spec.md)。

> 认证：JWT（role=agent）。Agent 使用 Manager 的账号密码登录。

### POST /api/v1/agent/auth/login

Agent 登录（使用 Manager 凭据）。

**请求：**
```json
{
  "username": "mgr1",           // Manager 用户名
  "password": "secret123",      // Manager 密码
  "node_id": "LAPTOP-ABC-1234", // 本地节点唯一标识
  "version": "1.0.0"            // 可选，客户端版本
}
```

**响应：**
```json
{
  "token": "<jwt>",
  "manager_id": 1,
  "node_id": "LAPTOP-ABC-1234"
}
```

**说明：** 登录时会自动注册/更新 AgentNode 记录，并检查 Manager 是否过期。

---

### POST /api/v1/agent/poll-jobs

轮询待执行任务。

**请求：**
```json
{
  "node_id": "LAPTOP-ABC-1234",
  "limit": 10,                    // 可选，默认 10
  "lease_seconds": 90              // 可选，默认 90
}
```

**响应：**
```json
{
  "jobs": [
    {
      "ID": 42,
      "ManagerID": 1,
      "UserID": 5,
      "TaskType": "signin",
      "Payload": {
        "user_id": 5,
        "source": "cloud_scheduler"
      },
      "Priority": 50,
      "ScheduledAt": "2025-01-01T08:00:00Z",
      "Status": "leased",
      "LeasedByNode": "LAPTOP-ABC-1234",
      "LeaseUntil": "2025-01-01T08:01:30Z",
      "Attempts": 0,
      "MaxAttempts": 3,
      "CreatedAt": "2025-01-01T07:59:00Z",
      "UpdatedAt": "2025-01-01T08:00:00Z"
    }
  ],
  "lease_until": "2025-01-01T08:01:30Z"
}
```

**说明：** 返回的 job 已被该 node 锁定（leased），需要在 `lease_until` 之前报告状态，否则会被自动重新排队。

---

### POST /api/v1/agent/jobs/:job_id/start

报告任务开始执行。

**请求：**
```json
{
  "node_id": "LAPTOP-ABC-1234",
  "lease_seconds": 90,
  "message": "queued local account 1"
}
```

**响应 200：** 空

---

### POST /api/v1/agent/jobs/:job_id/heartbeat

续约任务租约（保持任务活跃）。

**请求：**
```json
{
  "node_id": "LAPTOP-ABC-1234",
  "lease_seconds": 90,
  "message": "local queue busy, keep lease"
}
```

**响应 200：** 空

---

### POST /api/v1/agent/jobs/:job_id/complete

报告任务执行成功。

**请求：**
```json
{
  "node_id": "LAPTOP-ABC-1234",
  "message": "local batch done, account=1, tasks=[signin,explore]",
  "result": {
    "account_status": "active",
    "login_id": "game_account_001",
    "current_task": "",
    "assets": {
      "level": 150,
      "stamina": 500,
      "gouyu": 2500,
      "lanpiao": 100,
      "gold": 50000,
      "gongxun": 800,
      "xunzhang": 200,
      "tupo_ticket": 5,
      "fanhe_level": 8,
      "jiuhu_level": 6,
      "liao_level": 3
    },
    "explore_progress": {
      "1": true, "2": true, "3": false
    }
  }
}
```

**响应 200：** 空

**`result` 字段说明（complete/fail 通用）：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `account_status` | string | 账号状态：`active` / `invalid` / `cangbaoge`（来自 Oas2.0 本地状态映射） |
| `login_id` | string | 游戏账号登录标识，对应 Oas2.0 `GameAccount.login_id`，即云端 `User.LoginID` |
| `current_task` | string | 当前执行中的任务（完成时通常为空） |
| `assets` | object | **仅成功时包含**，账号资产快照（level, stamina, gouyu, lanpiao, gold, gongxun, xunzhang, tupo_ticket, fanhe_level, jiuhu_level, liao_level） |
| `explore_progress` | object | **仅成功时包含**，探索进度 `{"章节号": bool}`，如 `{"1": true, "2": false}` |

> **注意**：`result.login_id` 是 Oas2.0 本地 `GameAccount.login_id` 的值，与云端 `User.LoginID` 为同一概念（游戏账号登录标识）。Oas2.0 通过 `_collect_account_result()` 构建此字段。

---

### POST /api/v1/agent/jobs/:job_id/fail

报告任务执行失败。

**请求：**
```json
{
  "node_id": "LAPTOP-ABC-1234",
  "message": "local batch failed",
  "error_code": "LOCAL_BATCH_FAILED",
  "result": {
    "account_status": "invalid",
    "login_id": "game_account_001",
    "current_task": ""
  }
}
```

**错误码说明：**

| error_code | 含义 |
|-----------|------|
| `LOCAL_BATCH_FAILED` | 本地执行失败 |
| `LOCAL_ACCOUNT_NOT_MAPPED` | 云端 user_id 无对应本地账号 |
| `LOCAL_ACCOUNT_MISSING` | 任务缺少账号标识 |
| `TASK_TYPE_INVALID` | 不支持的任务类型 |
| `LOCAL_EXEC_FAIL` | 通用执行错误 |

**响应 200：** 空

---

### GET /api/v1/agent/users/:user_id/full-config

获取用户完整配置（任务+休息+阵容+式神+探索进度）。

**响应：**
```json
{
  "data": {
    "login_id": "1",
    "task_config": {
      "signin": {"enabled": true, "priority": 50},
      "explore": {"enabled": true, "priority": 30}
    },
    "rest_config": {
      "enabled": true,
      "mode": "random",
      "rest_start": "02:00",
      "rest_duration": 3
    },
    "lineup_config": {
      "formation_1": [1, 2, 3, 4, 5]
    },
    "shikigami_config": {},
    "explore_progress": {
      "1": true, "2": true
    }
  }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| login_id | string | 用户的登录编号，对应本地 `putonglogindata/{login_id}/` 目录 |

---

### PATCH /api/v1/agent/users/:user_id/game-profile

更新用户游戏资料。

**请求：**
```json
{
  "archive_status": "active",       // 可选
  "server": "不知火服",              // 可选
  "username": "游戏昵称"             // 可选
}
```

**响应 200：** 空

---

### PUT /api/v1/agent/users/:user_id/explore-progress

更新用户探索进度。

**请求：**
```json
{
  "progress": {
    "1": true,
    "2": true,
    "3": false
  }
}
```

**响应 200：** 空

---

### POST /api/v1/agent/users/:user_id/logs

批量上报执行日志。

**请求：**
```json
{
  "logs": [
    {
      "type": "signin",
      "level": "INFO",
      "message": "batch completed: signin",
      "ts": "2025-01-01T12:34:56.789000Z"
    },
    {
      "type": "explore",
      "level": "WARNING",
      "message": "batch failed: explore",
      "ts": "2025-01-01T12:35:10.123000Z"
    }
  ]
}
```

**响应 200：** 空

---

## 7. 云端协同流程

以下是 Oas2.0 客户端在云端模式下与 oasbackend 的完整交互流程：

```
┌─────────────┐                              ┌──────────────┐
│   Oas2.0    │                              │  oasbackend  │
│  (本地客户端)  │                              │  (云端后端)    │
└──────┬──────┘                              └──────┬───────┘
       │                                            │
       │  1. POST /agent/auth/login                 │
       │    {username, password, node_id}           │
       │──────────────────────────────────────────→ │
       │                           {token} ←────────│
       │                                            │
       │  2. POST /agent/poll-jobs                  │
       │    {node_id, limit, lease_seconds}         │
       │──────────────────────────────────────────→ │
       │                     {jobs, lease_until} ←──│
       │                                            │
       │  3. GET /agent/users/{user_id}/full-config │
       │──────────────────────────────────────────→ │
       │              {task_config, rest_config, ...}│
       │                                       ←────│
       │                                            │
       │  4. POST /agent/jobs/{id}/start            │
       │──────────────────────────────────────────→ │
       │                                            │
       │    ┌──── 本地执行任务 ────┐                   │
       │    │ cloud_user_id 映射   │                  │
       │    │ → 本地 GameAccount  │                  │
       │    │ → WorkerActor 执行  │                  │
       │    └─────────────────────┘                  │
       │                                            │
       │  5. POST /agent/jobs/{id}/heartbeat        │
       │    (执行中定期续约)                            │
       │──────────────────────────────────────────→ │
       │                                            │
       │  6. POST /agent/jobs/{id}/complete         │
       │    {result: {assets, explore_progress}}    │
       │──────────────────────────────────────────→ │
       │                                            │
       │  7. POST /agent/users/{uid}/logs           │
       │    {logs: [{type, level, message, ts}]}    │
       │──────────────────────────────────────────→ │
       │                                            │
       │  ← 回到步骤 2 继续轮询                        │
```

### 账号映射机制

云端的 `User.ID` 与本地的 `GameAccount.id` 是不同的主键。映射关系通过本地 `GameAccount.cloud_user_id` 字段建立：

1. 云端调度器生成 job 时，`job.UserID = User.ID`（云端主键）
2. 本地 CloudTaskPoller 接收 job 后，通过 `cloud_user_id` 字段查找对应的本地 `GameAccount`
3. 找到本地账号后，使用本地 `GameAccount.id` 作为 `account_id` 传入 ExecutorService

### login_id 字段对照

云端 `User.LoginID` 与 Oas2.0 `GameAccount.login_id` 是同一概念：**游戏账号登录标识**。

| 含义 | 云端字段 | Oas2.0 字段 |
|------|---------|------------|
| 游戏账号登录标识 | `User.LoginID` | `GameAccount.login_id` |
| 云端用户主键 | `User.ID` | `GameAccount.cloud_user_id` |
| 本地账号主键 | 无直接对应 | `GameAccount.id` |

**数据流向：**
- Oas2.0 在任务完成/失败时通过 `result.login_id` 回传本地 `GameAccount.login_id` 的值
- 云端可据此追踪每个 User 对应的游戏账号登录标识

### 任务类型

| task_type | 说明 |
|-----------|------|
| `signin` | 每日签到 |
| `explore` | 探索突破 |
| `xuanshang` | 悬赏封印 |
| `digui` | 地鬼 |
| `climb_tower` | 业原火 |
| `miwen` | 秘闻副本 |
| `yuhun` | 御魂 |
| `delegate_help` | 委派/助战 |
| `collect_login_gift` | 领取登录礼包 |
| `collect_mail` | 领取邮件 |
| `liao_shop` | 寮商店 |
| `liao_coin` | 寮金币 |
| `add_friend` | 添加好友 |
| `weekly_shop` | 每周商店 |
| `collect_achievement` | 领取成就 |
| `summon_gift` | 召唤礼包 |
| `weekly_share` | 每周分享 |
| `collect_fanhe_jiuhu` | 领取饭盒酒壶 |
| `duiyi_jingcai` | 对弈竞猜 |
| `init` | 初始化 |
| `init_collect_reward` | 初始化-领取奖励 |
| `init_rent_shikigami` | 初始化-租借式神 |
| `init_newbie_quest` | 初始化-新手任务 |
| `init_exp_dungeon` | 初始化-经验副本 |
| `init_collect_jinnang` | 初始化-领取锦囊 |
| `init_shikigami_train` | 初始化-式神培养 |
| `init_fanhe_upgrade` | 初始化-饭盒升级 |

### Job 状态流转

```
pending → leased → running → success
                           → failed → (重试) pending
                   timeout → timeout_requeued → pending
```
