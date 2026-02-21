package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"oas-cloud-go/internal/models"
)

// currentTestWindow returns the duiyi window string for the current Beijing time.
// Returns "" if outside 10:00-22:00 BJ time.
func currentTestWindow() string {
	return currentDuiyiWindowStr(time.Now().UTC())
}

// requireCurrentWindow skips the test if the current time is outside the duiyi time range.
func requireCurrentWindow(t *testing.T) string {
	t.Helper()
	w := currentTestWindow()
	if w == "" {
		t.Skip("outside duiyi window (10:00-22:00 BJ time), skipping time-sensitive test")
	}
	return w
}

// ── Manager Duiyi Answer Tests ─────────────────────────────

func TestManagerGetDuiyiAnswersEmpty(t *testing.T) {
	srv, db := setupTestServer(t)
	manager := createActiveManager(t, db, "mgr_duiyi_empty", "password123456")
	token := loginManagerToken(t, srv, "mgr_duiyi_empty", "password123456")

	resp := doJSONRequest(t, srv.router, http.MethodGet, "/api/v1/manager/duiyi-answers", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", resp.Code, resp.Body.String())
	}

	body := decodeBodyMap(t, resp.Body.Bytes())
	data, _ := body["data"].(map[string]any)
	if data == nil {
		t.Fatalf("expected data field")
	}
	// GET response should include current_window
	if _, ok := data["current_window"]; !ok {
		t.Fatalf("expected current_window field in response")
	}
	answers, _ := data["answers"].(map[string]any)
	if answers == nil {
		t.Fatalf("expected answers field")
	}
	for _, w := range []string{"10:00", "12:00", "14:00", "16:00", "18:00", "20:00", "22:00"} {
		if answers[w] != nil {
			t.Fatalf("expected answer for %s to be null, got %v", w, answers[w])
		}
	}
	_ = manager
}

func TestManagerPutDuiyiAnswersSingleWindow(t *testing.T) {
	window := requireCurrentWindow(t)
	srv, db := setupTestServer(t)
	_ = createActiveManager(t, db, "mgr_duiyi_put", "password123456")
	token := loginManagerToken(t, srv, "mgr_duiyi_put", "password123456")

	// PUT single window answer
	putBody := map[string]any{
		"window": window,
		"answer": "左",
	}
	putResp := doJSONRequest(t, srv.router, http.MethodPut, "/api/v1/manager/duiyi-answers", putBody, token)
	if putResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", putResp.Code, putResp.Body.String())
	}

	putData := decodeBodyMap(t, putResp.Body.Bytes())
	data, _ := putData["data"].(map[string]any)
	if data["date"] == nil {
		t.Fatalf("expected date to be set")
	}
	answers, _ := data["answers"].(map[string]any)
	if answers[window] != "左" {
		t.Fatalf("expected %s = 左, got %v", window, answers[window])
	}

	// GET should match
	getResp := doJSONRequest(t, srv.router, http.MethodGet, "/api/v1/manager/duiyi-answers", nil, token)
	if getResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", getResp.Code)
	}
	getData := decodeBodyMap(t, getResp.Body.Bytes())
	getDataInner, _ := getData["data"].(map[string]any)
	getAnswers, _ := getDataInner["answers"].(map[string]any)
	if getAnswers[window] != "左" {
		t.Fatalf("GET: expected %s = 左, got %v", window, getAnswers[window])
	}
}

func TestManagerPutDuiyiAnswersWrongWindow(t *testing.T) {
	window := requireCurrentWindow(t)
	srv, db := setupTestServer(t)
	_ = createActiveManager(t, db, "mgr_duiyi_wrongwin", "password123456")
	token := loginManagerToken(t, srv, "mgr_duiyi_wrongwin", "password123456")

	// Pick a window that is NOT the current one
	wrongWindow := "10:00"
	if window == "10:00" {
		wrongWindow = "12:00"
	}

	putBody := map[string]any{
		"window": wrongWindow,
		"answer": "左",
	}
	resp := doJSONRequest(t, srv.router, http.MethodPut, "/api/v1/manager/duiyi-answers", putBody, token)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for wrong window, got %d, body=%s", resp.Code, resp.Body.String())
	}
}

