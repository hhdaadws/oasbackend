package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"oas-cloud-go/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// scanCooldownSteps defines cooldown durations (in seconds) per attempt index.
// Index 0 = first attempt (no cooldown), index 1 = 180s after 2nd, etc.
var scanCooldownSteps = []int{0, 180, 600, 1800, 3600}

// scanActiveStatuses are statuses that indicate a scan job is still active.
var scanActiveStatuses = []string{
	models.ScanStatusPending,
	models.ScanStatusLeased,
	models.ScanStatusRunning,
}

// ── User scan handlers ──────────────────────────────────

func (s *Server) userScanCreate(c *gin.Context) {
	var req userScanCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	userID := getUint(c, ctxUserIDKey)
	managerID := getUint(c, ctxManagerIDKey)
	ctx := c.Request.Context()
	now := time.Now().UTC()

	// Auto-fill login_id from user record if not provided
	if req.LoginID == "" {
		var user models.User
		if err := s.db.Select("login_id").Where("id = ?", userID).First(&user).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询用户信息失败"})
			return
		}
		if user.LoginID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "用户未设置登录ID，请先在账号信息中设置"})
			return
		}
		req.LoginID = user.LoginID
	}

	// Check cooldown
	count, lastAt, err := s.redisStore.GetScanCooldown(ctx, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "获取冷却状态失败"})
		return
	}
	if count > 0 && !lastAt.IsZero() {
		stepIdx := count - 1
		if stepIdx >= len(scanCooldownSteps) {
			stepIdx = len(scanCooldownSteps) - 1
		}
		cooldownSecs := scanCooldownSteps[stepIdx]
		elapsed := now.Sub(lastAt).Seconds()
		if elapsed < float64(cooldownSecs) {
			remaining := cooldownSecs - int(elapsed)
			c.JSON(http.StatusTooManyRequests, gin.H{
				"detail":            fmt.Sprintf("冷却中，请等待 %d 秒后重试", remaining),
				"cooldown_remaining_sec": remaining,
			})
			return
		}
	}

	// Check for existing active scan job
	var activeCount int64
	if err := s.db.Model(&models.ScanJob{}).
		Where("user_id = ? AND status IN ?", userID, scanActiveStatuses).
		Count(&activeCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询扫码任务失败"})
		return
	}
	if activeCount > 0 {
		c.JSON(http.StatusConflict, gin.H{"detail": "已有进行中的扫码任务"})
		return
	}

	// Create scan job
	job := models.ScanJob{
		ManagerID:   managerID,
		UserID:      userID,
		LoginID:     req.LoginID,
		Status:      models.ScanStatusPending,
		Phase:       models.ScanPhaseWaiting,
		Screenshots: datatypes.JSON("{}"),
		UserChoice:  datatypes.JSON("{}"),
		MaxAttempts: 3,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.db.Create(&job).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "创建扫码任务失败"})
		return
	}

	// Update cooldown
	_ = s.redisStore.SetScanCooldown(ctx, userID, count+1, now)

	// Calculate queue position
	var position int64
	s.db.Model(&models.ScanJob{}).
		Where("status = ? AND id < ?", models.ScanStatusPending, job.ID).
		Count(&position)

	c.JSON(http.StatusCreated, gin.H{"data": gin.H{
		"scan_job_id":       job.ID,
		"position_in_queue": position + 1,
	}})
}

