package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"oas-cloud-go/internal/cache"
	"oas-cloud-go/internal/config"
	"oas-cloud-go/internal/models"
	"oas-cloud-go/internal/taskmeta"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Stats struct {
	Running          bool      `json:"running"`
	LastRunAt        time.Time `json:"last_run_at"`
	LastGenerated    int       `json:"last_generated"`
	LastScannedUsers int       `json:"last_scanned_users"`
	LastError        string    `json:"last_error"`
}

type Generator struct {
	cfg   config.Config
	db    *gorm.DB
	store cache.Store

	running atomic.Bool
	stopCh  chan struct{}
	doneCh  chan struct{}

	statsMu sync.Mutex
	stats   Stats
}

func NewGenerator(cfg config.Config, db *gorm.DB, store cache.Store) *Generator {
	return &Generator{
		cfg:    cfg,
		db:     db,
		store:  store,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

func (g *Generator) Start() {
	if !g.cfg.SchedulerEnabled {
		return
	}
	if !g.running.CompareAndSwap(false, true) {
		return
	}
	go g.loop()
}

func (g *Generator) Stop() {
	if !g.running.CompareAndSwap(true, false) {
		return
	}
	close(g.stopCh)
	<-g.doneCh
}

func (g *Generator) Snapshot() Stats {
	g.statsMu.Lock()
	defer g.statsMu.Unlock()
	stats := g.stats
	stats.Running = g.running.Load()
	return stats
}

func (g *Generator) loop() {
	defer close(g.doneCh)
	interval := g.cfg.SchedulerInterval
	if interval < time.Second {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	g.runOnce(context.Background())
	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			g.runOnce(ctx)
			cancel()
		case <-g.stopCh:
			return
		}
	}
}

func (g *Generator) runOnce(ctx context.Context) {
	now := time.Now().UTC()
	generated := 0
	scanned := 0
	var runErr error

	// Expire stale 对弈竞猜 pending jobs whose window has passed (runs even during rest window)
	if expired, err := g.expireStaleDuiyiJobs(now); err != nil {
		slog.Warn("expire stale duiyi jobs failed", "error", err)
	} else if expired > 0 {
		slog.Info("expired stale duiyi jobs", "count", expired)
	}

	// Rest window: skip generation during Beijing time 00:00–05:59
	bjLoc := time.FixedZone("Asia/Shanghai", 8*60*60)
	bjHour := now.In(bjLoc).Hour()
	if bjHour >= 0 && bjHour < 6 {
		g.updateStats(now, 0, 0, nil)
		return
	}

	users := make([]models.User, 0, g.cfg.SchedulerScanLimit)
	query := g.db.Where("status = ? AND expires_at IS NOT NULL AND expires_at > ?", models.UserStatusActive, now).Order("id asc")
	if g.cfg.SchedulerScanLimit > 0 {
		query = query.Limit(g.cfg.SchedulerScanLimit)
	}
	if err := query.Find(&users).Error; err != nil {
		runErr = err
		g.updateStats(now, generated, scanned, runErr)
		return
	}
	scanned = len(users)
	if scanned == 0 {
		g.updateStats(now, generated, scanned, nil)
		return
	}

	// Batch preload: user task configs (replaces N individual SELECT queries)
	userIDs := make([]uint, len(users))
	for i, u := range users {
		userIDs[i] = u.ID
	}
	var configs []models.UserTaskConfig
	if err := g.db.Where("user_id IN ?", userIDs).Find(&configs).Error; err != nil {
		runErr = err
		g.updateStats(now, generated, scanned, runErr)
		return
	}
	configMap := make(map[uint]models.UserTaskConfig, len(configs))
	for _, c := range configs {
		configMap[c.UserID] = c
	}

	// Batch preload: active job counts per user+taskType (replaces N*M COUNT queries)
	type jobCountRow struct {
		UserID   uint   `gorm:"column:user_id"`
		TaskType string `gorm:"column:task_type"`
		Cnt      int64  `gorm:"column:cnt"`
	}
	var jobCounts []jobCountRow
	activeStatuses := []string{models.JobStatusPending, models.JobStatusLeased, models.JobStatusRunning}
	if err := g.db.Model(&models.TaskJob{}).
		Select("user_id, task_type, COUNT(*) as cnt").
		Where("user_id IN ? AND status IN ?", userIDs, activeStatuses).
		Group("user_id, task_type").
		Find(&jobCounts).Error; err != nil {
		runErr = err
		g.updateStats(now, generated, scanned, runErr)
		return
	}
	// Build lookup: activeJobMap[userID][taskType] = count
	activeJobMap := make(map[uint]map[string]int64, len(users))
	for _, jc := range jobCounts {
		if activeJobMap[jc.UserID] == nil {
			activeJobMap[jc.UserID] = make(map[string]int64)
		}
		activeJobMap[jc.UserID][jc.TaskType] = jc.Cnt
	}

	// Batch preload: duiyi answer configs for all relevant managers (for 对弈竞猜)
	managerIDSet := make(map[uint]struct{}, len(users))
	for _, u := range users {
		managerIDSet[u.ManagerID] = struct{}{}
	}
	managerIDs := make([]uint, 0, len(managerIDSet))
	for mid := range managerIDSet {
		managerIDs = append(managerIDs, mid)
	}
	var duiyiConfigs []models.DuiyiAnswerConfig
	todayBJ := now.In(bjLoc).Format("2006-01-02")
	if len(managerIDs) > 0 {
		if err := g.db.Where("manager_id IN ? AND date = ?", managerIDs, todayBJ).
			Find(&duiyiConfigs).Error; err != nil {
			runErr = err
			g.updateStats(now, generated, scanned, runErr)
			return
		}
	}
	duiyiAnswerMap := make(map[uint]map[string]any, len(duiyiConfigs))
	for _, dc := range duiyiConfigs {
		duiyiAnswerMap[dc.ManagerID] = map[string]any(dc.Answers)
	}

	workers := g.cfg.SchedulerWorkers
	if workers <= 0 {
		workers = 4
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, user := range users {
		cfg, hasCfg := configMap[user.ID]
		if !hasCfg {
			continue
		}
		userJobCounts := activeJobMap[user.ID]

		sem <- struct{}{}
		wg.Add(1)
		go func(u models.User, c models.UserTaskConfig, jc map[string]int64, da map[string]any) {
			defer func() { <-sem; wg.Done() }()
			userGenerated, err := g.processUser(ctx, u, c, jc, da, now)
			mu.Lock()
			if err != nil {
				runErr = err
			}
			generated += userGenerated
			mu.Unlock()
		}(user, cfg, userJobCounts, duiyiAnswerMap[user.ManagerID])
	}
	wg.Wait()

	g.updateStats(now, generated, scanned, runErr)
}

func (g *Generator) processUser(ctx context.Context, user models.User, cfg models.UserTaskConfig, activeJobCounts map[string]int64, duiyiAnswers map[string]any, now time.Time) (int, error) {
	storedTaskConfig := map[string]any(cfg.TaskConfig)
	if storedTaskConfig == nil {
		return 0, nil
	}
	taskConfig := taskmeta.NormalizeTaskConfigByType(storedTaskConfig, user.UserType)

	generated := 0
	changed := !jsonMapEqual(storedTaskConfig, taskConfig)
	for taskType, rawTaskCfg := range taskConfig {
		taskMap, ok := rawTaskCfg.(map[string]any)
		if !ok {
			continue
		}
		enabled, hasEnabled := taskMap["enabled"].(bool)
		if !hasEnabled || enabled != true {
			continue
		}

		// 对弈竞猜: skip if no answer configured for current window
		if taskType == "对弈竞猜" {
			bjHour := now.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Hour()
			window := currentDuiyiWindow(bjHour)
			if window == "" {
				continue
			}
			ans, _ := duiyiAnswers[window].(string)
			if ans != "左" && ans != "右" {
				continue
			}
		}

		due, slot, dedupeTTL, nextTime := g.evaluateDue(taskMap, now)
		if !due {
			continue
		}

		acquired, err := g.store.AcquireScheduleSlot(
			ctx,
			user.ManagerID,
			user.ID,
			taskType,
			slot,
			dedupeTTL,
		)
		if err != nil || !acquired {
			if err != nil {
				return generated, err
			}
			continue
		}

		created, err := g.createJobIfNeeded(user, taskType, taskMap, nextTime, activeJobCounts, duiyiAnswers, now)
		if err != nil {
			return generated, err
		}
		if created {
			generated += 1
			if !nextTime.IsZero() {
				taskMap["next_time"] = nextTime.In(taskmeta.BJLoc).Format("2006-01-02 15:04")
				taskConfig[taskType] = taskMap
				changed = true
			}
		}
	}

	if changed {
		if err := g.db.Model(&models.UserTaskConfig{}).
			Where("id = ?", cfg.ID).
			Updates(map[string]any{
				"task_config": datatypes.JSONMap(taskConfig),
				"updated_at":  now,
				"version":     gorm.Expr("version + 1"),
			}).Error; err != nil {
			return generated, err
		}
	}

	return generated, nil
}

func jsonMapEqual(left map[string]any, right map[string]any) bool {
	return reflect.DeepEqual(left, right)
}

func (g *Generator) createJobIfNeeded(user models.User, taskType string, taskMap map[string]any, nextTime time.Time, activeJobCounts map[string]int64, duiyiAnswers map[string]any, now time.Time) (bool, error) {
	// Use preloaded active job counts instead of individual COUNT query
	if activeJobCounts != nil && activeJobCounts[taskType] > 0 {
		return false, nil
	}

	priority := toInt(taskMap["priority"], 50)
	payload := map[string]any{
		"user_id": user.ID,
		"source":  "cloud_scheduler",
	}
	if value, ok := taskMap["payload"].(map[string]any); ok {
		for key, item := range value {
			payload[key] = item
		}
	}

	// 对弈竞猜: inject answer into payload
	if taskType == "对弈竞猜" && duiyiAnswers != nil {
		bjHour := now.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Hour()
		if window := currentDuiyiWindow(bjHour); window != "" {
			if ans, _ := duiyiAnswers[window].(string); ans == "左" || ans == "右" {
				payload["answer"] = ans
			}
		}
	}

	job := models.TaskJob{
		ManagerID:   user.ManagerID,
		UserID:      user.ID,
		TaskType:    taskType,
		Payload:     datatypes.JSONMap(payload),
		Priority:    priority,
		ScheduledAt: now,
		Status:      models.JobStatusPending,
		MaxAttempts: toInt(taskMap["max_attempts"], 3),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := g.db.Create(&job).Error; err != nil {
		return false, err
	}
	return true, nil
}

func (g *Generator) evaluateDue(task map[string]any, now time.Time) (bool, string, time.Duration, time.Time) {
	slotTTL := g.cfg.SchedulerSlotTTL
	if slotTTL < 10*time.Second {
		slotTTL = 90 * time.Second
	}
	nextRun := time.Time{}
	bjNow := now.In(taskmeta.BJLoc)

	nextRaw, hasNext := task["next_time"].(string)
	if hasNext && strings.TrimSpace(nextRaw) != "" {
		nextRaw = strings.TrimSpace(nextRaw)
		if hhmm, ok := parseHHMM(nextRaw); ok {
			// HH:MM 视为北京时间每日任务
			target := time.Date(bjNow.Year(), bjNow.Month(), bjNow.Day(), hhmm.hour, hhmm.minute, 0, 0, taskmeta.BJLoc)
			if now.Before(target) {
				return false, "", slotTTL, nextRun
			}
			slot := fmt.Sprintf("daily:%s:%02d%02d", bjNow.Format("20060102"), hhmm.hour, hhmm.minute)
			nextRun = target.Add(24 * time.Hour)
			return true, slot, 26 * time.Hour, nextRun
		}

		// YYYY-MM-DD HH:MM 视为北京时间（parseDateTime 已用 BJLoc 解析）
		parsed := parseDateTime(nextRaw)
		if parsed.IsZero() || now.Before(parsed) {
			return false, "", slotTTL, nextRun
		}
		failDelayMinutes := toInt(task["fail_delay"], 0)
		if failDelayMinutes > 0 {
			nextRun = now.Add(time.Duration(failDelayMinutes) * time.Minute)
		}
		slot := "datetime:" + parsed.In(taskmeta.BJLoc).Format("200601021504")
		return true, slot, 24 * time.Hour, nextRun
	}

	slot := "rolling:" + bjNow.Truncate(time.Minute).Format("200601021504")
	failDelayMinutes := toInt(task["fail_delay"], 0)
	if failDelayMinutes > 0 {
		nextRun = now.Add(time.Duration(failDelayMinutes) * time.Minute)
	}
	return true, slot, slotTTL, nextRun
}

func (g *Generator) updateStats(now time.Time, generated int, scanned int, err error) {
	g.statsMu.Lock()
	defer g.statsMu.Unlock()
	g.stats.LastRunAt = now
	g.stats.LastGenerated = generated
	g.stats.LastScannedUsers = scanned
	if err != nil {
		g.stats.LastError = err.Error()
	} else {
		g.stats.LastError = ""
	}
}

func toInt(value any, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return fallback
		}
		return parsed
	default:
		return fallback
	}
}

type hhmmValue struct {
	hour   int
	minute int
}

func parseHHMM(value string) (hhmmValue, bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return hhmmValue{}, false
	}
	hour, err1 := strconv.Atoi(parts[0])
	minute, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return hhmmValue{}, false
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return hhmmValue{}, false
	}
	return hhmmValue{hour: hour, minute: minute}, true
}

