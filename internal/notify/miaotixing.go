package notify

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// NotifyRequest carries the information needed to send a notification.
type NotifyRequest struct {
	UserID    uint
	AccountNo string
	Username  string
	TaskType  string
	EventType string // "success" or "fail"
	Message   string
}

// Notifier handles sending notifications via MiaoTiXing (喵提醒).
type Notifier struct {
	client      *http.Client
	rateLimiter sync.Map      // key=miaoCode, value=time.Time
	minInterval time.Duration // minimum interval between sends per miaoCode
}

// NewNotifier creates a Notifier with sensible defaults.
func NewNotifier() *Notifier {
	return &Notifier{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		minInterval: 15 * time.Second,
	}
}

type miaoResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// SendMiaoTiXing sends a notification via the MiaoTiXing API.
// It returns nil if the request is rate-limited (silent skip).
func (n *Notifier) SendMiaoTiXing(miaoCode string, text string) error {
	if lastSend, ok := n.rateLimiter.Load(miaoCode); ok {
		if time.Since(lastSend.(time.Time)) < n.minInterval {
			slog.Debug("notification rate limited", "miao_code", miaoCode)
			return nil
		}
	}

	params := url.Values{}
	params.Set("id", miaoCode)
	params.Set("text", text)
	params.Set("type", "json")

	reqURL := "https://miaotixing.com/trigger?" + params.Encode()
	resp, err := n.client.Get(reqURL)
	if err != nil {
		return fmt.Errorf("miaotixing request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("miaotixing read response failed: %w", err)
	}

	var result miaoResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("miaotixing parse response failed: %w", err)
	}

	n.rateLimiter.Store(miaoCode, time.Now())

	if result.Code != 0 {
		return fmt.Errorf("miaotixing error code=%d msg=%s", result.Code, result.Msg)
	}
	slog.Debug("miaotixing notification sent", "miao_code", miaoCode)
	return nil
}

// BuildNotificationText constructs the push notification text from a NotifyRequest.
func BuildNotificationText(req NotifyRequest) string {
	status := "成功 ✓"
	if req.EventType == "fail" {
		status = "失败 ✗"
	}
	text := fmt.Sprintf("账号: %s\n", req.AccountNo)
	if req.Username != "" {
		text += fmt.Sprintf("角色: %s\n", req.Username)
	}
	text += fmt.Sprintf("任务: %s\n结果: %s", req.TaskType, status)
	if req.Message != "" {
		text += fmt.Sprintf("\n详情: %s", req.Message)
	}
	return text
}