func (s *Server) userScanStatus(c *gin.Context) {
	userID := getUint(c, ctxUserIDKey)
	ctx := c.Request.Context()
	now := time.Now().UTC()

	var job models.ScanJob
	err := s.db.Where("user_id = ? AND status NOT IN ?", userID,
		[]string{models.ScanStatusSuccess, models.ScanStatusFailed, models.ScanStatusCancelled, models.ScanStatusExpired}).
		Order("id DESC").First(&job).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// No active job, return cooldown info
		count, lastAt, _ := s.redisStore.GetScanCooldown(ctx, userID)
		var cooldownRemaining int
		if count > 0 && !lastAt.IsZero() {
			stepIdx := count - 1
			if stepIdx >= len(scanCooldownSteps) {
				stepIdx = len(scanCooldownSteps) - 1
			}
			cooldownSecs := scanCooldownSteps[stepIdx]
			elapsed := int(now.Sub(lastAt).Seconds())
			if elapsed < cooldownSecs {
				cooldownRemaining = cooldownSecs - elapsed
			}
		}
		c.JSON(http.StatusOK, gin.H{"data": gin.H{
			"active":             false,
			"cooldown_remaining_sec": cooldownRemaining,
		}})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询扫码状态失败"})
		return
	}

	// Calculate queue position for pending jobs
	var position int64
	if job.Status == models.ScanStatusPending {
		s.db.Model(&models.ScanJob{}).
			Where("status = ? AND id < ?", models.ScanStatusPending, job.ID).
			Count(&position)
		position++
	}

	// Parse screenshots
	var screenshots map[string]string
	_ = json.Unmarshal(job.Screenshots, &screenshots)

	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"active":            true,
		"scan_job_id":       job.ID,
		"status":            job.Status,
		"phase":             job.Phase,
		"login_id":          job.LoginID,
		"screenshots":       screenshots,
		"position_in_queue": position,
		"error_message":     job.ErrorMessage,
		"created_at":        job.CreatedAt,
	}})
}

func (s *Server) userScanChoice(c *gin.Context) {
	var req userScanChoiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	userID := getUint(c, ctxUserIDKey)
	ctx := c.Request.Context()

	var job models.ScanJob
	if err := s.db.Where("id = ? AND user_id = ?", req.ScanJobID, userID).First(&job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"detail": "扫码任务不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询扫码任务失败"})
		return
	}

	// Validate choice_type matches current phase
	validPhaseChoice := map[string]string{
		models.ScanPhaseChooseSystem: "system",
		models.ScanPhaseChooseZone:   "zone",
		models.ScanPhaseChooseRole:   "role",
	}
	expectedChoice, ok := validPhaseChoice[job.Phase]
	if !ok || expectedChoice != req.ChoiceType {
		c.JSON(http.StatusBadRequest, gin.H{"detail": fmt.Sprintf("当前阶段 %s 不接受 %s 选择", job.Phase, req.ChoiceType)})
		return
	}

	// Store choice in Redis
	if err := s.redisStore.SetScanUserChoice(ctx, req.ScanJobID, req.ChoiceType, req.Value); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "保存选择失败"})
		return
	}

	// Update DB UserChoice JSONB
	var choices map[string]string
	_ = json.Unmarshal(job.UserChoice, &choices)
	if choices == nil {
		choices = map[string]string{}
	}
	choices[req.ChoiceType] = req.Value
	choicesJSON, _ := json.Marshal(choices)
	s.db.Model(&models.ScanJob{}).Where("id = ?", req.ScanJobID).Updates(map[string]any{
		"user_choice": datatypes.JSON(choicesJSON),
		"updated_at":  time.Now().UTC(),
	})

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"message": "ok"}})
}

func (s *Server) userScanCancel(c *gin.Context) {
	var req userScanCancelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	userID := getUint(c, ctxUserIDKey)
	ctx := c.Request.Context()
	now := time.Now().UTC()

	var job models.ScanJob
	if err := s.db.Where("id = ? AND user_id = ?", req.ScanJobID, userID).First(&job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"detail": "扫码任务不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询扫码任务失败"})
		return
	}

	if job.Status == models.ScanStatusSuccess || job.Status == models.ScanStatusFailed ||
		job.Status == models.ScanStatusCancelled || job.Status == models.ScanStatusExpired {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "扫码任务已结束"})
		return
	}

	s.db.Model(&models.ScanJob{}).Where("id = ?", job.ID).Updates(map[string]any{
		"status":     models.ScanStatusCancelled,
		"updated_at": now,
	})

	// Release Redis lease if any
	if job.LeasedByNode != "" {
		_ = s.redisStore.ReleaseScanLease(ctx, job.ID, job.LeasedByNode)
	}

	// Notify via WebSocket
	s.scanWSHub.NotifyUser(job.UserID, ScanWSMessage{Type: "cancelled", Message: "用户取消扫码"})

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"message": "ok"}})
}