func parseDateTime(value string) time.Time {
	layouts := []string{
		"2006-01-02 15:04",
		time.RFC3339,
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		parsed, err := time.ParseInLocation(layout, value, taskmeta.BJLoc)
		if err == nil {
			return parsed
		}
	}
	return time.Time{}
}

// ── Duiyi (对弈竞猜) window helpers ─────────────────────

var duiyiWindows = []string{"10:00", "12:00", "14:00", "16:00", "18:00", "20:00", "22:00"}

// currentDuiyiWindow returns the applicable window for the given Beijing hour,
// or "" if outside the 10:00–22:00 range.
func currentDuiyiWindow(bjHour int) string {
	if bjHour < 10 {
		return ""
	}
	result := ""
	for _, w := range duiyiWindows {
		h, _ := strconv.Atoi(strings.Split(w, ":")[0])
		if bjHour >= h {
			result = w
		}
	}
	return result
}

// duiyiWindowStartForTime returns the start time of the duiyi window that
// contains the given Beijing time. Returns zero time if outside 10:00–23:59.
func duiyiWindowStartForTime(bjTime time.Time) time.Time {
	h := bjTime.Hour()
	if h < 10 {
		return time.Time{}
	}
	windowHour := (h / 2) * 2
	if windowHour < 10 {
		windowHour = 10
	}
	return time.Date(bjTime.Year(), bjTime.Month(), bjTime.Day(), windowHour, 0, 0, 0, taskmeta.BJLoc)
}

