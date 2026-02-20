# Oas2.0 Agent API 对接文档

> 本文档仅覆盖 Oas2.0 客户端在 `RUN_MODE=cloud` 模式下需要调用的 oasbackend 端点。
> 完整 API（含 Super Admin、Manager、User 前端接口）请参阅 [api-spec.md](./api-spec.md)。

---

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

---

## 2. 认证

Agent 使用 **JWT（role=agent）** 认证，通过 Manager 凭据 + `node_id` 登录获取 token。

| Header 格式 | 有效期 |
|------------|--------|
| `Authorization: Bearer <token>` | 可配置（默认 24h） |

登录时会自动注册/更新 AgentNode 记录，并检查 Manager 是否过期。

---

## 3. 字段映射与账号对照

### 核心映射关系

云端 `User.LoginID` 与 Oas2.0 `GameAccount.login_id` 是**同一概念**：游戏账号登录标识。

| 含义 | 云端字段 | Oas2.0 字段 |
|------|---------|------------|
| 游戏账号登录标识 | `User.LoginID` | `GameAccount.login_id` |
| 云端用户主键 | `User.ID` | `GameAccount.cloud_user_id` |
| 本地账号主键 | 无直接对应 | `GameAccount.id` |

### 任务分发映射流程

```
云端 TaskJob.UserID (= User.ID)
       │
       ▼
Oas2.0 通过 GameAccount.cloud_user_id 查找
       │
       ▼
找到本地 GameAccount.id → 传入 ExecutorService 执行
```

1. 云端调度器生成 job 时，`job.UserID = User.ID`（云端主键）
2. 本地 `CloudTaskPoller` 接收 job 后，通过 `cloud_user_id` 字段查找对应的本地 `GameAccount`
3. 找到本地账号后，使用本地 `GameAccount.id` 作为 `account_id` 传入 `ExecutorService`

### login_id 回传机制

Oas2.0 在任务完成/失败时，通过 job result 中的 `login_id` 字段回传本地 `GameAccount.login_id` 的值，云端据此可追踪每个 User 对应的游戏账号登录标识。

构建逻辑见 `poller.py:_collect_account_result()`：
- 始终包含：`account_status`、`login_id`、`current_task`
- 仅成功时包含：`assets`、`explore_progress`

---

## 4. Agent 端点

### 4.1 POST /api/v1/agent/auth/login

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

### 4.2 POST /api/v1/agent/poll-jobs

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

**说明：**
- 返回的 job 已被该 node 锁定（leased），需要在 `lease_until` 之前报告状态，否则会被自动重新排队
- `UserID` 为云端 `User.ID`，Oas2.0 需通过 `GameAccount.cloud_user_id` 查找本地账号

**Payload 特殊字段 — 对弈竞猜（`duiyi_jingcai`）：**

当 `TaskType` 为 `duiyi_jingcai` 时，`Payload` 中会额外包含 `"answer"` 字段，值为 `"左"` 或 `"右"`。此字段由云端调度器在生成任务时根据管理员配置的当前时间窗口答案自动注入。示例：

```json
{
  "Payload": {
    "user_id": 5,
    "source": "cloud_scheduler",
    "answer": "左"
  }
}
```

> 如果管理员未为当前时间窗口配置答案，则该时间窗口不会生成 `duiyi_jingcai` 任务。

---

### 4.3 POST /api/v1/agent/jobs/:job_id/start

报告任务开始执行。

**请求：**
```json
{
  "node_id": "LAPTOP-ABC-1234",
  "lease_seconds": 90,
  "message": "已入队本地账号 1"
}
```

**响应 200：** 空

---

### 4.4 POST /api/v1/agent/jobs/:job_id/heartbeat

续约任务租约（保持任务活跃）。

**请求：**
```json
{
  "node_id": "LAPTOP-ABC-1234",
  "lease_seconds": 90,
  "message": "本地队列繁忙，保持租约"
}
```

**响应 200：** 空

---

### 4.5 POST /api/v1/agent/jobs/:job_id/complete

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

**`result` 字段说明：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `account_status` | string | 账号状态：`active` / `invalid` / `cangbaoge`（来自 Oas2.0 本地状态映射） |
| `login_id` | string | 游戏账号登录标识，对应 Oas2.0 `GameAccount.login_id`，即云端 `User.LoginID` |
| `current_task` | string | 当前执行中的任务（完成时通常为空） |
| `assets` | object | **仅成功时包含**，账号资产快照 |
| `explore_progress` | object | **仅成功时包含**，探索进度 `{"章节号": bool}` |

**通知行为：** 任务完成后，后端会异步检查该用户的 `notify_config`，若启用了微信通知（`wechat_enabled=true` 且 `wechat_miao_code` 非空），则通过喵提醒 API 向用户微信推送任务完成通知。

