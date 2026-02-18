package server

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"oas-cloud-go/internal/auth"
	"oas-cloud-go/internal/cache"
	"oas-cloud-go/internal/config"
	"oas-cloud-go/internal/models"
	"oas-cloud-go/internal/scheduler"
	"oas-cloud-go/internal/taskmeta"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//go:embed static/super_console.html
var staticFS embed.FS

type Server struct {
	cfg          config.Config
	db           *gorm.DB
	redisStore   cache.Store
	generator    *scheduler.Generator
	tokenManager *auth.TokenManager
	router       *gin.Engine
}

var errInvalidTaskConfigPatch = errors.New("invalid task config patch")

func New(cfg config.Config, db *gorm.DB, redisStore cache.Store) *Server {
	app := &Server{
		cfg:          cfg,
		db:           db,
		redisStore:   redisStore,
		tokenManager: auth.NewTokenManager(cfg.JWTSecret),
		router:       gin.New(),
	}
	if cfg.SchedulerEnabled {
		app.generator = scheduler.NewGenerator(cfg, db, redisStore)
		app.generator.Start()
	}
	app.router.Use(gin.Logger(), gin.Recovery())
	app.mountRoutes()
	return app
}

func (s *Server) Run() error {
	return s.router.Run(s.cfg.Addr)
}

func (s *Server) mountRoutes() {
	s.router.GET("/health", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		redisErr := s.redisStore.Ping(ctx)
		if redisErr != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "degraded", "redis": "down"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "redis": "up"})
	})
	s.router.GET("/super/console", s.superConsole)

	api := s.router.Group("/api/v1")
	{
		api.GET("/bootstrap/status", s.bootstrapStatus)
		api.GET("/scheduler/status", s.schedulerStatus)
		api.GET("/task-templates", s.taskTemplates)
		api.POST("/bootstrap/init", s.bootstrapInit)
		api.POST("/super/auth/login", s.superLogin)
		api.POST("/manager/auth/register", s.managerRegister)
		api.POST("/manager/auth/login", s.managerLogin)
		api.POST("/user/auth/register-by-code", s.userRegisterByCode)
		api.POST("/user/auth/login", s.userLogin)
		api.POST("/agent/auth/login", s.agentLogin)
	}

	superGroup := api.Group("/super")
	superGroup.Use(s.requireJWT(models.ActorTypeSuper))
	{
		superGroup.POST("/manager-renewal-keys", s.superCreateManagerRenewalKey)
		superGroup.GET("/manager-renewal-keys", s.superListManagerRenewalKeys)
		superGroup.PATCH("/manager-renewal-keys/:id/status", s.superPatchManagerRenewalKeyStatus)
		superGroup.GET("/managers", s.superListManagers)
		superGroup.PATCH("/managers/:id/status", s.superPatchManagerStatus)
		superGroup.PATCH("/managers/:id/lifecycle", s.superPatchManagerLifecycle)
	}

	managerAuthGroup := api.Group("/manager")
	managerAuthGroup.Use(s.requireJWT(models.ActorTypeManager))
	{
		managerAuthGroup.POST("/auth/redeem-renewal-key", s.managerRedeemRenewalKey)
	}

	managerGroup := api.Group("/manager")
	managerGroup.Use(s.requireJWT(models.ActorTypeManager), s.requireManagerActive())
	{
		managerGroup.GET("/overview", s.managerOverview)
		managerGroup.POST("/activation-codes", s.managerCreateActivationCode)
		managerGroup.GET("/activation-codes", s.managerListActivationCodes)
		managerGroup.PATCH("/activation-codes/:id/status", s.managerPatchActivationCodeStatus)
		managerGroup.POST("/users/quick-create", s.managerQuickCreateUser)
		managerGroup.GET("/users", s.managerListUsers)
		managerGroup.PATCH("/users/:user_id/lifecycle", s.managerPatchUserLifecycle)
		managerGroup.GET("/users/:user_id/assets", s.managerGetUserAssets)
		managerGroup.PUT("/users/:user_id/assets", s.managerPutUserAssets)
		managerGroup.GET("/users/:user_id/tasks", s.managerGetUserTasks)
		managerGroup.PUT("/users/:user_id/tasks", s.managerPutUserTasks)
		managerGroup.GET("/users/:user_id/logs", s.managerGetUserLogs)
	}

	userGroup := api.Group("/user")
	userGroup.Use(s.requireUserToken())
	{
		userGroup.POST("/auth/logout", s.userLogout)
		userGroup.POST("/auth/redeem-code", s.userRedeemCode)
		userGroup.GET("/me/profile", s.userGetMeProfile)
		userGroup.GET("/me/assets", s.userGetMeAssets)
		userGroup.GET("/me/tasks", s.userGetMeTasks)
		userGroup.PUT("/me/tasks", s.userPutMeTasks)
		userGroup.GET("/me/logs", s.userGetMeLogs)
	}

	agentGroup := api.Group("/agent")
	agentGroup.Use(s.requireJWT(models.ActorTypeAgent))
	{
		agentGroup.POST("/poll-jobs", s.agentPollJobs)
		agentGroup.POST("/jobs/:job_id/start", s.agentJobStart)
		agentGroup.POST("/jobs/:job_id/heartbeat", s.agentJobHeartbeat)
		agentGroup.POST("/jobs/:job_id/complete", s.agentJobComplete)
		agentGroup.POST("/jobs/:job_id/fail", s.agentJobFail)
	}

	s.mountFrontendRoutes()
}

func (s *Server) schedulerStatus(c *gin.Context) {
	if s.generator == nil {
		c.JSON(http.StatusOK, gin.H{
			"enabled": false,
			"status":  "disabled",
		})
		return
	}
	snapshot := s.generator.Snapshot()
	c.JSON(http.StatusOK, gin.H{
		"enabled": true,
		"status":  snapshot,
	})
}

func (s *Server) taskTemplates(c *gin.Context) {
	rawType := strings.TrimSpace(c.Query("user_type"))
	if rawType != "" && !models.IsValidUserType(rawType) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid user_type"})
		return
	}
	userType := models.NormalizeUserType(rawType)
	c.JSON(http.StatusOK, gin.H{
		"user_type":            userType,
		"supported_user_types": taskmeta.UserTypes(),
		"task_pools":           taskmeta.TaskPools(),
		"order":                taskmeta.UserTypeTaskOrder(userType),
		"default_config":       taskmeta.BuildDefaultTaskConfigByType(userType),
		"items":                taskmeta.BuildTaskTemplateListByType(userType),
	})
}

func (s *Server) superConsole(c *gin.Context) {
	content, err := staticFS.ReadFile("static/super_console.html")
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load page")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", content)
}

func (s *Server) mountFrontendRoutes() {
	if !s.cfg.ServeFrontend {
		return
	}
	distDir := strings.TrimSpace(s.cfg.FrontendDistDir)
	if distDir == "" {
		return
	}
	indexPath := filepath.Join(distDir, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		return
	}

	assetsPath := filepath.Join(distDir, "assets")
	if stat, err := os.Stat(assetsPath); err == nil && stat.IsDir() {
		s.router.Static("/assets", assetsPath)
	}
	s.router.GET("/", func(c *gin.Context) {
		c.File(indexPath)
	})

	s.router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api/") || path == "/health" || strings.HasPrefix(path, "/super/") {
			c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
			return
		}
		c.File(indexPath)
	})
}

func (s *Server) bootstrapStatus(c *gin.Context) {
	var count int64
	s.db.Model(&models.SuperAdmin{}).Count(&count)
	c.JSON(http.StatusOK, gin.H{
		"initialized": count > 0,
	})
}

func (s *Server) bootstrapInit(c *gin.Context) {
	var req bootstrapInitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	var count int64
	s.db.Model(&models.SuperAdmin{}).Count(&count)
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{"detail": "super admin already initialized"})
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to hash password"})
		return
	}

	now := time.Now().UTC()
	admin := models.SuperAdmin{
		Username:     req.Username,
		PasswordHash: hash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.db.Create(&admin).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to create super admin"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "super admin initialized"})
}