// expireStaleDuiyiJobs expires pending 对弈竞猜 tasks whose execution window
// has passed. A task is stale if the current window start time is after the
// task's window start time.
func (g *Generator) expireStaleDuiyiJobs(now time.Time) (int64, error) {
	bjNow := now.In(taskmeta.BJLoc)
	currentWindowStart := duiyiWindowStartForTime(bjNow)

	var staleTasks []models.TaskJob
	if err := g.db.Where("status = ? AND task_type = ?",
		models.JobStatusPending, "对弈竞猜").
		Find(&staleTasks).Error; err != nil {
		return 0, err
	}
	if len(staleTasks) == 0 {
		return 0, nil
	}

	staleIDs := make([]uint, 0, len(staleTasks))
	for _, job := range staleTasks {
		jobBJ := job.ScheduledAt.In(taskmeta.BJLoc)
		jobWindowStart := duiyiWindowStartForTime(jobBJ)
		// If the job was created outside any window (before 10:00), it's stale.
		// If the current window start is after the job's window start, it's stale.
		if jobWindowStart.IsZero() || currentWindowStart.After(jobWindowStart) {
			staleIDs = append(staleIDs, job.ID)
		}
	}
	if len(staleIDs) == 0 {
		return 0, nil
	}

	if err := g.db.Model(&models.TaskJob{}).
		Where("id IN ?", staleIDs).
		Updates(map[string]any{
			"status":     models.JobStatusFailed,
			"updated_at": now,
		}).Error; err != nil {
		return 0, err
	}

	events := make([]models.TaskJobEvent, 0, len(staleIDs))
	for _, id := range staleIDs {
		events = append(events, models.TaskJobEvent{
			JobID:     id,
			EventType: "expired",
			Message:   "执行窗口已过，自动失败",
			EventAt:   now,
		})
	}
	if len(events) > 0 {
		_ = g.db.Create(&events).Error
	}

	return int64(len(staleIDs)), nil
}