func TestManagerPutDuiyiAnswersInvalidValue(t *testing.T) {
	window := requireCurrentWindow(t)
	srv, db := setupTestServer(t)
	_ = createActiveManager(t, db, "mgr_duiyi_badval", "password123456")
	token := loginManagerToken(t, srv, "mgr_duiyi_badval", "password123456")

	putBody := map[string]any{
		"window": window,
		"answer": "上",
	}
	resp := doJSONRequest(t, srv.router, http.MethodPut, "/api/v1/manager/duiyi-answers", putBody, token)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d, body=%s", resp.Code, resp.Body.String())
	}
}

func TestManagerDuiyiAnswersIsolated(t *testing.T) {
	window := requireCurrentWindow(t)
	srv, db := setupTestServer(t)
	_ = createActiveManager(t, db, "mgr_duiyi_iso1", "password123456")
	_ = createActiveManager(t, db, "mgr_duiyi_iso2", "password123456")
	token1 := loginManagerToken(t, srv, "mgr_duiyi_iso1", "password123456")
	token2 := loginManagerToken(t, srv, "mgr_duiyi_iso2", "password123456")

	// Manager 1 sets 左
	doJSONRequest(t, srv.router, http.MethodPut, "/api/v1/manager/duiyi-answers",
		map[string]any{"window": window, "answer": "左"}, token1)

	// Manager 2 sets 右
	doJSONRequest(t, srv.router, http.MethodPut, "/api/v1/manager/duiyi-answers",
		map[string]any{"window": window, "answer": "右"}, token2)

	// Verify isolation: manager 1 sees 左, manager 2 sees 右
	resp1 := doJSONRequest(t, srv.router, http.MethodGet, "/api/v1/manager/duiyi-answers", nil, token1)
	data1 := decodeBodyMap(t, resp1.Body.Bytes())
	answers1, _ := data1["data"].(map[string]any)["answers"].(map[string]any)
	if answers1[window] != "左" {
		t.Fatalf("manager1 expected 左, got %v", answers1[window])
	}

	resp2 := doJSONRequest(t, srv.router, http.MethodGet, "/api/v1/manager/duiyi-answers", nil, token2)
	data2 := decodeBodyMap(t, resp2.Body.Bytes())
	answers2, _ := data2["data"].(map[string]any)["answers"].(map[string]any)
	if answers2[window] != "右" {
		t.Fatalf("manager2 expected 右, got %v", answers2[window])
	}
}