func (s *Server) superLogin(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	var admin models.SuperAdmin
	if err := s.db.Where("username = ?", req.Username).First(&admin).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "invalid credentials"})
		return
	}
	if !auth.VerifyPassword(req.Password, admin.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "invalid credentials"})
		return
	}
	token, err := s.tokenManager.IssueJWT(models.ActorTypeSuper, admin.ID, 0, s.cfg.JWTTTL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to issue token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "role": models.ActorTypeSuper})
}

func (s *Server) managerRegister(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to hash password"})
		return
	}
	now := time.Now().UTC()
	manager := models.Manager{
		Username:     req.Username,
		PasswordHash: hash,
		Status:       models.ManagerStatusExpired,
		ExpiresAt:    &now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.db.Create(&manager).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"detail": "username already exists"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "manager registered, redeem renewal key to activate"})
}

func (s *Server) managerLogin(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	var manager models.Manager
	if err := s.db.Where("username = ?", req.Username).First(&manager).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "invalid credentials"})
		return
	}
	if !auth.VerifyPassword(req.Password, manager.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "invalid credentials"})
		return
	}
	now := time.Now().UTC()
	if manager.Status == models.ManagerStatusDisabled {
		c.JSON(http.StatusForbidden, gin.H{"detail": "manager disabled"})
		return
	}
	token, err := s.tokenManager.IssueJWT(models.ActorTypeManager, manager.ID, manager.ID, s.cfg.JWTTTL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to issue token"})
		return
	}
	expired := manager.ExpiresAt == nil || !manager.ExpiresAt.After(now) || manager.Status != models.ManagerStatusActive
	c.JSON(http.StatusOK, gin.H{
		"token":      token,
		"role":       models.ActorTypeManager,
		"manager_id": manager.ID,
		"status":     manager.Status,
		"expires_at": manager.ExpiresAt,
		"expired":    expired,
	})
}

func (s *Server) superCreateManagerRenewalKey(c *gin.Context) {
	var req createRenewalKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	code, err := auth.GenerateOpaqueToken("mrk", 12)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to generate key"})
		return
	}
	actorID := getUint(c, ctxActorIDKey)
	now := time.Now().UTC()
	key := models.ManagerRenewalKey{
		Code:                  code,
		DurationDays:          req.DurationDays,
		Status:                models.CodeStatusUnused,
		CreatedBySuperAdminID: actorID,
		CreatedAt:             now,
	}
	if err := s.db.Create(&key).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to save key"})
		return
	}
	s.audit(models.ActorTypeSuper, actorID, "create_manager_renewal_key", "manager_renewal_key", key.ID, datatypes.JSONMap{"duration_days": req.DurationDays}, c.ClientIP())
	c.JSON(http.StatusCreated, gin.H{"code": key.Code, "duration_days": key.DurationDays})
}

func (s *Server) superListManagerRenewalKeys(c *gin.Context) {
	status := strings.TrimSpace(c.Query("status"))
	keyword := strings.TrimSpace(c.Query("keyword"))
	limit := readQueryInt(c, "limit", 200, 1, 2000)
	if status != "" && !isCodeStatus(status) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid status"})
		return
	}

	query := s.db.Model(&models.ManagerRenewalKey{}).Order("id desc").Limit(limit)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if keyword != "" {
		query = query.Where("code LIKE ?", "%"+keyword+"%")
	}

	var keys []models.ManagerRenewalKey
	if err := query.Find(&keys).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to query renewal keys"})
		return
	}

	managerIDs := make([]uint, 0, len(keys))
	managerIDSet := map[uint]struct{}{}
	for _, key := range keys {
		if key.UsedByManagerID == nil {
			continue
		}
		if _, exists := managerIDSet[*key.UsedByManagerID]; exists {
			continue
		}
		managerIDSet[*key.UsedByManagerID] = struct{}{}
		managerIDs = append(managerIDs, *key.UsedByManagerID)
	}

	managerNameMap := map[uint]string{}
	if len(managerIDs) > 0 {
		var managers []models.Manager
		if err := s.db.Where("id IN ?", managerIDs).Find(&managers).Error; err == nil {
			for _, manager := range managers {
				managerNameMap[manager.ID] = manager.Username
			}
		}
	}

	items := make([]gin.H, 0, len(keys))
	var totalCount int64
	var unusedCount int64
	var usedCount int64
	var revokedCount int64
	_ = s.db.Model(&models.ManagerRenewalKey{}).Count(&totalCount).Error
	_ = s.db.Model(&models.ManagerRenewalKey{}).Where("status = ?", models.CodeStatusUnused).Count(&unusedCount).Error
	_ = s.db.Model(&models.ManagerRenewalKey{}).Where("status = ?", models.CodeStatusUsed).Count(&usedCount).Error
	_ = s.db.Model(&models.ManagerRenewalKey{}).Where("status = ?", models.CodeStatusRevoked).Count(&revokedCount).Error
	for _, key := range keys {
		var usedByManagerID any
		var usedByManagerUsername any
		if key.UsedByManagerID != nil {
			usedByManagerID = *key.UsedByManagerID
			usedByManagerUsername = managerNameMap[*key.UsedByManagerID]
		}
		items = append(items, gin.H{
			"id":                        key.ID,
			"code":                      key.Code,
			"duration_days":             key.DurationDays,
			"status":                    key.Status,
			"used_by_manager_id":        usedByManagerID,
			"used_by_manager_username":  usedByManagerUsername,
			"used_at":                   key.UsedAt,
			"created_by_super_admin_id": key.CreatedBySuperAdminID,
			"created_at":                key.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"summary": gin.H{
			"total":   totalCount,
			"unused":  unusedCount,
			"used":    usedCount,
			"revoked": revokedCount,
		},
	})
}

func (s *Server) superListManagers(c *gin.Context) {
	status := strings.TrimSpace(c.Query("status"))
	keyword := strings.TrimSpace(c.Query("keyword"))
	limit := readQueryInt(c, "limit", 500, 1, 2000)
	if status != "" && !isManagerStatus(status) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid status"})
		return
	}

	query := s.db.Model(&models.Manager{}).Order("id desc").Limit(limit)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if keyword != "" {
		query = query.Where("username LIKE ?", "%"+keyword+"%")
	}

	var managers []models.Manager
	if err := query.Find(&managers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to query managers"})
		return
	}

	now := time.Now().UTC()
	expiringThreshold := now.Add(7 * 24 * time.Hour)
	summary := gin.H{
		"total":       len(managers),
		"active":      0,
		"expired":     0,
		"disabled":    0,
		"expiring_7d": 0,
	}

	items := make([]gin.H, 0, len(managers))
	for _, manager := range managers {
		expiresAt := manager.ExpiresAt
		isExpired := expiresAt == nil || !expiresAt.After(now)
		if manager.Status == models.ManagerStatusActive {
			summary["active"] = summary["active"].(int) + 1
		}
		if manager.Status == models.ManagerStatusExpired {
			summary["expired"] = summary["expired"].(int) + 1
		}
		if manager.Status == models.ManagerStatusDisabled {
			summary["disabled"] = summary["disabled"].(int) + 1
		}
		if manager.Status == models.ManagerStatusActive && expiresAt != nil && expiresAt.After(now) && expiresAt.Before(expiringThreshold) {
			summary["expiring_7d"] = summary["expiring_7d"].(int) + 1
		}
		items = append(items, gin.H{
			"id":         manager.ID,
			"username":   manager.Username,
			"status":     manager.Status,
			"expires_at": manager.ExpiresAt,
			"is_expired": isExpired,
			"created_at": manager.CreatedAt,
			"updated_at": manager.UpdatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "summary": summary})
}

