package server

import (
	"net/http"
	"testing"
	"time"

	"oas-cloud-go/internal/models"
)

func TestManagerOverviewReturnsAggregatedStats(t *testing.T) {
	srv, db := setupTestServer(t)

	manager := createActiveManager(t, db, "manager_overview", "passwordOverview123")
	now := time.Now().UTC()

	users := []models.User{
		{
			AccountNo: "U_OVERVIEW_ACTIVE",
			ManagerID: manager.ID,
			LoginID:   "1",
			UserType:  models.UserTypeDaily,
			Status:    models.UserStatusActive,
			ExpiresAt: ptrTime(now.Add(10 * 24 * time.Hour)),
			CreatedBy: "manager_create",
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			AccountNo: "U_OVERVIEW_EXPIRED",
			ManagerID: manager.ID,
			LoginID:   "2",
			UserType:  models.UserTypeShuaka,
			Status:    models.UserStatusExpired,
			ExpiresAt: ptrTime(now.Add(-24 * time.Hour)),
			CreatedBy: "manager_create",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("create users failed: %v", err)
	}

	jobs := []models.TaskJob{
		{
			ManagerID:   manager.ID,
			UserID:      users[0].ID,
			TaskType:    "sign_in",
			Priority:    50,
			ScheduledAt: now,
			Status:      models.JobStatusPending,
			MaxAttempts: 3,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ManagerID:   manager.ID,
			UserID:      users[0].ID,
			TaskType:    "guild",
			Priority:    60,
			ScheduledAt: now,
			Status:      models.JobStatusRunning,
			MaxAttempts: 3,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}
	if err := db.Create(&jobs).Error; err != nil {
		t.Fatalf("create jobs failed: %v", err)
	}
	failEvent := models.TaskJobEvent{
		JobID:     jobs[1].ID,
		EventType: "fail",
		Message:   "boom",
		EventAt:   now,
	}
	if err := db.Create(&failEvent).Error; err != nil {
		t.Fatalf("create fail event failed: %v", err)
	}

	loginResp := doJSONRequest(
		t,
		srv.router,
		http.MethodPost,
		"/api/v1/manager/auth/login",
		map[string]any{
			"username": manager.Username,
			"password": "passwordOverview123",
		},
		"",
	)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("manager login failed, status=%d, body=%s", loginResp.Code, loginResp.Body.String())
	}
	token := extractTokenFromBody(t, loginResp.Body.Bytes())
	if token == "" {
		t.Fatalf("token should not be empty")
	}

	overviewResp := doJSONRequest(
		t,
		srv.router,
		http.MethodGet,
		"/api/v1/manager/overview",
		nil,
		token,
	)
	if overviewResp.Code != http.StatusOK {
		t.Fatalf("overview should success, status=%d body=%s", overviewResp.Code, overviewResp.Body.String())
	}
	payload := decodeBodyMap(t, overviewResp.Body.Bytes())
	userStats, ok := payload["user_stats"].(map[string]any)
	if !ok {
		t.Fatalf("user_stats should be an object")
	}
	if int(userStats["total"].(float64)) != 2 {
		t.Fatalf("expected user total 2, got %v", userStats["total"])
	}
	jobStats, ok := payload["job_stats"].(map[string]any)
	if !ok {
		t.Fatalf("job_stats should be an object")
	}
	if int(jobStats["pending"].(float64)) != 1 || int(jobStats["running"].(float64)) != 1 {
		t.Fatalf("unexpected job stats: %+v", jobStats)
	}
	if int(payload["recent_failures_24h"].(float64)) != 1 {
		t.Fatalf("expected recent_failures_24h=1, got %v", payload["recent_failures_24h"])
	}
}

func TestManagerListTaskPool(t *testing.T) {
	srv, db := setupTestServer(t)

	manager := createActiveManager(t, db, "manager_pool", "passwordPool123")
	now := time.Now().UTC()

	users := []models.User{
		{
			AccountNo: "POOL_USER_A",
			ManagerID: manager.ID,
			LoginID:   "1",
			UserType:  models.UserTypeDaily,
			Status:    models.UserStatusActive,
			Server:    "望辉",
			Username:  "playerA",
			ExpiresAt: ptrTime(now.Add(10 * 24 * time.Hour)),
			CreatedBy: "manager_create",
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			AccountNo: "POOL_USER_B",
			ManagerID: manager.ID,
			LoginID:   "2",
			UserType:  models.UserTypeShuaka,
			Status:    models.UserStatusActive,
			Server:    "雷鸣",
			Username:  "playerB",
			ExpiresAt: ptrTime(now.Add(10 * 24 * time.Hour)),
			CreatedBy: "manager_create",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("create users failed: %v", err)
	}

	jobs := []models.TaskJob{
		{
			ManagerID:   manager.ID,
			UserID:      users[0].ID,
			TaskType:    "签到",
			Priority:    50,
			ScheduledAt: now,
			Status:      models.JobStatusPending,
			MaxAttempts: 3,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ManagerID:   manager.ID,
			UserID:      users[0].ID,
			TaskType:    "寄养",
			Priority:    60,
			ScheduledAt: now,
			Status:      models.JobStatusRunning,
			MaxAttempts: 3,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ManagerID:   manager.ID,
			UserID:      users[1].ID,
			TaskType:    "签到",
			Priority:    50,
			ScheduledAt: now,
			Status:      models.JobStatusSuccess,
			MaxAttempts: 3,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}
	if err := db.Create(&jobs).Error; err != nil {
		t.Fatalf("create jobs failed: %v", err)
	}

	loginResp := doJSONRequest(t, srv.router, http.MethodPost, "/api/v1/manager/auth/login",
		map[string]any{"username": "manager_pool", "password": "passwordPool123"}, "")
	if loginResp.Code != http.StatusOK {
		t.Fatalf("login failed: status=%d body=%s", loginResp.Code, loginResp.Body.String())
	}
	token := extractTokenFromBody(t, loginResp.Body.Bytes())

	// Default query returns active tasks (pending + leased + running)
	resp := doJSONRequest(t, srv.router, http.MethodGet, "/api/v1/manager/task-pool", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("task-pool should succeed, status=%d body=%s", resp.Code, resp.Body.String())
	}
	payload := decodeBodyMap(t, resp.Body.Bytes())
	items, ok := payload["items"].([]any)
	if !ok {
		t.Fatalf("items should be an array, got %T", payload["items"])
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 active jobs, got %d", len(items))
	}

	// Verify first item has user info (sorted by priority desc, so 寄养 with priority 60 first)
	first := items[0].(map[string]any)
	if first["account_no"] != "POOL_USER_A" {
		t.Fatalf("expected account_no=POOL_USER_A, got %v", first["account_no"])
	}
	if first["task_type"] != "寄养" {
		t.Fatalf("expected task_type=寄养, got %v", first["task_type"])
	}
	if first["server"] != "望辉" {
		t.Fatalf("expected server=望辉, got %v", first["server"])
	}

	// Verify summary
	summary, ok := payload["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary should be an object")
	}
	if int(summary["pending"].(float64)) != 1 {
		t.Fatalf("expected pending=1, got %v", summary["pending"])
	}
	if int(summary["running"].(float64)) != 1 {
		t.Fatalf("expected running=1, got %v", summary["running"])
	}

	// Filter by status=success
	resp2 := doJSONRequest(t, srv.router, http.MethodGet, "/api/v1/manager/task-pool?status=success", nil, token)
	if resp2.Code != http.StatusOK {
		t.Fatalf("task-pool status filter should succeed, status=%d body=%s", resp2.Code, resp2.Body.String())
	}
	payload2 := decodeBodyMap(t, resp2.Body.Bytes())
	items2, _ := payload2["items"].([]any)
	if len(items2) != 1 {
		t.Fatalf("expected 1 success job, got %d", len(items2))
	}
	successItem := items2[0].(map[string]any)
	if successItem["account_no"] != "POOL_USER_B" {
		t.Fatalf("expected account_no=POOL_USER_B, got %v", successItem["account_no"])
	}
}