---

### 4.6 POST /api/v1/agent/jobs/:job_id/fail

报告任务执行失败。

**请求：**
```json
{
  "node_id": "LAPTOP-ABC-1234",
  "message": "本地批次失败",
  "error_code": "LOCAL_BATCH_FAILED",
  "result": {
    "account_status": "invalid",
    "login_id": "game_account_001",
    "current_task": ""
  }
}
```

**响应 200：** 空

**错误码说明：**

| error_code | 含义 |
|-----------|------|
| `LOCAL_BATCH_FAILED` | 本地执行失败 |
| `LOCAL_ACCOUNT_NOT_MAPPED` | 云端 user_id 无对应本地账号 |
| `LOCAL_ACCOUNT_MISSING` | 任务缺少账号标识 |
| `TASK_TYPE_INVALID` | 不支持的任务类型 |
| `LOCAL_EXEC_FAIL` | 通用执行错误 |

**通知行为：** 任务失败后，后端同样会异步检查该用户的通知配置并推送失败通知（与 complete 接口行为一致）。

---

### 4.7 GET /api/v1/agent/users/:user_id/full-config

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
      "逢魔": {"group": 1, "position": 3},
      "地鬼": {"group": 2, "position": 1},
      "探索": {"group": 0, "position": 0},
      "结界突破": {"group": 0, "position": 0},
      "道馆": {"group": 0, "position": 0},
      "秘闻": {"group": 0, "position": 0},
      "御魂": {"group": 3, "position": 2}
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

### 4.8 PATCH /api/v1/agent/users/:user_id/game-profile

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

### 4.9 PUT /api/v1/agent/users/:user_id/explore-progress

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

### 4.10 POST /api/v1/agent/users/:user_id/logs

批量上报执行日志。

**请求：**
```json
{
  "logs": [
    {
      "type": "signin",
      "level": "INFO",
      "message": "批次完成: signin",
      "ts": "2025-01-01T12:34:56.789000Z"
    },
    {
      "type": "explore",
      "level": "WARNING",
      "message": "批次失败: explore",
      "ts": "2025-01-01T12:35:10.123000Z"
    }
  ]
}
```

**响应 200：** 空

---

## 4.5 扫码任务端点（Agent Scan API）

> 以下端点用于 Oas2.0 ScanTaskPoller 轮询和执行扫码任务。认证方式同普通 Agent JWT。

### 4.11 POST /api/v1/agent/scan/poll

轮询待执行的扫码任务。FIFO 顺序分配，自动清理过期租约。

**请求：**
```json
{
  "node_id": "LAPTOP-ABC-1234",
  "limit": 1,
  "lease_seconds": 120
}
```

**响应：**
```json
{
  "data": {
    "jobs": [
      {
        "scan_job_id": 42,
        "user_id": 5,
        "login_id": "myaccount001",
        "lease_until": "2026-02-20T12:02:00Z"
      }
    ],
    "lease_until": "2026-02-20T12:02:00Z"
  }
}
```

**说明：** `login_id` 对应本地 `putonglogindata/{login_id}/` 目录，扫码完成后数据存储到此目录。

---

### 4.12 POST /api/v1/agent/scan/:scan_id/start

报告扫码任务开始执行。

**请求：**
```json
{
  "node_id": "LAPTOP-ABC-1234",
  "lease_seconds": 120
}
```

**响应 200：** `{"data": {"message": "ok"}}`

---

### 4.13 POST /api/v1/agent/scan/:scan_id/phase

更新扫码阶段并上传截图。自动刷新租约、清除上一阶段的用户选择缓存。

**请求：**
```json
{
  "node_id": "LAPTOP-ABC-1234",
  "phase": "qrcode_ready",
  "screenshot": "<base64 PNG>",
  "screenshot_key": "qrcode"
}
```

**Phase 枚举：** `waiting` → `launching` → `qrcode_ready` → `choose_system` → `choose_zone` → `choose_role`（可选） → `entering` → `pulling_data` → `done`

**说明：** `screenshot_key` 决定截图存储的键名（`qrcode` / `xuanqu` / `role`），前端根据此键获取对应截图。

---

### 4.14 GET /api/v1/agent/scan/:scan_id/choice

轮询用户选择结果。同时返回任务取消状态和用户在线状态。

**查询参数：** `?node_id=LAPTOP-ABC-1234`（必选）

**响应：**
```json
{
  "data": {
    "has_choice": true,
    "choice_type": "system",
    "value": "ios",
    "cancelled": false,
    "user_online": true
  }
}
```

**Agent 使用模式：** 轮询此接口（每2秒），当 `has_choice=true` 且 `choice_type` 匹配当前等待类型时，读取 `value`。当 `cancelled=true` 或 `user_online=false` 时应中止任务。