func (s *Server) superPatchManagerRenewalKeyStatus(c *gin.Context) {
	var req patchCodeStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	keyID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}

	var key models.ManagerRenewalKey
	if err := s.db.Where("id = ?", keyID).First(&key).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"detail": "renewal key not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to query renewal key"})
		return
	}
	if key.Status == models.CodeStatusUsed {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "used renewal key cannot be revoked"})
		return
	}
	if key.Status == models.CodeStatusRevoked {
		c.JSON(http.StatusOK, gin.H{"message": "renewal key already revoked"})
		return
	}

	if err := s.db.Model(&models.ManagerRenewalKey{}).Where("id = ?", keyID).Updates(map[string]any{
		"status": models.CodeStatusRevoked,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to revoke renewal key"})
		return
	}
	actorID := getUint(c, ctxActorIDKey)
	s.audit(models.ActorTypeSuper, actorID, "patch_manager_renewal_key_status", "manager_renewal_key", keyID, datatypes.JSONMap{
		"status": req.Status,
	}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"message": "renewal key revoked"})
}

func (s *Server) superPatchManagerStatus(c *gin.Context) {
	var req patchManagerStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid manager id"})
		return
	}

	updates := map[string]any{"status": req.Status, "updated_at": time.Now().UTC()}
	if req.Status == models.ManagerStatusExpired {
		now := time.Now().UTC()
		updates["expires_at"] = now
	}
	result := s.db.Model(&models.Manager{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to patch manager"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"detail": "manager not found"})
		return
	}
	actorID := getUint(c, ctxActorIDKey)
	s.audit(models.ActorTypeSuper, actorID, "patch_manager_status", "manager", uint(id), datatypes.JSONMap{"status": req.Status}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"message": "manager status updated"})
}

func (s *Server) superPatchManagerLifecycle(c *gin.Context) {
	managerID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}

	var req superPatchManagerLifecycleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	if strings.TrimSpace(req.ExpiresAt) == "" && req.ExtendDays == 0 && strings.TrimSpace(req.Status) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "at least one field must be provided"})
		return
	}

	now := time.Now().UTC()
	var manager models.Manager
	if err := s.db.Where("id = ?", managerID).First(&manager).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"detail": "manager not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to query manager"})
		return
	}

	updates := map[string]any{"updated_at": now}
	hasExpiryUpdate := false
	if rawExpires := strings.TrimSpace(req.ExpiresAt); rawExpires != "" {
		parsed, err := parseFlexibleDateTime(rawExpires)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid expires_at format"})
			return
		}
		updates["expires_at"] = parsed
		hasExpiryUpdate = true
	}
	if req.ExtendDays != 0 {
		if req.ExtendDays < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "extend_days must be positive"})
			return
		}
		newExpire := extendExpiry(manager.ExpiresAt, req.ExtendDays, now)
		updates["expires_at"] = newExpire
		hasExpiryUpdate = true
	}

	if rawStatus := strings.TrimSpace(req.Status); rawStatus != "" {
		if !isManagerStatus(rawStatus) {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid status"})
			return
		}
		updates["status"] = rawStatus
	} else if hasExpiryUpdate {
		expireValue, has := updates["expires_at"]
		if has {
			expireTime, ok := expireValue.(time.Time)
			if ok {
				if expireTime.After(now) {
					updates["status"] = models.ManagerStatusActive
				} else {
					updates["status"] = models.ManagerStatusExpired
				}
			}
		}
	}

	if err := s.db.Model(&models.Manager{}).Where("id = ?", managerID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to patch manager lifecycle"})
		return
	}
	actorID := getUint(c, ctxActorIDKey)
	s.audit(models.ActorTypeSuper, actorID, "patch_manager_lifecycle", "manager", managerID, datatypes.JSONMap{
		"expires_at":  req.ExpiresAt,
		"extend_days": req.ExtendDays,
		"status":      req.Status,
	}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"message": "manager lifecycle updated"})
}

func (s *Server) managerRedeemRenewalKey(c *gin.Context) {
	var req managerRedeemKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	managerID := getUint(c, ctxActorIDKey)
	now := time.Now().UTC()

	err := s.db.Transaction(func(tx *gorm.DB) error {
		var key models.ManagerRenewalKey
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code = ?", req.Code).First(&key).Error; err != nil {
			return err
		}
		if key.Status != models.CodeStatusUnused {
			return fmt.Errorf("renewal key already consumed")
		}

		var manager models.Manager
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", managerID).First(&manager).Error; err != nil {
			return err
		}
		newExpire := extendExpiry(manager.ExpiresAt, key.DurationDays, now)
		if err := tx.Model(&models.Manager{}).Where("id = ?", managerID).Updates(map[string]any{
			"expires_at": newExpire,
			"status":     models.ManagerStatusActive,
			"updated_at": now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.ManagerRenewalKey{}).Where("id = ?", key.ID).Updates(map[string]any{
			"status":             models.CodeStatusUsed,
			"used_by_manager_id": managerID,
			"used_at":            now,
		}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"detail": "renewal key not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	s.audit(models.ActorTypeManager, managerID, "redeem_manager_renewal_key", "manager", managerID, datatypes.JSONMap{"code": req.Code}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"message": "renewal success"})
}

func (s *Server) managerCreateActivationCode(c *gin.Context) {
	var req createActivationCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	managerID := getUint(c, ctxActorIDKey)
	userType := models.NormalizeUserType(req.UserType)
	code, err := auth.GenerateOpaqueToken("uac", 12)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to generate activation code"})
		return
	}
	now := time.Now().UTC()
	activation := models.UserActivationCode{
		ManagerID:    managerID,
		UserType:     userType,
		Code:         code,
		DurationDays: req.DurationDays,
		Status:       models.CodeStatusUnused,
		CreatedAt:    now,
	}
	if err := s.db.Create(&activation).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to create activation code"})
		return
	}
	s.audit(models.ActorTypeManager, managerID, "create_activation_code", "user_activation_code", activation.ID, datatypes.JSONMap{
		"duration_days": req.DurationDays,
		"user_type":     activation.UserType,
	}, c.ClientIP())
	c.JSON(http.StatusCreated, gin.H{
		"code":          activation.Code,
		"duration_days": activation.DurationDays,
		"user_type":     activation.UserType,
	})
}

func (s *Server) managerListActivationCodes(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	status := strings.TrimSpace(c.Query("status"))
	userType := strings.TrimSpace(c.Query("user_type"))
	keyword := strings.TrimSpace(c.Query("keyword"))
	limit := readQueryInt(c, "limit", 200, 1, 2000)

	if status != "" && !isCodeStatus(status) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid status"})
		return
	}
	if userType != "" && !models.IsValidUserType(userType) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid user_type"})
		return
	}

	query := s.db.Model(&models.UserActivationCode{}).
		Where("manager_id = ?", managerID).
		Order("id desc").
		Limit(limit)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if userType != "" {
		query = query.Where("user_type = ?", models.NormalizeUserType(userType))
	}
	if keyword != "" {
		query = query.Where("code LIKE ?", "%"+keyword+"%")
	}

	var codes []models.UserActivationCode
	if err := query.Find(&codes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to query activation codes"})
		return
	}

	userIDs := make([]uint, 0, len(codes))
	seen := map[uint]struct{}{}
	for _, code := range codes {
		if code.UsedByUserID == nil {
			continue
		}
		if _, ok := seen[*code.UsedByUserID]; ok {
			continue
		}
		seen[*code.UsedByUserID] = struct{}{}
		userIDs = append(userIDs, *code.UsedByUserID)
	}
	accountMap := map[uint]string{}
	if len(userIDs) > 0 {
		var users []models.User
		if err := s.db.Where("id IN ? AND manager_id = ?", userIDs, managerID).Find(&users).Error; err == nil {
			for _, user := range users {
				accountMap[user.ID] = user.AccountNo
			}
		}
	}

	var totalCount int64
	var unusedCount int64
	var usedCount int64
	var revokedCount int64
	_ = s.db.Model(&models.UserActivationCode{}).Where("manager_id = ?", managerID).Count(&totalCount).Error
	_ = s.db.Model(&models.UserActivationCode{}).Where("manager_id = ? AND status = ?", managerID, models.CodeStatusUnused).Count(&unusedCount).Error
	_ = s.db.Model(&models.UserActivationCode{}).Where("manager_id = ? AND status = ?", managerID, models.CodeStatusUsed).Count(&usedCount).Error
	_ = s.db.Model(&models.UserActivationCode{}).Where("manager_id = ? AND status = ?", managerID, models.CodeStatusRevoked).Count(&revokedCount).Error

	items := make([]gin.H, 0, len(codes))
	for _, code := range codes {
		var usedByUserID any
		var usedByAccountNo any
		if code.UsedByUserID != nil {
			usedByUserID = *code.UsedByUserID
			usedByAccountNo = accountMap[*code.UsedByUserID]
		}
		items = append(items, gin.H{
			"id":                 code.ID,
			"code":               code.Code,
			"user_type":          models.NormalizeUserType(code.UserType),
			"duration_days":      code.DurationDays,
			"status":             code.Status,
			"used_by_user_id":    usedByUserID,
			"used_by_account_no": usedByAccountNo,
			"used_at":            code.UsedAt,
			"created_at":         code.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"summary": gin.H{
			"total":   totalCount,
			"unused":  unusedCount,
			"used":    usedCount,
			"revoked": revokedCount,
		},
	})
}