func (s *Server) userScanHeartbeat(c *gin.Context) {
	var req userScanHeartbeatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	userID := getUint(c, ctxUserIDKey)
	ctx := c.Request.Context()
	now := time.Now().UTC()

	var job models.ScanJob
	if err := s.db.Where("id = ? AND user_id = ?", req.ScanJobID, userID).First(&job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"detail": "扫码任务不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询扫码任务失败"})
		return
	}

	_ = s.redisStore.SetScanUserHeartbeat(ctx, req.ScanJobID)
	s.db.Model(&models.ScanJob{}).Where("id = ?", job.ID).Update("user_heartbeat", now)

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"message": "ok"}})
}

// ── Agent scan handlers ──────────────────────────────────

func (s *Server) agentScanPoll(c *gin.Context) {
	var req agentScanPollRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	managerID := getUint(c, ctxManagerIDKey)
	ctx := c.Request.Context()
	now := time.Now().UTC()

	if req.Limit <= 0 {
		req.Limit = 1
	}
	if req.Limit > 5 {
		req.Limit = 5
	}
	if req.LeaseSeconds <= 0 {
		req.LeaseSeconds = 120
	}
	leaseTTL := time.Duration(req.LeaseSeconds) * time.Second
	leaseUntil := now.Add(leaseTTL)

	leasedJobs := make([]gin.H, 0, req.Limit)
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// Clean up expired leases
		var expiredJobs []models.ScanJob
		if err := tx.Where("manager_id = ? AND status IN ? AND lease_until IS NOT NULL AND lease_until < ?",
			managerID, []string{models.ScanStatusLeased, models.ScanStatusRunning}, now).
			Find(&expiredJobs).Error; err != nil {
			return err
		}
		for _, job := range expiredJobs {
			job.Attempts++
			if job.Attempts >= job.MaxAttempts {
				job.Status = models.ScanStatusExpired
				job.ErrorMessage = "租约超时"
			} else {
				job.Status = models.ScanStatusPending
				job.Phase = models.ScanPhaseWaiting
			}
			job.LeasedByNode = ""
			job.LeaseUntil = nil
			job.UpdatedAt = now
			if err := tx.Save(&job).Error; err != nil {
				return err
			}
			_ = s.redisStore.ClearScanLease(ctx, job.ID)
		}

		// Find pending scan jobs
		var candidates []models.ScanJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("manager_id = ? AND status = ?", managerID, models.ScanStatusPending).
			Order("created_at ASC").Limit(req.Limit).Find(&candidates).Error; err != nil {
			return err
		}

		for _, job := range candidates {
			acquired, err := s.redisStore.AcquireScanLease(ctx, job.ID, req.NodeID, leaseTTL)
			if err != nil {
				return err
			}
			if !acquired {
				continue
			}

			updateResult := tx.Model(&models.ScanJob{}).
				Where("id = ? AND status = ?", job.ID, models.ScanStatusPending).
				Updates(map[string]any{
					"status":         models.ScanStatusLeased,
					"leased_by_node": req.NodeID,
					"lease_until":    leaseUntil,
					"updated_at":     now,
				})
			if updateResult.Error != nil {
				_ = s.redisStore.ReleaseScanLease(ctx, job.ID, req.NodeID)
				return updateResult.Error
			}
			if updateResult.RowsAffected == 0 {
				_ = s.redisStore.ReleaseScanLease(ctx, job.ID, req.NodeID)
				continue
			}

			leasedJobs = append(leasedJobs, gin.H{
				"scan_job_id":  job.ID,
				"user_id":      job.UserID,
				"login_id":     job.LoginID,
				"lease_until":  leaseUntil,
			})
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "获取扫码任务失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"jobs":        leasedJobs,
		"lease_until": leaseUntil,
	}})
}