func TestManagerPutDuiyiAnswersMerge(t *testing.T) {
	window := requireCurrentWindow(t)
	srv, db := setupTestServer(t)
	_ = createActiveManager(t, db, "mgr_duiyi_merge", "password123456")
	token := loginManagerToken(t, srv, "mgr_duiyi_merge", "password123456")

	// First PUT: 左
	doJSONRequest(t, srv.router, http.MethodPut, "/api/v1/manager/duiyi-answers",
		map[string]any{"window": window, "answer": "左"}, token)

	// Second PUT: overwrite same window to 右
	resp := doJSONRequest(t, srv.router, http.MethodPut, "/api/v1/manager/duiyi-answers",
		map[string]any{"window": window, "answer": "右"}, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	body := decodeBodyMap(t, resp.Body.Bytes())
	data, _ := body["data"].(map[string]any)
	answers, _ := data["answers"].(map[string]any)

	if answers[window] != "右" {
		t.Fatalf("expected %s = 右 after overwrite, got %v", window, answers[window])
	}
}

// ── Super Admin Blogger CRUD Tests ─────────────────────────

func TestSuperBloggerCRUD(t *testing.T) {
	srv, db := setupTestServer(t)
	createSuperAdmin(t, db, "super_blogger_crud", "superPass123")
	superResp := doJSONRequest(t, srv.router, http.MethodPost, "/api/v1/super/auth/login",
		map[string]any{"username": "super_blogger_crud", "password": "superPass123"}, "")
	superToken := extractTokenFromBody(t, superResp.Body.Bytes())

	// 1. List bloggers — should be empty
	listResp := doJSONRequest(t, srv.router, http.MethodGet, "/api/v1/super/bloggers", nil, superToken)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", listResp.Code)
	}
	listBody := decodeBodyMap(t, listResp.Body.Bytes())
	items, _ := listBody["data"].([]any)
	if len(items) != 0 {
		t.Fatalf("expected 0 bloggers, got %d", len(items))
	}

	// 2. Create blogger
	createResp := doJSONRequest(t, srv.router, http.MethodPost, "/api/v1/super/bloggers",
		map[string]any{"name": "测试博主A"}, superToken)
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", createResp.Code, createResp.Body.String())
	}
	createBody := decodeBodyMap(t, createResp.Body.Bytes())
	createData, _ := createBody["data"].(map[string]any)
	if createData["name"] != "测试博主A" {
		t.Fatalf("expected name=测试博主A, got %v", createData["name"])
	}
	bloggerID := createData["id"]

	// 3. List should now have 1
	listResp2 := doJSONRequest(t, srv.router, http.MethodGet, "/api/v1/super/bloggers", nil, superToken)
	listBody2 := decodeBodyMap(t, listResp2.Body.Bytes())
	items2, _ := listBody2["data"].([]any)
	if len(items2) != 1 {
		t.Fatalf("expected 1 blogger, got %d", len(items2))
	}

	// 4. Create duplicate name → 409
	dupResp := doJSONRequest(t, srv.router, http.MethodPost, "/api/v1/super/bloggers",
		map[string]any{"name": "测试博主A"}, superToken)
	if dupResp.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate name, got %d", dupResp.Code)
	}

	// 5. Delete blogger
	deleteURL := fmt.Sprintf("/api/v1/super/bloggers/%v", bloggerID)
	deleteResp := doJSONRequest(t, srv.router, http.MethodDelete, deleteURL, nil, superToken)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", deleteResp.Code, deleteResp.Body.String())
	}

	// 6. List should be empty again
	listResp3 := doJSONRequest(t, srv.router, http.MethodGet, "/api/v1/super/bloggers", nil, superToken)
	listBody3 := decodeBodyMap(t, listResp3.Body.Bytes())
	items3, _ := listBody3["data"].([]any)
	if len(items3) != 0 {
		t.Fatalf("expected 0 bloggers after delete, got %d", len(items3))
	}
}