func (s *Server) managerPatchActivationCodeStatus(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	codeID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}

	var req patchCodeStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	var code models.UserActivationCode
	if err := s.db.Where("id = ? AND manager_id = ?", codeID, managerID).First(&code).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"detail": "activation code not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to query activation code"})
		return
	}
	if code.Status == models.CodeStatusUsed {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "used activation code cannot be revoked"})
		return
	}
	if code.Status == models.CodeStatusRevoked {
		c.JSON(http.StatusOK, gin.H{"message": "activation code already revoked"})
		return
	}
	if err := s.db.Model(&models.UserActivationCode{}).Where("id = ?", codeID).Updates(map[string]any{
		"status": models.CodeStatusRevoked,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to revoke activation code"})
		return
	}
	s.audit(models.ActorTypeManager, managerID, "patch_activation_code_status", "user_activation_code", codeID, datatypes.JSONMap{
		"status": req.Status,
	}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"message": "activation code revoked"})
}

func (s *Server) managerQuickCreateUser(c *gin.Context) {
	var req quickCreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	managerID := getUint(c, ctxActorIDKey)
	userType := models.NormalizeUserType(req.UserType)
	now := time.Now().UTC()

	var createdUser models.User
	err := s.db.Transaction(func(tx *gorm.DB) error {
		code, err := auth.GenerateOpaqueToken("uac", 12)
		if err != nil {
			return err
		}
		activation := models.UserActivationCode{
			ManagerID:    managerID,
			UserType:     userType,
			Code:         code,
			DurationDays: req.DurationDays,
			Status:       models.CodeStatusUnused,
			CreatedAt:    now,
		}
		if err := tx.Create(&activation).Error; err != nil {
			return err
		}
		user, err := s.createUserByActivationCode(tx, &activation, "manager_create", now)
		if err != nil {
			return err
		}
		createdUser = *user
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to quick create user"})
		return
	}
	s.audit(models.ActorTypeManager, managerID, "quick_create_user", "user", createdUser.ID, datatypes.JSONMap{
		"duration_days": req.DurationDays,
		"user_type":     createdUser.UserType,
	}, c.ClientIP())
	c.JSON(http.StatusCreated, gin.H{
		"account_no": createdUser.AccountNo,
		"user_id":    createdUser.ID,
		"user_type":  models.NormalizeUserType(createdUser.UserType),
		"expires_at": createdUser.ExpiresAt,
	})
}

func (s *Server) managerListUsers(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	status := strings.TrimSpace(c.Query("status"))
	userType := strings.TrimSpace(c.Query("user_type"))
	keyword := strings.TrimSpace(c.Query("keyword"))
	limit := readQueryInt(c, "limit", 800, 1, 5000)
	if status != "" && !isUserStatus(status) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid status"})
		return
	}
	if userType != "" && !models.IsValidUserType(userType) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid user_type"})
		return
	}

	query := s.db.Where("manager_id = ?", managerID).Order("id asc").Limit(limit)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if userType != "" {
		query = query.Where("user_type = ?", models.NormalizeUserType(userType))
	}
	if keyword != "" {
		query = query.Where("account_no LIKE ?", "%"+keyword+"%")
	}
	var users []models.User
	if err := query.Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to query users"})
		return
	}
	summary := gin.H{
		"total":    len(users),
		"active":   0,
		"expired":  0,
		"disabled": 0,
		"daily":    0,
		"duiyi":    0,
		"shuaka":   0,
	}
	for index := range users {
		users[index].UserType = models.NormalizeUserType(users[index].UserType)
		switch users[index].Status {
		case models.UserStatusActive:
			summary["active"] = summary["active"].(int) + 1
		case models.UserStatusExpired:
			summary["expired"] = summary["expired"].(int) + 1
		case models.UserStatusDisabled:
			summary["disabled"] = summary["disabled"].(int) + 1
		}
		switch users[index].UserType {
		case models.UserTypeDaily:
			summary["daily"] = summary["daily"].(int) + 1
		case models.UserTypeDuiyi:
			summary["duiyi"] = summary["duiyi"].(int) + 1
		case models.UserTypeShuaka:
			summary["shuaka"] = summary["shuaka"].(int) + 1
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": users, "summary": summary})
}

func (s *Server) managerOverview(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	now := time.Now().UTC()

	userStats := gin.H{
		"total":    int64(0),
		"active":   int64(0),
		"expired":  int64(0),
		"disabled": int64(0),
	}
	jobStats := gin.H{
		"pending": int64(0),
		"leased":  int64(0),
		"running": int64(0),
		"success": int64(0),
		"failed":  int64(0),
	}

	userStatusTargets := []string{models.UserStatusActive, models.UserStatusExpired, models.UserStatusDisabled}
	var totalUsers int64
	for _, status := range userStatusTargets {
		var count int64
		if err := s.db.Model(&models.User{}).Where("manager_id = ? AND status = ?", managerID, status).Count(&count).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to query user overview"})
			return
		}
		userStats[status] = count
		totalUsers += count
	}
	userStats["total"] = totalUsers

	jobStatusTargets := []string{models.JobStatusPending, models.JobStatusLeased, models.JobStatusRunning, models.JobStatusSuccess, models.JobStatusFailed}
	for _, status := range jobStatusTargets {
		var count int64
		if err := s.db.Model(&models.TaskJob{}).Where("manager_id = ? AND status = ?", managerID, status).Count(&count).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to query job overview"})
			return
		}
		jobStats[status] = count
	}

	var recentFailures int64
	since := now.Add(-24 * time.Hour)
	if err := s.db.Model(&models.TaskJobEvent{}).
		Joins("JOIN task_jobs ON task_jobs.id = task_job_events.job_id").
		Where("task_jobs.manager_id = ? AND task_job_events.event_type = ? AND task_job_events.event_at >= ?", managerID, "fail", since).
		Count(&recentFailures).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to query recent failures"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user_stats":          userStats,
		"job_stats":           jobStats,
		"recent_failures_24h": recentFailures,
		"generated_at":        now,
	})
}

func (s *Server) managerGetUserTasks(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	userID, ok := parseUintParam(c, "user_id")
	if !ok {
		return
	}
	if !s.managerOwnsUser(c, managerID, userID) {
		return
	}
	var user models.User
	if err := s.db.Where("id = ? AND manager_id = ?", userID, managerID).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "user not found"})
		return
	}
	config, err := s.getOrCreateTaskConfig(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load task config"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"user_id":     userID,
		"user_type":   models.NormalizeUserType(user.UserType),
		"task_config": config.TaskConfig,
		"version":     config.Version,
	})
}

