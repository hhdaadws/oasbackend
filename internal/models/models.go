package models

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	UserStatusActive   = "active"
	UserStatusExpired  = "expired"
	UserStatusDisabled = "disabled"

	UserTypeDaily  = "daily"
	UserTypeDuiyi  = "duiyi"
	UserTypeShuaka = "shuaka"

	CodeStatusUnused  = "unused"
	CodeStatusUsed    = "used"
	CodeStatusRevoked = "revoked"

	JobStatusPending  = "pending"
	JobStatusLeased   = "leased"
	JobStatusRunning  = "running"
	JobStatusSuccess  = "success"
	JobStatusFailed   = "failed"
	JobStatusRequeued = "timeout_requeued"

	ActorTypeSuper   = "super"
	ActorTypeManager = "manager"
	ActorTypeUser    = "user"
	ActorTypeAgent   = "agent"
)

type SuperAdmin struct {
	ID           uint      `gorm:"primaryKey"`
	Username     string    `gorm:"size:64;not null;uniqueIndex"`
	PasswordHash string    `gorm:"size:255;not null"`
	CreatedAt    time.Time `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"not null"`
}

type Manager struct {
	ID           uint       `gorm:"primaryKey"`
	Username     string     `gorm:"size:64;not null;uniqueIndex"`
	PasswordHash string     `gorm:"size:255;not null"`
	Alias        string     `gorm:"size:64;not null;default:''"`
	ExpiresAt    *time.Time `gorm:"index"`
	CreatedAt    time.Time  `gorm:"not null"`
	UpdatedAt    time.Time  `gorm:"not null"`
}

type ManagerRenewalKey struct {
	ID                    uint   `gorm:"primaryKey"`
	Code                  string `gorm:"size:64;not null;uniqueIndex"`
	DurationDays          int    `gorm:"not null"`
	Status                string `gorm:"size:20;not null;default:unused;index"`
	UsedByManagerID       *uint  `gorm:"index"`
	UsedAt                *time.Time
	CreatedBySuperAdminID uint      `gorm:"not null;index"`
	CreatedAt             time.Time `gorm:"not null"`
}

type User struct {
	ID            uint              `gorm:"primaryKey"`
	AccountNo     string            `gorm:"size:64;not null;uniqueIndex"`
	ManagerID     uint              `gorm:"not null;index"`
	LoginID       string            `gorm:"size:64;not null;default:''"`
	UserType      string            `gorm:"size:20;not null;default:daily;index"`
	Status        string            `gorm:"size:20;not null;default:expired;index"`
	ArchiveStatus string            `gorm:"size:20;not null;default:normal"`
	Server        string            `gorm:"size:128;not null;default:''"`
	Username      string            `gorm:"size:128;not null;default:''"`
	ExpiresAt     *time.Time        `gorm:"index"`
	Assets          datatypes.JSONMap `gorm:"type:jsonb;not null;default:'{}'"`
	RestConfig      datatypes.JSONMap `gorm:"type:jsonb;not null;default:'{}'"`
	LineupConfig    datatypes.JSONMap `gorm:"type:jsonb;not null;default:'{}'"`
	ShikigamiConfig datatypes.JSONMap `gorm:"type:jsonb;not null;default:'{}'"`
	ExploreProgress datatypes.JSONMap `gorm:"type:jsonb;not null;default:'{}'"`
	NotifyConfig    datatypes.JSONMap `gorm:"type:jsonb;not null;default:'{}'"`
	CreatedBy       string            `gorm:"size:30;not null"`
	CreatedAt     time.Time         `gorm:"not null"`
	UpdatedAt     time.Time         `gorm:"not null"`
}

type UserToken struct {
	ID         uint      `gorm:"primaryKey"`
	UserID     uint      `gorm:"not null;index"`
	TokenHash  string    `gorm:"size:64;not null;uniqueIndex"`
	ExpiresAt  time.Time `gorm:"not null;index"`
	RevokedAt  *time.Time
	CreatedAt  time.Time `gorm:"not null"`
	LastUsedAt *time.Time
	DeviceInfo string `gorm:"size:255"`
}

type UserActivationCode struct {
	ID           uint   `gorm:"primaryKey"`
	ManagerID    uint   `gorm:"not null;index"`
	UserType     string `gorm:"size:20;not null;default:daily;index"`
	Code         string `gorm:"size:64;not null;uniqueIndex"`
	DurationDays int    `gorm:"not null"`
	Status       string `gorm:"size:20;not null;default:unused;index"`
	UsedByUserID *uint  `gorm:"index"`
	UsedAt       *time.Time
	CreatedAt    time.Time `gorm:"not null"`
}

type UserTaskConfig struct {
	ID         uint              `gorm:"primaryKey"`
	UserID     uint              `gorm:"not null;uniqueIndex"`
	TaskConfig datatypes.JSONMap `gorm:"type:jsonb;not null;default:'{}'"`
	UpdatedAt  time.Time         `gorm:"not null"`
	Version    int               `gorm:"not null;default:1"`
}