func TestSuperDeleteBloggerCleansUpUserReferences(t *testing.T) {
	srv, db := setupTestServer(t)
	createSuperAdmin(t, db, "super_blog_cleanup", "superPass123")
	superResp := doJSONRequest(t, srv.router, http.MethodPost, "/api/v1/super/auth/login",
		map[string]any{"username": "super_blog_cleanup", "password": "superPass123"}, "")
	superToken := extractTokenFromBody(t, superResp.Body.Bytes())

	// Create a blogger
	createResp := doJSONRequest(t, srv.router, http.MethodPost, "/api/v1/super/bloggers",
		map[string]any{"name": "即将删除博主"}, superToken)
	createBody := decodeBodyMap(t, createResp.Body.Bytes())
	bloggerIDFloat, _ := createBody["data"].(map[string]any)["id"].(float64)
	bloggerID := uint(bloggerIDFloat)

	// Create a user who references this blogger
	manager := createActiveManager(t, db, "mgr_blog_cleanup", "password123456")
	user := models.User{
		AccountNo:         "U_BLOG_CLEANUP",
		ManagerID:         manager.ID,
		UserType:          models.UserTypeDuiyi,
		Status:            models.UserStatusActive,
		ExpiresAt:         ptrTime(time.Now().UTC().Add(7 * 24 * time.Hour)),
		DuiyiAnswerSource: "blogger",
		DuiyiBloggerID:    &bloggerID,
		CreatedBy:         "test",
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	// Delete the blogger
	deleteURL := fmt.Sprintf("/api/v1/super/bloggers/%d", bloggerID)
	deleteResp := doJSONRequest(t, srv.router, http.MethodDelete, deleteURL, nil, superToken)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", deleteResp.Code)
	}

	// Verify user was reset to manager source
	var updatedUser models.User
	if err := db.Where("id = ?", user.ID).First(&updatedUser).Error; err != nil {
		t.Fatalf("query user failed: %v", err)
	}
	if updatedUser.DuiyiAnswerSource != "manager" {
		t.Fatalf("expected user source reset to manager, got %s", updatedUser.DuiyiAnswerSource)
	}
	if updatedUser.DuiyiBloggerID != nil {
		t.Fatalf("expected user blogger_id reset to nil, got %v", updatedUser.DuiyiBloggerID)
	}
}

// ── Manager Blogger Answer Tests ───────────────────────────