func (s *Server) managerPatchUserLifecycle(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	userID, ok := parseUintParam(c, "user_id")
	if !ok {
		return
	}
	if !s.managerOwnsUser(c, managerID, userID) {
		return
	}

	var req managerPatchUserLifecycleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	if strings.TrimSpace(req.ExpiresAt) == "" && req.ExtendDays == 0 && strings.TrimSpace(req.Status) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "at least one field must be provided"})
		return
	}

	now := time.Now().UTC()
	var user models.User
	if err := s.db.Where("id = ? AND manager_id = ?", userID, managerID).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "user not found"})
		return
	}

	updates := map[string]any{"updated_at": now}
	hasExpiryUpdate := false
	if strings.TrimSpace(req.ExpiresAt) != "" {
		parsed, err := parseFlexibleDateTime(req.ExpiresAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid expires_at format"})
			return
		}
		updates["expires_at"] = parsed
		hasExpiryUpdate = true
	}
	if req.ExtendDays != 0 {
		if req.ExtendDays < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "extend_days must be positive"})
			return
		}
		newExpire := extendExpiry(user.ExpiresAt, req.ExtendDays, now)
		updates["expires_at"] = newExpire
		hasExpiryUpdate = true
	}

	if status := strings.TrimSpace(req.Status); status != "" {
		if status != models.UserStatusActive && status != models.UserStatusExpired && status != models.UserStatusDisabled {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid status"})
			return
		}
		updates["status"] = status
	} else if hasExpiryUpdate {
		expireValue, has := updates["expires_at"]
		if has {
			if expireTime, ok := expireValue.(time.Time); ok {
				if expireTime.After(now) {
					updates["status"] = models.UserStatusActive
				} else {
					updates["status"] = models.UserStatusExpired
				}
			}
		}
	}

	if err := s.db.Model(&models.User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to update user lifecycle"})
		return
	}
	s.audit(models.ActorTypeManager, managerID, "patch_user_lifecycle", "user", userID, datatypes.JSONMap{
		"expires_at":  req.ExpiresAt,
		"extend_days": req.ExtendDays,
		"status":      req.Status,
	}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"message": "user lifecycle updated"})
}

func (s *Server) managerGetUserAssets(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	userID, ok := parseUintParam(c, "user_id")
	if !ok {
		return
	}
	if !s.managerOwnsUser(c, managerID, userID) {
		return
	}

	var user models.User
	if err := s.db.Where("id = ?", userID).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "user not found"})
		return
	}
	assets := deepMergeMap(taskmeta.BuildDefaultUserAssets(), map[string]any(user.Assets))
	c.JSON(http.StatusOK, gin.H{
		"user_id":    userID,
		"user_type":  models.NormalizeUserType(user.UserType),
		"assets":     assets,
		"expires_at": user.ExpiresAt,
		"status":     user.Status,
	})
}

func (s *Server) managerPutUserAssets(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	userID, ok := parseUintParam(c, "user_id")
	if !ok {
		return
	}
	if !s.managerOwnsUser(c, managerID, userID) {
		return
	}
	var req putUserAssetsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	base := taskmeta.BuildDefaultUserAssets()
	for key, value := range req.Assets {
		if err := taskmeta.ValidateAssetKey(key); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
			return
		}
		base[key] = taskmeta.ParseAssetInt(value, taskmeta.ParseAssetInt(base[key], 0))
	}

	if err := s.db.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"assets":     datatypes.JSONMap(base),
		"updated_at": time.Now().UTC(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to update user assets"})
		return
	}
	s.audit(models.ActorTypeManager, managerID, "update_user_assets", "user", userID, datatypes.JSONMap{"assets": req.Assets}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"message": "user assets updated", "assets": base})
}

func (s *Server) managerPutUserTasks(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	userID, ok := parseUintParam(c, "user_id")
	if !ok {
		return
	}
	if !s.managerOwnsUser(c, managerID, userID) {
		return
	}
	var req putTaskConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	updated, err := s.mergeTaskConfig(userID, req.TaskConfig)
	if err != nil {
		if errors.Is(err, errInvalidTaskConfigPatch) {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "task_config contains tasks not allowed for this user type"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to update task config"})
		return
	}
	var user models.User
	if err := s.db.Where("id = ? AND manager_id = ?", userID, managerID).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "user not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"user_id":     userID,
		"user_type":   models.NormalizeUserType(user.UserType),
		"task_config": updated.TaskConfig,
		"version":     updated.Version,
	})
}

func (s *Server) managerGetUserLogs(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	userID, ok := parseUintParam(c, "user_id")
	if !ok {
		return
	}
	if !s.managerOwnsUser(c, managerID, userID) {
		return
	}
	limit := readQueryInt(c, "limit", 50, 1, 200)
	items, err := s.queryUserLogs(managerID, userID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to query logs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (s *Server) userRegisterByCode(c *gin.Context) {
	var req userRegisterByCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	now := time.Now().UTC()
	var createdUser models.User
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var code models.UserActivationCode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code = ?", req.Code).First(&code).Error; err != nil {
			return err
		}
		if code.Status != models.CodeStatusUnused {
			return fmt.Errorf("activation code already consumed")
		}
		user, err := s.createUserByActivationCode(tx, &code, "self_register", now)
		if err != nil {
			return err
		}
		createdUser = *user
		return nil
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"detail": "activation code not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	rawToken, tokenExpire, err := s.issueUserToken(createdUser.ID, "register")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to issue user token"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"account_no": createdUser.AccountNo,
		"user_type":  models.NormalizeUserType(createdUser.UserType),
		"token":      rawToken,
		"expires_at": createdUser.ExpiresAt,
		"token_exp":  tokenExpire,
	})
}

func (s *Server) userLogin(c *gin.Context) {
	var req userLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	var user models.User
	if err := s.db.Where("account_no = ?", req.AccountNo).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "invalid account"})
		return
	}
	now := time.Now().UTC()
	if user.Status != models.UserStatusActive {
		c.JSON(http.StatusForbidden, gin.H{"detail": "user not active"})
		return
	}
	if user.ExpiresAt == nil || !user.ExpiresAt.After(now) {
		c.JSON(http.StatusForbidden, gin.H{"detail": "user expired"})
		return
	}
	rawToken, tokenExpire, err := s.issueUserToken(user.ID, req.DeviceInfo)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to issue user token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"token":      rawToken,
		"account_no": user.AccountNo,
		"user_type":  models.NormalizeUserType(user.UserType),
		"token_exp":  tokenExpire,
	})
}

func (s *Server) userRedeemCode(c *gin.Context) {
	var req userRedeemCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	userID := getUint(c, ctxUserIDKey)
	managerID := getUint(c, ctxManagerIDKey)
	now := time.Now().UTC()

	err := s.db.Transaction(func(tx *gorm.DB) error {
		var code models.UserActivationCode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code = ?", req.Code).First(&code).Error; err != nil {
			return err
		}
		if code.Status != models.CodeStatusUnused {
			return fmt.Errorf("activation code already consumed")
		}
		if code.ManagerID != managerID {
			return fmt.Errorf("forbidden activation code")
		}
		var user models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; err != nil {
			return err
		}
		newExpire := extendExpiry(user.ExpiresAt, code.DurationDays, now)
		nextUserType := models.NormalizeUserType(code.UserType)
		if err := tx.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
			"expires_at": newExpire,
			"user_type":  nextUserType,
			"status":     models.UserStatusActive,
			"updated_at": now,
		}).Error; err != nil {
			return err
		}
		if err := s.ensureTaskConfigForTypeTx(tx, userID, nextUserType, now); err != nil {
			return err
		}
		if err := tx.Model(&models.UserActivationCode{}).Where("id = ?", code.ID).Updates(map[string]any{
			"status":          models.CodeStatusUsed,
			"used_by_user_id": userID,
			"used_at":         now,
		}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"detail": "activation code not found"})
			return
		}
		if strings.Contains(err.Error(), "forbidden") {
			c.JSON(http.StatusForbidden, gin.H{"detail": "activation code does not belong to your manager"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	s.audit(models.ActorTypeUser, userID, "redeem_user_activation_code", "user", userID, datatypes.JSONMap{"code": req.Code}, c.ClientIP())
	var user models.User
	if err := s.db.Where("id = ?", userID).First(&user).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "renewal success"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":    "renewal success",
		"user_type":  models.NormalizeUserType(user.UserType),
		"expires_at": user.ExpiresAt,
	})
}