---

### 4.15 POST /api/v1/agent/scan/:scan_id/heartbeat

续约扫码任务租约。

**请求：**
```json
{
  "node_id": "LAPTOP-ABC-1234",
  "lease_seconds": 120
}
```

**响应 200：** `{"data": {"message": "ok"}}`

---

### 4.16 POST /api/v1/agent/scan/:scan_id/complete

报告扫码任务完成。释放租约，通过 WebSocket 通知前端用户。

**请求：**
```json
{
  "node_id": "LAPTOP-ABC-1234",
  "message": "扫码完成: login_id=myaccount001"
}
```

**响应 200：** `{"data": {"message": "ok"}}`

---

### 4.17 POST /api/v1/agent/scan/:scan_id/fail

报告扫码任务失败。如果 attempts < max_attempts (默认3)，自动重置为 pending 进行重试。

**请求：**
```json
{
  "node_id": "LAPTOP-ABC-1234",
  "message": "扫码超时",
  "error_code": "EXECUTOR_ERROR"
}
```

**error_code 枚举：**
| error_code | 含义 |
|-----------|------|
| `CANCELLED` | 用户取消 |
| `EXECUTOR_ERROR` | 执行器通用错误 |

**响应 200：** `{"data": {"message": "ok"}}`

---

## 5. 公共端点（无认证）

### GET /health

健康检查（含 Redis 和数据库连通性检测）。

**响应：**
```json
// 正常
{"status": "ok", "redis": "up", "db": "up"}
// Redis 或数据库不可用
{"status": "degraded", "redis": "up", "db": "down"}  // HTTP 503
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

## 6. 云端协同流程

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
       │    {result: {login_id, assets, ...}}       │
       │──────────────────────────────────────────→ │
       │                                            │
       │  7. POST /agent/users/{uid}/logs           │
       │    {logs: [{type, level, message, ts}]}    │
       │──────────────────────────────────────────→ │
       │                                            │
       │  ← 回到步骤 2 继续轮询                        │
```

### Job 状态流转

```
pending → leased → running → success
                           → failed → (重试) pending
                   timeout → timeout_requeued → pending
```

---

## 7. 附录

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

### 错误码

| error_code | 含义 |
|-----------|------|
| `LOCAL_BATCH_FAILED` | 本地执行失败 |
| `LOCAL_ACCOUNT_NOT_MAPPED` | 云端 user_id 无对应本地账号 |
| `LOCAL_ACCOUNT_MISSING` | 任务缺少账号标识 |
| `TASK_TYPE_INVALID` | 不支持的任务类型 |
| `LOCAL_EXEC_FAIL` | 通用执行错误 |
| `CANCELLED` | 扫码被用户取消 |
| `EXECUTOR_ERROR` | 扫码执行器通用错误 |

### ScanJob 状态流转

```
pending → leased → running → success
                           → failed → (attempts < max ? pending : failed)
                           → cancelled (用户取消 / 用户离开)
                           → expired (总超时 15分钟)
                   timeout → (attempts < max ? pending : expired)
```

**Phase 流转：**
```
waiting → launching → qrcode_ready → choose_system → choose_zone
   → [choose_role] → entering → pulling_data → done
```

### 扫码协同流程

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│   用户前端    │     │  oasbackend  │     │   Oas2.0     │
└──────┬───────┘     └──────┬───────┘     └──────┬───────┘
       │ POST /user/scan/create │                 │
       │───────────────────────>│                 │
       │                        │   POST /agent/scan/poll
       │                        │<────────────────│
       │                        │   返回 ScanJob   │
       │                        │────────────────>│
       │                        │   POST /scan/:id/start
       │ WS: phase_change       │<────────────────│
       │  {launching}           │                 │
       │<───────────────────────│                 │
       │                        │   POST /scan/:id/phase
       │ WS: phase_change       │   {qrcode_ready, screenshot}
       │  {qrcode截图}          │<────────────────│
       │<───────────────────────│                 │
       │                        │                 │
       │  用户手机扫码           │                 │ 检测二维码消失
       │                        │   POST /scan/:id/phase
       │ WS: need_choice        │   {choose_system}
       │  {choice_type:system}  │<────────────────│
       │<───────────────────────│                 │
       │                        │                 │ GET /scan/:id/choice (轮询)
       │ POST /user/scan/choice │                 │
       │ {type:system,val:ios}  │                 │
       │───────────────────────>│────────────────>│ has_choice=true
       │                        │                 │ 后续交互同理...
       │                        │   POST /scan/:id/complete
       │  WS: completed         │<────────────────│
       │<───────────────────────│                 │
```