type TaskJob struct {
	ID           uint              `gorm:"primaryKey"`
	ManagerID    uint              `gorm:"not null;index:idx_task_jobs_manager_status_scheduled,priority:1"`
	UserID       uint              `gorm:"not null;index"`
	TaskType     string            `gorm:"size:64;not null"`
	Payload      datatypes.JSONMap `gorm:"type:jsonb;not null;default:'{}'"`
	Priority     int               `gorm:"not null;default:0"`
	ScheduledAt  time.Time         `gorm:"not null;index:idx_task_jobs_manager_status_scheduled,priority:3"`
	Status       string            `gorm:"size:24;not null;default:pending;index:idx_task_jobs_manager_status_scheduled,priority:2"`
	LeasedByNode string            `gorm:"size:128;index"`
	LeaseUntil   *time.Time        `gorm:"index"`
	Attempts     int               `gorm:"not null;default:0"`
	MaxAttempts  int               `gorm:"not null;default:3"`
	CreatedAt    time.Time         `gorm:"not null"`
	UpdatedAt    time.Time         `gorm:"not null"`
}

type TaskJobEvent struct {
	ID        uint      `gorm:"primaryKey"`
	JobID     uint      `gorm:"not null;index"`
	EventType string    `gorm:"size:24;not null;index"`
	Message   string    `gorm:"type:text"`
	ErrorCode string    `gorm:"size:64"`
	EventAt   time.Time `gorm:"not null;index"`
}

type AgentNode struct {
	ID            uint      `gorm:"primaryKey"`
	ManagerID     uint      `gorm:"not null;index"`
	NodeID        string    `gorm:"size:128;not null;uniqueIndex"`
	LastHeartbeat time.Time `gorm:"not null;index"`
	Status        string    `gorm:"size:20;not null;default:online"`
	Version       string    `gorm:"size:64"`
	CreatedAt     time.Time `gorm:"not null"`
	UpdatedAt     time.Time `gorm:"not null"`
}

type AuditLog struct {
	ID         uint              `gorm:"primaryKey"`
	ActorType  string            `gorm:"size:20;not null;index"`
	ActorID    uint              `gorm:"not null;index"`
	Action     string            `gorm:"size:64;not null;index"`
	TargetType string            `gorm:"size:40;not null"`
	TargetID   uint              `gorm:"not null"`
	Detail     datatypes.JSONMap `gorm:"type:jsonb;not null;default:'{}'"`
	IP         string            `gorm:"size:64"`
	CreatedAt  time.Time         `gorm:"not null;index"`
}

func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&SuperAdmin{},
		&Manager{},
		&ManagerRenewalKey{},
		&User{},
		&UserToken{},
		&UserActivationCode{},
		&UserTaskConfig{},
		&TaskJob{},
		&TaskJobEvent{},
		&AgentNode{},
		&AuditLog{},
	); err != nil {
		return err
	}
	return backfillLoginIDs(db)
}

// backfillLoginIDs assigns sequential login_id to existing users that have an
// empty value, then ensures the composite unique index (manager_id, login_id)
// exists.
func backfillLoginIDs(db *gorm.DB) error {
	var count int64
	if err := db.Model(&User{}).Where("login_id = ''").Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		var managerIDs []uint
		if err := db.Model(&User{}).Where("login_id = ''").Distinct("manager_id").Pluck("manager_id", &managerIDs).Error; err != nil {
			return err
		}
		for _, mid := range managerIDs {
			var maxVal int64
			row := db.Model(&User{}).
				Where("manager_id = ? AND login_id != ''", mid).
				Select("COALESCE(MAX(CAST(login_id AS INTEGER)), 0)").Row()
			if row != nil {
				_ = row.Scan(&maxVal)
			}
			var users []User
			if err := db.Where("manager_id = ? AND login_id = ''", mid).Order("id asc").Find(&users).Error; err != nil {
				return err
			}
			for i, u := range users {
				newID := strconv.FormatInt(maxVal+int64(i)+1, 10)
				if err := db.Model(&User{}).Where("id = ?", u.ID).Update("login_id", newID).Error; err != nil {
					return fmt.Errorf("backfill login_id for user %d: %w", u.ID, err)
				}
			}
		}
	}
	return db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_users_manager_login_id ON users(manager_id, login_id)").Error
}

func NormalizeUserType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case UserTypeDaily:
		return UserTypeDaily
	case UserTypeDuiyi:
		return UserTypeDuiyi
	case UserTypeShuaka:
		return UserTypeShuaka
	default:
		return UserTypeDaily
	}
}

func IsValidUserType(value string) bool {
	normalized := NormalizeUserType(value)
	raw := strings.ToLower(strings.TrimSpace(value))
	return raw == normalized
}