func (s *Server) userGetMeProfile(c *gin.Context) {
	userID := getUint(c, ctxUserIDKey)
	tokenID := getUint(c, ctxUserTokenIDKey)

	var user models.User
	if err := s.db.Where("id = ?", userID).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "user not found"})
		return
	}

	var token models.UserToken
	if err := s.db.Where("id = ? AND user_id = ?", tokenID, userID).First(&token).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "user token not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user_id":       user.ID,
		"account_no":    user.AccountNo,
		"manager_id":    user.ManagerID,
		"user_type":     models.NormalizeUserType(user.UserType),
		"status":        user.Status,
		"expires_at":    user.ExpiresAt,
		"assets":        deepMergeMap(taskmeta.BuildDefaultUserAssets(), map[string]any(user.Assets)),
		"token_exp":     token.ExpiresAt,
		"token_created": token.CreatedAt,
		"last_used_at":  token.LastUsedAt,
	})
}

func (s *Server) userLogout(c *gin.Context) {
	var req userLogoutRequest
	if raw := strings.TrimSpace(c.GetHeader("Content-Length")); raw != "" && raw != "0" {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
			return
		}
	}

	userID := getUint(c, ctxUserIDKey)
	tokenID := getUint(c, ctxUserTokenIDKey)
	now := time.Now().UTC()

	query := s.db.Model(&models.UserToken{}).Where("user_id = ? AND revoked_at IS NULL", userID)
	if !req.All {
		query = query.Where("id = ?", tokenID)
	}
	result := query.Updates(map[string]any{"revoked_at": now})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to revoke token"})
		return
	}

	s.audit(models.ActorTypeUser, userID, "user_logout", "user_token", tokenID, datatypes.JSONMap{"all": req.All}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"message": "logout success", "revoked": result.RowsAffected, "all": req.All})
}

func (s *Server) userGetMeAssets(c *gin.Context) {
	userID := getUint(c, ctxUserIDKey)

	var user models.User
	if err := s.db.Where("id = ?", userID).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "user not found"})
		return
	}

	assets := deepMergeMap(taskmeta.BuildDefaultUserAssets(), map[string]any(user.Assets))
	c.JSON(http.StatusOK, gin.H{
		"user_id":    user.ID,
		"account_no": user.AccountNo,
		"user_type":  models.NormalizeUserType(user.UserType),
		"assets":     assets,
		"expires_at": user.ExpiresAt,
		"status":     user.Status,
	})
}

func (s *Server) userGetMeTasks(c *gin.Context) {
	userID := getUint(c, ctxUserIDKey)
	var user models.User
	if err := s.db.Where("id = ?", userID).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "user not found"})
		return
	}
	config, err := s.getOrCreateTaskConfig(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load task config"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"user_type":   models.NormalizeUserType(user.UserType),
		"task_config": config.TaskConfig,
		"version":     config.Version,
	})
}

func parseFlexibleDateTime(value string) (time.Time, error) {
	candidates := []string{
		time.RFC3339,
		"2006-01-02 15:04",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	input := strings.TrimSpace(value)
	if input == "" {
		return time.Time{}, fmt.Errorf("empty datetime")
	}
	for _, layout := range candidates {
		parsed, err := time.Parse(layout, input)
		if err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid datetime")
}

func (s *Server) userPutMeTasks(c *gin.Context) {
	userID := getUint(c, ctxUserIDKey)
	var req putTaskConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	updated, err := s.mergeTaskConfig(userID, req.TaskConfig)
	if err != nil {
		if errors.Is(err, errInvalidTaskConfigPatch) {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "task_config contains tasks not allowed for this user type"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to update task config"})
		return
	}
	var user models.User
	if err := s.db.Where("id = ?", userID).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "user not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"user_type":   models.NormalizeUserType(user.UserType),
		"task_config": updated.TaskConfig,
		"version":     updated.Version,
	})
}

func (s *Server) userGetMeLogs(c *gin.Context) {
	userID := getUint(c, ctxUserIDKey)
	managerID := getUint(c, ctxManagerIDKey)
	limit := readQueryInt(c, "limit", 50, 1, 200)
	items, err := s.queryUserLogs(managerID, userID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to query logs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (s *Server) agentLogin(c *gin.Context) {
	var req agentLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	var manager models.Manager
	if err := s.db.Where("username = ?", req.Username).First(&manager).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "invalid credentials"})
		return
	}
	if !auth.VerifyPassword(req.Password, manager.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "invalid credentials"})
		return
	}
	now := time.Now().UTC()
	if manager.Status != models.ManagerStatusActive || manager.ExpiresAt == nil || !manager.ExpiresAt.After(now) {
		c.JSON(http.StatusForbidden, gin.H{"detail": "manager not active"})
		return
	}
	if err := s.upsertAgentNode(manager.ID, req.NodeID, req.Version, now); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to update node"})
		return
	}
	token, err := s.tokenManager.IssueJWT(models.ActorTypeAgent, manager.ID, manager.ID, s.cfg.AgentJWTTTL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to issue token"})
		return
	}
	if err := s.redisStore.SaveAgentSession(
		c.Request.Context(),
		token,
		manager.ID,
		req.NodeID,
		s.cfg.AgentJWTTTL,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to save redis agent session"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "manager_id": manager.ID, "node_id": req.NodeID})
}

