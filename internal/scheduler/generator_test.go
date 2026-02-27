package scheduler

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"oas-cloud-go/internal/cache"
	"oas-cloud-go/internal/config"
	"oas-cloud-go/internal/models"
	"oas-cloud-go/internal/taskmeta"

	"github.com/glebarez/sqlite"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type schedulerStoreStub struct {
	cache.Store

	mu    sync.Mutex
	slots map[string]time.Time
}

func newSchedulerStoreStub() *schedulerStoreStub {
	return &schedulerStoreStub{
		slots: map[string]time.Time{},
	}
}

func (s *schedulerStoreStub) AcquireScheduleSlot(
	ctx context.Context,
	managerID uint,
	userID uint,
	taskType string,
	slot string,
	ttl time.Duration,
) (bool, error) {
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	key := fmt.Sprintf("%d:%d:%s:%s", managerID, userID, taskType, slot)
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	if exp, ok := s.slots[key]; ok && exp.After(now) {
		return false, nil
	}
	s.slots[key] = now.Add(ttl)
	return true, nil
}

func setupGeneratorTest(t *testing.T) (*Generator, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := models.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}

	cfg := config.Config{
		SchedulerSlotTTL: 90 * time.Second,
	}
	return NewGenerator(cfg, db, newSchedulerStoreStub()), db
}

func seedUserAndConfig(t *testing.T, db *gorm.DB, userType string, taskConfig map[string]any) (models.User, models.UserTaskConfig) {
	t.Helper()
	now := time.Now().UTC()
	expiresAt := now.Add(24 * time.Hour)

	user := models.User{
		AccountNo: fmt.Sprintf("U-%d", now.UnixNano()),
		ManagerID: 1,
		LoginID:   fmt.Sprintf("%d", now.UnixNano()),
		UserType:  userType,
		Status:    models.UserStatusActive,
		ExpiresAt: &expiresAt,
		Assets:    datatypes.JSONMap(taskmeta.BuildDefaultUserAssets()),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	cfg := models.UserTaskConfig{
		UserID:     user.ID,
		TaskConfig: datatypes.JSONMap(taskConfig),
		UpdatedAt:  now,
		Version:    1,
	}
	if err := db.Create(&cfg).Error; err != nil {
		t.Fatalf("create user task config failed: %v", err)
	}
	return user, cfg
}

func disableAllTasksExcept(taskConfig map[string]any, keepTask string) {
	for taskName, rawCfg := range taskConfig {
		taskMap, ok := rawCfg.(map[string]any)
		if !ok {
			continue
		}
		taskMap["enabled"] = taskName == keepTask
		taskConfig[taskName] = taskMap
	}
}

func countPendingJobs(t *testing.T, db *gorm.DB, userID uint, taskType string) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&models.TaskJob{}).
		Where("user_id = ? AND task_type = ? AND status = ?", userID, taskType, models.JobStatusPending).
		Count(&count).Error; err != nil {
		t.Fatalf("count task jobs failed: %v", err)
	}
	return count
}

func TestProcessUser_FangkaInvalidNextTime_GeneratesAcrossTaskPools(t *testing.T) {
	now := time.Date(2026, 2, 27, 10, 0, 0, 0, time.UTC)
	userTypes := []string{
		models.UserTypeDaily,
		models.UserTypeShuaka,
		models.UserTypeFoster,
		models.UserTypeJingzhi,
	}

	for _, userType := range userTypes {
		t.Run(userType, func(t *testing.T) {
			g, db := setupGeneratorTest(t)
			taskConfig := taskmeta.BuildDefaultTaskConfigByType(userType)
			disableAllTasksExcept(taskConfig, "放卡")
			fangkaCfg := taskConfig["放卡"].(map[string]any)
			fangkaCfg["next_time"] = "2026/02/27 09:00"
			taskConfig["放卡"] = fangkaCfg

			user, cfg := seedUserAndConfig(t, db, userType, taskConfig)
			generated, err := g.processUser(context.Background(), user, cfg, map[string]int64{}, nil, now)
			if err != nil {
				t.Fatalf("process user failed: %v", err)
			}
			if generated != 1 {
				t.Fatalf("expected generated=1, got %d", generated)
			}
			if jobs := countPendingJobs(t, db, user.ID, "放卡"); jobs != 1 {
				t.Fatalf("expected 1 pending 放卡 job, got %d", jobs)
			}
		})
	}
}

func TestProcessUser_FangkaSkipsWhenActiveJobExists(t *testing.T) {
	now := time.Date(2026, 2, 27, 10, 0, 0, 0, time.UTC)
	g, db := setupGeneratorTest(t)

	taskConfig := taskmeta.BuildDefaultTaskConfigByType(models.UserTypeFoster)
	disableAllTasksExcept(taskConfig, "放卡")
	fangkaCfg := taskConfig["放卡"].(map[string]any)
	fangkaCfg["next_time"] = "2026/02/27 09:00"
	taskConfig["放卡"] = fangkaCfg

	user, cfg := seedUserAndConfig(t, db, models.UserTypeFoster, taskConfig)
	firstGenerated, err := g.processUser(context.Background(), user, cfg, map[string]int64{}, nil, now)
	if err != nil {
		t.Fatalf("first process user failed: %v", err)
	}
	if firstGenerated != 1 {
		t.Fatalf("expected first generated=1, got %d", firstGenerated)
	}

	secondGenerated, err := g.processUser(context.Background(), user, cfg, map[string]int64{"放卡": 1}, nil, now)
	if err != nil {
		t.Fatalf("second process user failed: %v", err)
	}
	if secondGenerated != 0 {
		t.Fatalf("expected second generated=0, got %d", secondGenerated)
	}
	if jobs := countPendingJobs(t, db, user.ID, "放卡"); jobs != 1 {
		t.Fatalf("expected 1 pending 放卡 job after second run, got %d", jobs)
	}
}

func TestProcessUser_NonFangkaInvalidNextTime_NotScheduled(t *testing.T) {
	now := time.Date(2026, 2, 27, 10, 0, 0, 0, time.UTC)
	g, db := setupGeneratorTest(t)

	taskConfig := taskmeta.BuildDefaultTaskConfigByType(models.UserTypeFoster)
	disableAllTasksExcept(taskConfig, "寄养")
	fosterCfg := taskConfig["寄养"].(map[string]any)
	fosterCfg["next_time"] = "2026/02/27 09:00"
	taskConfig["寄养"] = fosterCfg

	user, cfg := seedUserAndConfig(t, db, models.UserTypeFoster, taskConfig)
	generated, err := g.processUser(context.Background(), user, cfg, map[string]int64{}, nil, now)
	if err != nil {
		t.Fatalf("process user failed: %v", err)
	}
	if generated != 0 {
		t.Fatalf("expected generated=0, got %d", generated)
	}
	if jobs := countPendingJobs(t, db, user.ID, "寄养"); jobs != 0 {
		t.Fatalf("expected 0 pending 寄养 jobs, got %d", jobs)
	}
}

func TestParseDateTime_SupportsISOWithoutTimezone(t *testing.T) {
	parsed := parseDateTime("2026-02-27T08:00")
	if parsed.IsZero() {
		t.Fatalf("expected parseDateTime to parse ISO datetime without timezone")
	}
	expected := time.Date(2026, 2, 27, 8, 0, 0, 0, taskmeta.BJLoc)
	if !parsed.Equal(expected) {
		t.Fatalf("unexpected parsed time: got %s want %s", parsed, expected)
	}
}