func (s *Server) agentScanStart(c *gin.Context) {
	scanJobID, ok := parseUintParam(c, "scan_id")
	if !ok {
		return
	}

	var req agentScanStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	ctx := c.Request.Context()
	now := time.Now().UTC()

	owned, err := s.redisStore.IsScanLeaseOwner(ctx, scanJobID, req.NodeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "检查租约失败"})
		return
	}
	if !owned {
		c.JSON(http.StatusForbidden, gin.H{"detail": "租约不匹配"})
		return
	}

	s.db.Model(&models.ScanJob{}).Where("id = ?", scanJobID).Updates(map[string]any{
		"status":     models.ScanStatusRunning,
		"phase":      models.ScanPhaseLaunching,
		"updated_at": now,
	})

	// Notify user
	var job models.ScanJob
	if err := s.db.Where("id = ?", scanJobID).First(&job).Error; err == nil {
		s.scanWSHub.NotifyUser(job.UserID, ScanWSMessage{
			Type:  "phase",
			Phase: models.ScanPhaseLaunching,
		})
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"message": "ok"}})
}

func (s *Server) agentScanPhase(c *gin.Context) {
	scanJobID, ok := parseUintParam(c, "scan_id")
	if !ok {
		return
	}

	var req agentScanPhaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	ctx := c.Request.Context()
	now := time.Now().UTC()

	owned, err := s.redisStore.IsScanLeaseOwner(ctx, scanJobID, req.NodeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "检查租约失败"})
		return
	}
	if !owned {
		c.JSON(http.StatusForbidden, gin.H{"detail": "租约不匹配"})
		return
	}

	updates := map[string]any{
		"phase":      req.Phase,
		"updated_at": now,
	}

	// Store screenshot if provided
	if req.Screenshot != "" && req.ScreenshotKey != "" {
		var job models.ScanJob
		if err := s.db.Where("id = ?", scanJobID).First(&job).Error; err == nil {
			var screenshots map[string]string
			_ = json.Unmarshal(job.Screenshots, &screenshots)
			if screenshots == nil {
				screenshots = map[string]string{}
			}
			screenshots[req.ScreenshotKey] = req.Screenshot
			screenshotsJSON, _ := json.Marshal(screenshots)
			updates["screenshots"] = datatypes.JSON(screenshotsJSON)
		}
	}

	s.db.Model(&models.ScanJob{}).Where("id = ?", scanJobID).Updates(updates)

	// Clear previous user choice (new phase started)
	_ = s.redisStore.ClearScanUserChoice(ctx, scanJobID)

	// Refresh lease
	leaseSeconds := 120
	leaseTTL := time.Duration(leaseSeconds) * time.Second
	_, _ = s.redisStore.RefreshScanLease(ctx, scanJobID, req.NodeID, leaseTTL)

	// WebSocket push to user
	var wsJob models.ScanJob
	if err := s.db.Where("id = ?", scanJobID).First(&wsJob).Error; err == nil {
		msg := ScanWSMessage{
			Type:  "phase",
			Phase: req.Phase,
		}
		if req.Screenshot != "" {
			msg.Screenshot = req.Screenshot
		}
		s.scanWSHub.NotifyUser(wsJob.UserID, msg)
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"message": "ok"}})
}

func (s *Server) agentScanGetChoice(c *gin.Context) {
	scanJobID, ok := parseUintParam(c, "scan_id")
	if !ok {
		return
	}

	nodeID := c.Query("node_id")
	if nodeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "缺少node_id"})
		return
	}

	ctx := c.Request.Context()

	owned, err := s.redisStore.IsScanLeaseOwner(ctx, scanJobID, nodeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "检查租约失败"})
		return
	}
	if !owned {
		c.JSON(http.StatusForbidden, gin.H{"detail": "租约不匹配"})
		return
	}

	// Check if job is cancelled
	var job models.ScanJob
	if err := s.db.Where("id = ?", scanJobID).First(&job).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "扫码任务不存在"})
		return
	}
	cancelled := job.Status == models.ScanStatusCancelled

	// Check user online
	userOnline, _ := s.redisStore.IsScanUserOnline(ctx, scanJobID)

	// Get user choice from Redis
	choices, _ := s.redisStore.GetScanUserChoice(ctx, scanJobID)
	hasChoice := len(choices) > 0

	var choiceType, choiceValue string
	if hasChoice {
		for k, v := range choices {
			choiceType = k
			choiceValue = v
			break
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"has_choice":  hasChoice,
		"choice_type": choiceType,
		"value":       choiceValue,
		"cancelled":   cancelled,
		"user_online": userOnline,
	}})
}

