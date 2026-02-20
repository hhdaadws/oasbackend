package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

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
	if data["date"] != nil {
		t.Fatalf("expected date to be null, got %v", data["date"])
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

func TestManagerPutDuiyiAnswers(t *testing.T) {
	srv, db := setupTestServer(t)
	_ = createActiveManager(t, db, "mgr_duiyi_put", "password123456")
	token := loginManagerToken(t, srv, "mgr_duiyi_put", "password123456")

	// PUT answers
	putBody := map[string]any{
		"answers": map[string]any{
			"10:00": "左",
			"12:00": "右",
			"14:00": nil,
		},
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
	if answers["10:00"] != "左" {
		t.Fatalf("expected 10:00 = 左, got %v", answers["10:00"])
	}
	if answers["12:00"] != "右" {
		t.Fatalf("expected 12:00 = 右, got %v", answers["12:00"])
	}

	// GET should match
	getResp := doJSONRequest(t, srv.router, http.MethodGet, "/api/v1/manager/duiyi-answers", nil, token)
	if getResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", getResp.Code)
	}
	getData := decodeBodyMap(t, getResp.Body.Bytes())
	getDataInner, _ := getData["data"].(map[string]any)
	getAnswers, _ := getDataInner["answers"].(map[string]any)
	if getAnswers["10:00"] != "左" {
		t.Fatalf("GET: expected 10:00 = 左, got %v", getAnswers["10:00"])
	}
	if getAnswers["12:00"] != "右" {
		t.Fatalf("GET: expected 12:00 = 右, got %v", getAnswers["12:00"])
	}
}

func TestManagerPutDuiyiAnswersInvalidWindow(t *testing.T) {
	srv, db := setupTestServer(t)
	_ = createActiveManager(t, db, "mgr_duiyi_badwin", "password123456")
	token := loginManagerToken(t, srv, "mgr_duiyi_badwin", "password123456")

	putBody := map[string]any{
		"answers": map[string]any{
			"09:00": "左",
		},
	}
	resp := doJSONRequest(t, srv.router, http.MethodPut, "/api/v1/manager/duiyi-answers", putBody, token)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d, body=%s", resp.Code, resp.Body.String())
	}
}

func TestManagerPutDuiyiAnswersInvalidValue(t *testing.T) {
	srv, db := setupTestServer(t)
	_ = createActiveManager(t, db, "mgr_duiyi_badval", "password123456")
	token := loginManagerToken(t, srv, "mgr_duiyi_badval", "password123456")

	putBody := map[string]any{
		"answers": map[string]any{
			"10:00": "上",
		},
	}
	resp := doJSONRequest(t, srv.router, http.MethodPut, "/api/v1/manager/duiyi-answers", putBody, token)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d, body=%s", resp.Code, resp.Body.String())
	}
}

func TestManagerDuiyiAnswersIsolated(t *testing.T) {
	srv, db := setupTestServer(t)
	_ = createActiveManager(t, db, "mgr_duiyi_iso1", "password123456")
	_ = createActiveManager(t, db, "mgr_duiyi_iso2", "password123456")
	token1 := loginManagerToken(t, srv, "mgr_duiyi_iso1", "password123456")
	token2 := loginManagerToken(t, srv, "mgr_duiyi_iso2", "password123456")

	// Manager 1 sets answers
	putBody1 := map[string]any{
		"answers": map[string]any{
			"10:00": "左",
		},
	}
	doJSONRequest(t, srv.router, http.MethodPut, "/api/v1/manager/duiyi-answers", putBody1, token1)

	// Manager 2 sets different answers
	putBody2 := map[string]any{
		"answers": map[string]any{
			"10:00": "右",
		},
	}
	doJSONRequest(t, srv.router, http.MethodPut, "/api/v1/manager/duiyi-answers", putBody2, token2)

	// Verify isolation: manager 1 sees 左, manager 2 sees 右
	resp1 := doJSONRequest(t, srv.router, http.MethodGet, "/api/v1/manager/duiyi-answers", nil, token1)
	data1 := decodeBodyMap(t, resp1.Body.Bytes())
	answers1, _ := data1["data"].(map[string]any)["answers"].(map[string]any)
	if answers1["10:00"] != "左" {
		t.Fatalf("manager1 expected 左, got %v", answers1["10:00"])
	}

	resp2 := doJSONRequest(t, srv.router, http.MethodGet, "/api/v1/manager/duiyi-answers", nil, token2)
	data2 := decodeBodyMap(t, resp2.Body.Bytes())
	answers2, _ := data2["data"].(map[string]any)["answers"].(map[string]any)
	if answers2["10:00"] != "右" {
		t.Fatalf("manager2 expected 右, got %v", answers2["10:00"])
	}
}

func TestManagerPutDuiyiAnswersOverwrite(t *testing.T) {
	srv, db := setupTestServer(t)
	_ = createActiveManager(t, db, "mgr_duiyi_ow", "password123456")
	token := loginManagerToken(t, srv, "mgr_duiyi_ow", "password123456")

	// First PUT
	putBody1 := map[string]any{
		"answers": map[string]any{
			"10:00": "左",
			"12:00": "右",
		},
	}
	doJSONRequest(t, srv.router, http.MethodPut, "/api/v1/manager/duiyi-answers", putBody1, token)

	// Second PUT (overwrite)
	putBody2 := map[string]any{
		"answers": map[string]any{
			"10:00": "右",
			"14:00": "左",
		},
	}
	resp := doJSONRequest(t, srv.router, http.MethodPut, "/api/v1/manager/duiyi-answers", putBody2, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	body := decodeBodyMap(t, resp.Body.Bytes())
	data, _ := body["data"].(map[string]any)
	answers, _ := data["answers"].(map[string]any)

	if answers["10:00"] != "右" {
		t.Fatalf("expected 10:00 = 右 after overwrite, got %v", answers["10:00"])
	}
	if answers["14:00"] != "左" {
		t.Fatalf("expected 14:00 = 左, got %v", answers["14:00"])
	}
	// 12:00 should be nil because second PUT didn't include it
	if answers["12:00"] != nil {
		t.Fatalf("expected 12:00 = nil after overwrite, got %v", answers["12:00"])
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