func (s *Server) agentPollJobs(c *gin.Context) {
	var req agentPollJobsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	managerID := getUint(c, ctxManagerIDKey)
	ctx := c.Request.Context()
	if req.Limit <= 0 {
		req.Limit = 5
	}
	if req.Limit > s.cfg.MaxPollLimit {
		req.Limit = s.cfg.MaxPollLimit
	}
	if req.LeaseSeconds <= 0 {
		req.LeaseSeconds = s.cfg.DefaultLeaseSecond
	}

	now := time.Now().UTC()
	leaseTTL := time.Duration(req.LeaseSeconds) * time.Second
	leaseUntil := now.Add(leaseTTL)

	leasedJobs := make([]models.TaskJob, 0, req.Limit)
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.upsertAgentNodeTx(tx, managerID, req.NodeID, "", now); err != nil {
			return err
		}
		var expiredJobs []models.TaskJob
		if err := tx.Where("manager_id = ? AND status IN ? AND lease_until < ?", managerID, []string{models.JobStatusLeased, models.JobStatusRunning}, now).
			Find(&expiredJobs).Error; err != nil {
			return err
		}
		if len(expiredJobs) > 0 {
			expiredIDs := make([]uint, 0, len(expiredJobs))
			for _, item := range expiredJobs {
				expiredIDs = append(expiredIDs, item.ID)
			}
			if err := tx.Model(&models.TaskJob{}).
				Where("id IN ?", expiredIDs).
				Updates(map[string]any{"status": models.JobStatusPending, "leased_by_node": "", "lease_until": nil, "updated_at": now, "attempts": gorm.Expr("attempts + 1")}).
				Error; err != nil {
				return err
			}
			for _, id := range expiredIDs {
				event := models.TaskJobEvent{
					JobID:     id,
					EventType: models.JobStatusRequeued,
					Message:   "lease timeout requeued",
					EventAt:   now,
				}
				if err := tx.Create(&event).Error; err != nil {
					return err
				}
			}
			for _, id := range expiredIDs {
				_ = s.redisStore.ClearJobLease(ctx, managerID, id)
			}
		}

		candidates := make([]models.TaskJob, 0, req.Limit)
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("manager_id = ? AND status = ? AND scheduled_at <= ?", managerID, models.JobStatusPending, now).
			Order("priority desc").Order("scheduled_at asc").Limit(req.Limit).Find(&candidates).Error; err != nil {
			return err
		}
		for _, job := range candidates {
			acquired, err := s.redisStore.AcquireJobLease(ctx, managerID, job.ID, req.NodeID, leaseTTL)
			if err != nil {
				return err
			}
			if !acquired {
				continue
			}

			updateResult := tx.Model(&models.TaskJob{}).Where("id = ? AND status = ?", job.ID, models.JobStatusPending).Updates(map[string]any{
				"status":         models.JobStatusLeased,
				"leased_by_node": req.NodeID,
				"lease_until":    leaseUntil,
				"updated_at":     now,
			})
			if updateResult.Error != nil {
				_ = s.redisStore.ReleaseJobLease(ctx, managerID, job.ID, req.NodeID)
				return updateResult.Error
			}
			if updateResult.RowsAffected == 0 {
				_ = s.redisStore.ReleaseJobLease(ctx, managerID, job.ID, req.NodeID)
				continue
			}

			event := models.TaskJobEvent{JobID: job.ID, EventType: "leased", Message: "job leased by agent", EventAt: now}
			if err := tx.Create(&event).Error; err != nil {
				_ = s.redisStore.ReleaseJobLease(ctx, managerID, job.ID, req.NodeID)
				return err
			}
			job.Status = models.JobStatusLeased
			job.LeasedByNode = req.NodeID
			job.LeaseUntil = &leaseUntil
			leasedJobs = append(leasedJobs, job)
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to poll jobs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"jobs": leasedJobs, "lease_until": leaseUntil})
}

func (s *Server) agentJobStart(c *gin.Context) {
	s.updateJobStatusByAgent(c, "start", models.JobStatusRunning)
}

func (s *Server) agentJobHeartbeat(c *gin.Context) {
	s.updateJobStatusByAgent(c, "heartbeat", "")
}

func (s *Server) agentJobComplete(c *gin.Context) {
	s.updateJobStatusByAgent(c, "success", models.JobStatusSuccess)
}

func (s *Server) agentJobFail(c *gin.Context) {
	s.updateJobStatusByAgent(c, "fail", models.JobStatusFailed)
}

func (s *Server) updateJobStatusByAgent(c *gin.Context, eventType string, nextStatus string) {
	var req agentJobUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	jobID, ok := parseUintParam(c, "job_id")
	if !ok {
		return
	}
	managerID := getUint(c, ctxManagerIDKey)
	ctx := c.Request.Context()
	now := time.Now().UTC()
	leaseSeconds := req.LeaseSeconds
	if leaseSeconds <= 0 {
		leaseSeconds = s.cfg.DefaultLeaseSecond
	}
	leaseTTL := time.Duration(leaseSeconds) * time.Second
	leaseUntil := now.Add(leaseTTL)

	owned, err := s.redisStore.IsJobLeaseOwner(ctx, managerID, jobID, req.NodeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to check redis lease owner"})
		return
	}
	if !owned {
		c.JSON(http.StatusForbidden, gin.H{"detail": "redis lease owner mismatch"})
		return
	}

	err = s.db.Transaction(func(tx *gorm.DB) error {
		var job models.TaskJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND manager_id = ?", jobID, managerID).First(&job).Error; err != nil {
			return err
		}
		if job.LeasedByNode != req.NodeID {
			return fmt.Errorf("node does not own this job")
		}

		updates := map[string]any{"updated_at": now}
		if nextStatus != "" {
			updates["status"] = nextStatus
		}
		if eventType == "heartbeat" || eventType == "start" {
			updates["lease_until"] = leaseUntil
		}
		if eventType == "fail" {
			updates["attempts"] = gorm.Expr("attempts + 1")
		}
		if err := tx.Model(&models.TaskJob{}).Where("id = ?", job.ID).Updates(updates).Error; err != nil {
			return err
		}
		event := models.TaskJobEvent{JobID: job.ID, EventType: eventType, Message: req.Message, ErrorCode: req.ErrorCode, EventAt: now}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if strings.Contains(err.Error(), "node does not own") {
			c.JSON(http.StatusForbidden, gin.H{"detail": err.Error()})
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"detail": "job not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	if eventType == "heartbeat" || eventType == "start" {
		refreshed, leaseErr := s.redisStore.RefreshJobLease(ctx, managerID, jobID, req.NodeID, leaseTTL)
		if leaseErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to refresh redis lease"})
			return
		}
		if !refreshed {
			c.JSON(http.StatusConflict, gin.H{"detail": "redis lease refresh conflict"})
			return
		}
	}

	if eventType == "success" || eventType == "fail" {
		if leaseErr := s.redisStore.ReleaseJobLease(ctx, managerID, jobID, req.NodeID); leaseErr != nil {
			c.JSON(http.StatusOK, gin.H{"message": "ok", "lease_warning": leaseErr.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

func (s *Server) createUserByActivationCode(tx *gorm.DB, code *models.UserActivationCode, createdBy string, now time.Time) (*models.User, error) {
	if code.Status != models.CodeStatusUnused {
		return nil, fmt.Errorf("activation code already consumed")
	}
	userType := models.NormalizeUserType(code.UserType)
	accountNo, err := s.generateAccountNo(tx)
	if err != nil {
		return nil, err
	}
	newExpire := extendExpiry(nil, code.DurationDays, now)
	user := models.User{
		AccountNo: accountNo,
		ManagerID: code.ManagerID,
		UserType:  userType,
		Status:    models.UserStatusActive,
		ExpiresAt: &newExpire,
		Assets:    datatypes.JSONMap(taskmeta.BuildDefaultUserAssets()),
		CreatedBy: createdBy,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := tx.Create(&user).Error; err != nil {
		return nil, err
	}
	if err := tx.Model(&models.UserActivationCode{}).Where("id = ?", code.ID).Updates(map[string]any{
		"status":          models.CodeStatusUsed,
		"used_by_user_id": user.ID,
		"used_at":         now,
	}).Error; err != nil {
		return nil, err
	}

	cfg := models.UserTaskConfig{
		UserID:     user.ID,
		TaskConfig: datatypes.JSONMap(taskmeta.BuildDefaultTaskConfigByType(userType)),
		UpdatedAt:  now,
		Version:    1,
	}
	if err := tx.Create(&cfg).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *Server) generateAccountNo(tx *gorm.DB) (string, error) {
	for i := 0; i < 8; i++ {
		candidate := fmt.Sprintf("U%s%03d", time.Now().UTC().Format("20060102150405"), rand.Intn(1000))
		var count int64
		if err := tx.Model(&models.User{}).Where("account_no = ?", candidate).Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return candidate, nil
		}
		time.Sleep(2 * time.Millisecond)
	}
	return "", fmt.Errorf("failed to generate unique account number")
}

func extendExpiry(current *time.Time, durationDays int, now time.Time) time.Time {
	base := now.UTC()
	if current != nil && current.After(base) {
		base = current.UTC()
	}
	return base.Add(time.Duration(durationDays) * 24 * time.Hour)
}

func (s *Server) issueUserToken(userID uint, deviceInfo string) (string, time.Time, error) {
	raw, err := auth.GenerateOpaqueToken("utk", 24)
	if err != nil {
		return "", time.Time{}, err
	}
	exp := time.Now().UTC().Add(s.cfg.UserTokenTTL)
	record := models.UserToken{
		UserID:     userID,
		TokenHash:  auth.HashToken(raw),
		ExpiresAt:  exp,
		CreatedAt:  time.Now().UTC(),
		DeviceInfo: deviceInfo,
	}
	if err := s.db.Create(&record).Error; err != nil {
		return "", time.Time{}, err
	}
	return raw, exp, nil
}

func (s *Server) upsertAgentNode(managerID uint, nodeID, version string, now time.Time) error {
	return s.upsertAgentNodeTx(s.db, managerID, nodeID, version, now)
}

func (s *Server) upsertAgentNodeTx(tx *gorm.DB, managerID uint, nodeID, version string, now time.Time) error {
	var node models.AgentNode
	err := tx.Where("node_id = ?", nodeID).First(&node).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		node = models.AgentNode{ManagerID: managerID, NodeID: nodeID, LastHeartbeat: now, Status: "online", Version: version, CreatedAt: now, UpdatedAt: now}
		return tx.Create(&node).Error
	}
	if err != nil {
		return err
	}
	return tx.Model(&models.AgentNode{}).Where("id = ?", node.ID).Updates(map[string]any{
		"manager_id":     managerID,
		"last_heartbeat": now,
		"status":         "online",
		"version":        version,
		"updated_at":     now,
	}).Error
}

func (s *Server) managerOwnsUser(c *gin.Context, managerID, userID uint) bool {
	var count int64
	if err := s.db.Model(&models.User{}).Where("id = ? AND manager_id = ?", userID, managerID).Count(&count).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to verify user owner"})
		return false
	}
	if count == 0 {
		c.JSON(http.StatusForbidden, gin.H{"detail": "user not under this manager"})
		return false
	}
	return true
}

func (s *Server) getOrCreateTaskConfig(userID uint) (*models.UserTaskConfig, error) {
	var user models.User
	if err := s.db.Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, err
	}
	userType := models.NormalizeUserType(user.UserType)

	var cfg models.UserTaskConfig
	err := s.db.Where("user_id = ?", userID).First(&cfg).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		now := time.Now().UTC()
		cfg = models.UserTaskConfig{
			UserID:     userID,
			TaskConfig: datatypes.JSONMap(taskmeta.BuildDefaultTaskConfigByType(userType)),
			UpdatedAt:  now,
			Version:    1,
		}
		if err := s.db.Create(&cfg).Error; err != nil {
			return nil, err
		}
		return &cfg, nil
	}
	if err != nil {
		return nil, err
	}
	normalized := taskmeta.NormalizeTaskConfigByType(map[string]any(cfg.TaskConfig), userType)
	cfg.TaskConfig = datatypes.JSONMap(normalized)
	return &cfg, nil
}

func (s *Server) mergeTaskConfig(userID uint, patch map[string]any) (*models.UserTaskConfig, error) {
	now := time.Now().UTC()
	var result models.UserTaskConfig
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; err != nil {
			return err
		}
		userType := models.NormalizeUserType(user.UserType)
		filteredPatch, err := taskmeta.FilterTaskPatchByType(patch, userType)
		if err != nil {
			return fmt.Errorf("%w: %v", errInvalidTaskConfigPatch, err)
		}

		var cfg models.UserTaskConfig
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&cfg).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			cfg = models.UserTaskConfig{
				UserID:     userID,
				TaskConfig: datatypes.JSONMap(taskmeta.BuildDefaultTaskConfigByType(userType)),
				UpdatedAt:  now,
				Version:    1,
			}
			if err := tx.Create(&cfg).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}

		base := taskmeta.NormalizeTaskConfigByType(map[string]any(cfg.TaskConfig), userType)
		merged := deepMergeMap(base, filteredPatch)
		cfg.TaskConfig = datatypes.JSONMap(merged)
		cfg.UpdatedAt = now
		cfg.Version = cfg.Version + 1
		if err := tx.Model(&models.UserTaskConfig{}).Where("id = ?", cfg.ID).Updates(map[string]any{
			"task_config": cfg.TaskConfig,
			"updated_at":  cfg.UpdatedAt,
			"version":     cfg.Version,
		}).Error; err != nil {
			return err
		}
		result = cfg
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Server) ensureTaskConfigForTypeTx(tx *gorm.DB, userID uint, userType string, now time.Time) error {
	resolvedType := models.NormalizeUserType(userType)
	var cfg models.UserTaskConfig
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&cfg).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		cfg = models.UserTaskConfig{
			UserID:     userID,
			TaskConfig: datatypes.JSONMap(taskmeta.BuildDefaultTaskConfigByType(resolvedType)),
			UpdatedAt:  now,
			Version:    1,
		}
		return tx.Create(&cfg).Error
	}
	if err != nil {
		return err
	}
	normalized := taskmeta.NormalizeTaskConfigByType(map[string]any(cfg.TaskConfig), resolvedType)
	cfg.TaskConfig = datatypes.JSONMap(normalized)
	cfg.UpdatedAt = now
	cfg.Version = cfg.Version + 1
	return tx.Model(&models.UserTaskConfig{}).Where("id = ?", cfg.ID).Updates(map[string]any{
		"task_config": cfg.TaskConfig,
		"updated_at":  cfg.UpdatedAt,
		"version":     cfg.Version,
	}).Error
}