func (s *Server) agentScanHeartbeat(c *gin.Context) {
	scanJobID, ok := parseUintParam(c, "scan_id")
	if !ok {
		return
	}

	var req agentScanHeartbeatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	ctx := c.Request.Context()
	now := time.Now().UTC()

	owned, err := s.redisStore.IsScanLeaseOwner(ctx, scanJobID, req.NodeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "检查租约失败"})
		return
	}
	if !owned {
		c.JSON(http.StatusForbidden, gin.H{"detail": "租约不匹配"})
		return
	}

	leaseSeconds := req.LeaseSeconds
	if leaseSeconds <= 0 {
		leaseSeconds = 120
	}
	leaseTTL := time.Duration(leaseSeconds) * time.Second
	leaseUntil := now.Add(leaseTTL)

	_, _ = s.redisStore.RefreshScanLease(ctx, scanJobID, req.NodeID, leaseTTL)
	s.db.Model(&models.ScanJob{}).Where("id = ?", scanJobID).Updates(map[string]any{
		"lease_until": leaseUntil,
		"updated_at":  now,
	})

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"message": "ok"}})
}

func (s *Server) agentScanComplete(c *gin.Context) {
	scanJobID, ok := parseUintParam(c, "scan_id")
	if !ok {
		return
	}

	var req agentScanCompleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	ctx := c.Request.Context()
	now := time.Now().UTC()

	owned, err := s.redisStore.IsScanLeaseOwner(ctx, scanJobID, req.NodeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "检查租约失败"})
		return
	}
	if !owned {
		c.JSON(http.StatusForbidden, gin.H{"detail": "租约不匹配"})
		return
	}

	s.db.Model(&models.ScanJob{}).Where("id = ?", scanJobID).Updates(map[string]any{
		"status":     models.ScanStatusSuccess,
		"phase":      models.ScanPhaseDone,
		"updated_at": now,
	})

	_ = s.redisStore.ReleaseScanLease(ctx, scanJobID, req.NodeID)

	// WebSocket push
	var job models.ScanJob
	if err := s.db.Where("id = ?", scanJobID).First(&job).Error; err == nil {
		s.scanWSHub.NotifyUser(job.UserID, ScanWSMessage{
			Type:    "completed",
			Phase:   models.ScanPhaseDone,
			Message: req.Message,
		})
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"message": "ok"}})
}

func (s *Server) agentScanFail(c *gin.Context) {
	scanJobID, ok := parseUintParam(c, "scan_id")
	if !ok {
		return
	}

	var req agentScanFailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	ctx := c.Request.Context()
	now := time.Now().UTC()

	owned, err := s.redisStore.IsScanLeaseOwner(ctx, scanJobID, req.NodeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "检查租约失败"})
		return
	}
	if !owned {
		c.JSON(http.StatusForbidden, gin.H{"detail": "租约不匹配"})
		return
	}

	var job models.ScanJob
	if err := s.db.Where("id = ?", scanJobID).First(&job).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "扫码任务不存在"})
		return
	}

	job.Attempts++
	errMsg := req.Message
	if errMsg == "" {
		errMsg = "扫码失败"
	}

	if job.Attempts < job.MaxAttempts {
		// Can retry
		s.db.Model(&models.ScanJob{}).Where("id = ?", scanJobID).Updates(map[string]any{
			"status":        models.ScanStatusPending,
			"phase":         models.ScanPhaseWaiting,
			"attempts":      job.Attempts,
			"error_message": errMsg,
			"leased_by_node": "",
			"lease_until":   nil,
			"updated_at":    now,
		})
	} else {
		// Max attempts reached
		s.db.Model(&models.ScanJob{}).Where("id = ?", scanJobID).Updates(map[string]any{
			"status":        models.ScanStatusFailed,
			"attempts":      job.Attempts,
			"error_message": errMsg,
			"leased_by_node": "",
			"lease_until":   nil,
			"updated_at":    now,
		})
	}

	_ = s.redisStore.ReleaseScanLease(ctx, scanJobID, req.NodeID)

	s.scanWSHub.NotifyUser(job.UserID, ScanWSMessage{
		Type:    "failed",
		Message: errMsg,
	})

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"message": "ok"}})
}