func TestManagerListBloggers(t *testing.T) {
	srv, db := setupTestServer(t)
	// Create a blogger directly in DB
	blogger := models.Blogger{
		Name:      "博主列表测试",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := db.Create(&blogger).Error; err != nil {
		t.Fatalf("create blogger failed: %v", err)
	}

	_ = createActiveManager(t, db, "mgr_list_bloggers", "password123456")
	token := loginManagerToken(t, srv, "mgr_list_bloggers", "password123456")

	resp := doJSONRequest(t, srv.router, http.MethodGet, "/api/v1/manager/bloggers", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", resp.Code, resp.Body.String())
	}
	body := decodeBodyMap(t, resp.Body.Bytes())
	items, _ := body["data"].([]any)
	if len(items) < 1 {
		t.Fatalf("expected at least 1 blogger, got %d", len(items))
	}
	first, _ := items[0].(map[string]any)
	if first["name"] != "博主列表测试" {
		t.Fatalf("expected blogger name=博主列表测试, got %v", first["name"])
	}
	// has_today_answers should be false
	if first["has_today_answers"] != false {
		t.Fatalf("expected has_today_answers=false, got %v", first["has_today_answers"])
	}
}

func TestManagerGetBloggerAnswersEmpty(t *testing.T) {
	srv, db := setupTestServer(t)
	blogger := models.Blogger{
		Name:      "博主答案空测试",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := db.Create(&blogger).Error; err != nil {
		t.Fatalf("create blogger failed: %v", err)
	}

	_ = createActiveManager(t, db, "mgr_get_blog_ans", "password123456")
	token := loginManagerToken(t, srv, "mgr_get_blog_ans", "password123456")

	url := fmt.Sprintf("/api/v1/manager/blogger-answers/%d", blogger.ID)
	resp := doJSONRequest(t, srv.router, http.MethodGet, url, nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", resp.Code, resp.Body.String())
	}
	body := decodeBodyMap(t, resp.Body.Bytes())
	data, _ := body["data"].(map[string]any)
	if data["blogger_name"] != "博主答案空测试" {
		t.Fatalf("expected blogger_name, got %v", data["blogger_name"])
	}
	answers, _ := data["answers"].(map[string]any)
	for _, w := range []string{"10:00", "12:00", "14:00", "16:00", "18:00", "20:00", "22:00"} {
		if answers[w] != nil {
			t.Fatalf("expected answer for %s to be nil, got %v", w, answers[w])
		}
	}
}

func TestManagerPutBloggerAnswer(t *testing.T) {
	window := requireCurrentWindow(t)
	srv, db := setupTestServer(t)
	blogger := models.Blogger{
		Name:      "博主答案配置测试",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := db.Create(&blogger).Error; err != nil {
		t.Fatalf("create blogger failed: %v", err)
	}

	_ = createActiveManager(t, db, "mgr_put_blog_ans", "password123456")
	token := loginManagerToken(t, srv, "mgr_put_blog_ans", "password123456")

	url := fmt.Sprintf("/api/v1/manager/blogger-answers/%d", blogger.ID)
	putBody := map[string]any{
		"window": window,
		"answer": "右",
	}
	resp := doJSONRequest(t, srv.router, http.MethodPut, url, putBody, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", resp.Code, resp.Body.String())
	}
	body := decodeBodyMap(t, resp.Body.Bytes())
	data, _ := body["data"].(map[string]any)
	answers, _ := data["answers"].(map[string]any)
	if answers[window] != "右" {
		t.Fatalf("expected %s = 右, got %v", window, answers[window])
	}

	// GET should reflect
	getResp := doJSONRequest(t, srv.router, http.MethodGet, url, nil, token)
	getBody := decodeBodyMap(t, getResp.Body.Bytes())
	getData, _ := getBody["data"].(map[string]any)
	getAnswers, _ := getData["answers"].(map[string]any)
	if getAnswers[window] != "右" {
		t.Fatalf("GET: expected %s = 右, got %v", window, getAnswers[window])
	}
}

func TestManagerPutBloggerAnswerWrongWindow(t *testing.T) {
	window := requireCurrentWindow(t)
	srv, db := setupTestServer(t)
	blogger := models.Blogger{
		Name:      "博主窗口限制测试",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := db.Create(&blogger).Error; err != nil {
		t.Fatalf("create blogger failed: %v", err)
	}

	_ = createActiveManager(t, db, "mgr_blog_wrongwin", "password123456")
	token := loginManagerToken(t, srv, "mgr_blog_wrongwin", "password123456")

	wrongWindow := "10:00"
	if window == "10:00" {
		wrongWindow = "12:00"
	}

	url := fmt.Sprintf("/api/v1/manager/blogger-answers/%d", blogger.ID)
	resp := doJSONRequest(t, srv.router, http.MethodPut, url,
		map[string]any{"window": wrongWindow, "answer": "左"}, token)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for wrong window, got %d, body=%s", resp.Code, resp.Body.String())
	}
}

func TestManagerBloggerAnswerSharedAcrossManagers(t *testing.T) {
	window := requireCurrentWindow(t)
	srv, db := setupTestServer(t)
	blogger := models.Blogger{
		Name:      "共享答案测试",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := db.Create(&blogger).Error; err != nil {
		t.Fatalf("create blogger failed: %v", err)
	}

	_ = createActiveManager(t, db, "mgr_share1", "password123456")
	_ = createActiveManager(t, db, "mgr_share2", "password123456")
	token1 := loginManagerToken(t, srv, "mgr_share1", "password123456")
	token2 := loginManagerToken(t, srv, "mgr_share2", "password123456")

	// Manager 1 configures blogger answer
	url := fmt.Sprintf("/api/v1/manager/blogger-answers/%d", blogger.ID)
	putResp := doJSONRequest(t, srv.router, http.MethodPut, url,
		map[string]any{"window": window, "answer": "左"}, token1)
	if putResp.Code != http.StatusOK {
		t.Fatalf("manager1 PUT expected 200, got %d", putResp.Code)
	}

	// Manager 2 should see the same answer via GET
	getResp := doJSONRequest(t, srv.router, http.MethodGet, url, nil, token2)
	if getResp.Code != http.StatusOK {
		t.Fatalf("manager2 GET expected 200, got %d", getResp.Code)
	}
	body := decodeBodyMap(t, getResp.Body.Bytes())
	data, _ := body["data"].(map[string]any)
	answers, _ := data["answers"].(map[string]any)
	if answers[window] != "左" {
		t.Fatalf("manager2 should see manager1's answer: expected %s=左, got %v", window, answers[window])
	}
}

// ── User Duiyi Answer Source Tests ─────────────────────────

func TestUserGetDuiyiAnswerSources(t *testing.T) {
	srv, db := setupTestServer(t)
	// Create blogger
	blogger := models.Blogger{
		Name:      "用户来源测试博主",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := db.Create(&blogger).Error; err != nil {
		t.Fatalf("create blogger failed: %v", err)
	}

	manager := createActiveManager(t, db, "mgr_user_source", "password123456")
	user := models.User{
		AccountNo:         "U_SOURCE_GET",
		ManagerID:         manager.ID,
		UserType:          models.UserTypeDuiyi,
		Status:            models.UserStatusActive,
		ExpiresAt:         ptrTime(time.Now().UTC().Add(7 * 24 * time.Hour)),
		DuiyiAnswerSource: "manager",
		CreatedBy:         "test",
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	rawToken, _, err := srv.issueUserToken(user.ID, "test-device")
	if err != nil {
		t.Fatalf("issue user token failed: %v", err)
	}

	resp := doJSONRequest(t, srv.router, http.MethodGet, "/api/v1/user/duiyi-answer-sources", nil, rawToken)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", resp.Code, resp.Body.String())
	}
	body := decodeBodyMap(t, resp.Body.Bytes())
	data, _ := body["data"].(map[string]any)
	if data["current_source"] != "manager" {
		t.Fatalf("expected current_source=manager, got %v", data["current_source"])
	}
	bloggers, _ := data["bloggers"].([]any)
	if len(bloggers) < 1 {
		t.Fatalf("expected at least 1 blogger in list, got %d", len(bloggers))
	}
}

func TestUserPutDuiyiAnswerSourceBlogger(t *testing.T) {
	srv, db := setupTestServer(t)
	blogger := models.Blogger{
		Name:      "用户选择博主",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := db.Create(&blogger).Error; err != nil {
		t.Fatalf("create blogger failed: %v", err)
	}

	manager := createActiveManager(t, db, "mgr_user_put_src", "password123456")
	user := models.User{
		AccountNo:         "U_SOURCE_PUT",
		ManagerID:         manager.ID,
		UserType:          models.UserTypeDuiyi,
		Status:            models.UserStatusActive,
		ExpiresAt:         ptrTime(time.Now().UTC().Add(7 * 24 * time.Hour)),
		DuiyiAnswerSource: "manager",
		CreatedBy:         "test",
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	rawToken, _, err := srv.issueUserToken(user.ID, "test-device")
	if err != nil {
		t.Fatalf("issue user token failed: %v", err)
	}

	// Switch to blogger source
	putResp := doJSONRequest(t, srv.router, http.MethodPut, "/api/v1/user/duiyi-answer-source",
		map[string]any{"source": "blogger", "blogger_id": blogger.ID}, rawToken)
	if putResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", putResp.Code, putResp.Body.String())
	}
	putBody := decodeBodyMap(t, putResp.Body.Bytes())
	putData, _ := putBody["data"].(map[string]any)
	if putData["source"] != "blogger" {
		t.Fatalf("expected source=blogger, got %v", putData["source"])
	}

	// Verify via GET
	getResp := doJSONRequest(t, srv.router, http.MethodGet, "/api/v1/user/duiyi-answer-sources", nil, rawToken)
	getBody := decodeBodyMap(t, getResp.Body.Bytes())
	getData, _ := getBody["data"].(map[string]any)
	if getData["current_source"] != "blogger" {
		t.Fatalf("expected current_source=blogger after PUT, got %v", getData["current_source"])
	}
}

func TestUserPutDuiyiAnswerSourceBackToManager(t *testing.T) {
	srv, db := setupTestServer(t)
	blogger := models.Blogger{
		Name:      "切回管理员",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := db.Create(&blogger).Error; err != nil {
		t.Fatalf("create blogger failed: %v", err)
	}

	manager := createActiveManager(t, db, "mgr_user_back", "password123456")
	user := models.User{
		AccountNo:         "U_SOURCE_BACK",
		ManagerID:         manager.ID,
		UserType:          models.UserTypeDuiyi,
		Status:            models.UserStatusActive,
		ExpiresAt:         ptrTime(time.Now().UTC().Add(7 * 24 * time.Hour)),
		DuiyiAnswerSource: "blogger",
		DuiyiBloggerID:    &blogger.ID,
		CreatedBy:         "test",
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	rawToken, _, err := srv.issueUserToken(user.ID, "test-device")
	if err != nil {
		t.Fatalf("issue user token failed: %v", err)
	}

	// Switch back to manager
	putResp := doJSONRequest(t, srv.router, http.MethodPut, "/api/v1/user/duiyi-answer-source",
		map[string]any{"source": "manager"}, rawToken)
	if putResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", putResp.Code, putResp.Body.String())
	}

	// Verify user in DB
	var updatedUser models.User
	if err := db.Where("id = ?", user.ID).First(&updatedUser).Error; err != nil {
		t.Fatalf("query user failed: %v", err)
	}
	if updatedUser.DuiyiAnswerSource != "manager" {
		t.Fatalf("expected source=manager, got %s", updatedUser.DuiyiAnswerSource)
	}
	if updatedUser.DuiyiBloggerID != nil {
		t.Fatalf("expected blogger_id=nil, got %v", updatedUser.DuiyiBloggerID)
	}
}

func TestUserPutDuiyiAnswerSourceBloggerWithoutID(t *testing.T) {
	srv, db := setupTestServer(t)
	manager := createActiveManager(t, db, "mgr_user_noid", "password123456")
	user := models.User{
		AccountNo:         "U_SOURCE_NOID",
		ManagerID:         manager.ID,
		UserType:          models.UserTypeDuiyi,
		Status:            models.UserStatusActive,
		ExpiresAt:         ptrTime(time.Now().UTC().Add(7 * 24 * time.Hour)),
		DuiyiAnswerSource: "manager",
		CreatedBy:         "test",
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	rawToken, _, err := srv.issueUserToken(user.ID, "test-device")
	if err != nil {
		t.Fatalf("issue user token failed: %v", err)
	}

	// Try to set blogger source without blogger_id → 400
	resp := doJSONRequest(t, srv.router, http.MethodPut, "/api/v1/user/duiyi-answer-source",
		map[string]any{"source": "blogger"}, rawToken)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when blogger_id missing, got %d, body=%s", resp.Code, resp.Body.String())
	}
}

func TestUserPutDuiyiAnswerSourceNonexistentBlogger(t *testing.T) {
	srv, db := setupTestServer(t)
	manager := createActiveManager(t, db, "mgr_user_noexist", "password123456")
	user := models.User{
		AccountNo:         "U_SOURCE_NOEXIST",
		ManagerID:         manager.ID,
		UserType:          models.UserTypeDuiyi,
		Status:            models.UserStatusActive,
		ExpiresAt:         ptrTime(time.Now().UTC().Add(7 * 24 * time.Hour)),
		DuiyiAnswerSource: "manager",
		CreatedBy:         "test",
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	rawToken, _, err := srv.issueUserToken(user.ID, "test-device")
	if err != nil {
		t.Fatalf("issue user token failed: %v", err)
	}

	// Try to set blogger source with non-existent blogger_id → 400
	nonExistentID := uint(99999)
	resp := doJSONRequest(t, srv.router, http.MethodPut, "/api/v1/user/duiyi-answer-source",
		map[string]any{"source": "blogger", "blogger_id": nonExistentID}, rawToken)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-existent blogger, got %d, body=%s", resp.Code, resp.Body.String())
	}
}

// Helper to parse JSON response
func parseJSON(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("parse JSON failed: %v", err)
	}
	return result
}