func (s *Server) queryUserLogs(managerID, userID uint, limit int) ([]gin.H, error) {
	var jobs []models.TaskJob
	if err := s.db.Where("manager_id = ? AND user_id = ?", managerID, userID).Order("id desc").Limit(limit).Find(&jobs).Error; err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return []gin.H{}, nil
	}
	ids := make([]uint, 0, len(jobs))
	for _, j := range jobs {
		ids = append(ids, j.ID)
	}
	var events []models.TaskJobEvent
	if err := s.db.Where("job_id IN ?", ids).Order("event_at desc").Limit(limit * 3).Find(&events).Error; err != nil {
		return nil, err
	}
	result := make([]gin.H, 0, len(events))
	for _, e := range events {
		result = append(result, gin.H{
			"job_id":     e.JobID,
			"event_type": e.EventType,
			"message":    e.Message,
			"error_code": e.ErrorCode,
			"event_at":   e.EventAt,
		})
	}
	return result, nil
}

func isCodeStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case models.CodeStatusUnused, models.CodeStatusUsed, models.CodeStatusRevoked:
		return true
	default:
		return false
	}
}

func isManagerStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case models.ManagerStatusActive, models.ManagerStatusExpired, models.ManagerStatusDisabled:
		return true
	default:
		return false
	}
}

func isUserStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case models.UserStatusActive, models.UserStatusExpired, models.UserStatusDisabled:
		return true
	default:
		return false
	}
}

func parseUintParam(c *gin.Context, key string) (uint, bool) {
	id, err := strconv.Atoi(c.Param(key))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid " + key})
		return 0, false
	}
	return uint(id), true
}

func readQueryInt(c *gin.Context, key string, fallback, minValue, maxValue int) int {
	raw := c.Query(key)
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if parsed < minValue {
		return minValue
	}
	if parsed > maxValue {
		return maxValue
	}
	return parsed
}

func deepMergeMap(base map[string]any, patch map[string]any) map[string]any {
	merged := map[string]any{}
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range patch {
		existing, has := merged[k]
		if !has {
			merged[k] = v
			continue
		}
		existingMap, ok1 := existing.(map[string]any)
		patchMap, ok2 := v.(map[string]any)
		if ok1 && ok2 {
			merged[k] = deepMergeMap(existingMap, patchMap)
		} else {
			merged[k] = v
		}
	}
	return merged
}

func (s *Server) audit(actorType string, actorID uint, action, targetType string, targetID uint, detail datatypes.JSONMap, ip string) {
	entry := models.AuditLog{
		ActorType:  actorType,
		ActorID:    actorID,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Detail:     detail,
		IP:         ip,
		CreatedAt:  time.Now().UTC(),
	}
	_ = s.db.Create(&entry).Error
}