// ── Scan job timeout worker ──────────────────────────────────

func (s *Server) scanJobTimeoutWorker() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now().UTC()
		ctx := context.Background()

		// 1. Lease timeout: leased/running jobs with expired lease
		var expiredLeases []models.ScanJob
		s.db.Where("status IN ? AND lease_until IS NOT NULL AND lease_until < ?",
			[]string{models.ScanStatusLeased, models.ScanStatusRunning}, now).
			Find(&expiredLeases)
		if len(expiredLeases) > 0 {
			// Split into retry vs expired groups
			retryIDs := make([]uint, 0, len(expiredLeases))
			expiredIDs := make([]uint, 0, len(expiredLeases))
			allIDs := make([]uint, 0, len(expiredLeases))
			for _, job := range expiredLeases {
				allIDs = append(allIDs, job.ID)
				if job.Attempts+1 >= job.MaxAttempts {
					expiredIDs = append(expiredIDs, job.ID)
				} else {
					retryIDs = append(retryIDs, job.ID)
				}
			}
			if len(retryIDs) > 0 {
				s.db.Model(&models.ScanJob{}).Where("id IN ?", retryIDs).Updates(map[string]any{
					"status":         models.ScanStatusPending,
					"phase":          models.ScanPhaseWaiting,
					"leased_by_node": "",
					"lease_until":    nil,
					"attempts":       gorm.Expr("attempts + 1"),
					"updated_at":     now,
				})
			}
			if len(expiredIDs) > 0 {
				s.db.Model(&models.ScanJob{}).Where("id IN ?", expiredIDs).Updates(map[string]any{
					"status":         models.ScanStatusExpired,
					"error_message":  "租约超时",
					"leased_by_node": "",
					"lease_until":    nil,
					"attempts":       gorm.Expr("attempts + 1"),
					"updated_at":     now,
				})
			}
			for _, id := range allIDs {
				_ = s.redisStore.ClearScanLease(ctx, id)
			}
		}

		// 2. User heartbeat timeout (60 seconds)
		heartbeatDeadline := now.Add(-60 * time.Second)
		var noHeartbeat []models.ScanJob
		s.db.Where("status = ? AND user_heartbeat IS NOT NULL AND user_heartbeat < ?",
			models.ScanStatusRunning, heartbeatDeadline).
			Find(&noHeartbeat)
		if len(noHeartbeat) > 0 {
			hbIDs := make([]uint, 0, len(noHeartbeat))
			for _, job := range noHeartbeat {
				hbIDs = append(hbIDs, job.ID)
			}
			s.db.Model(&models.ScanJob{}).Where("id IN ?", hbIDs).Updates(map[string]any{
				"status":        models.ScanStatusCancelled,
				"error_message": "用户离开扫码页面",
				"updated_at":    now,
			})
			for _, job := range noHeartbeat {
				if job.LeasedByNode != "" {
					_ = s.redisStore.ReleaseScanLease(ctx, job.ID, job.LeasedByNode)
				}
				s.scanWSHub.NotifyUser(job.UserID, ScanWSMessage{Type: "failed", Message: "已取消：用户离开页面"})
			}
		}

		// 3. Total timeout (15 minutes)
		totalDeadline := now.Add(-15 * time.Minute)
		s.db.Model(&models.ScanJob{}).
			Where("status IN ? AND created_at < ?", scanActiveStatuses, totalDeadline).
			Updates(map[string]any{
				"status":        models.ScanStatusExpired,
				"error_message": "总超时",
				"updated_at":    now,
			})
	}
}
