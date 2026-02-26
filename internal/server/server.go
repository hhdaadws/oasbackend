package server

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"net/mail"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"oas-cloud-go/internal/auth"
	"oas-cloud-go/internal/cache"
	"oas-cloud-go/internal/config"
	"oas-cloud-go/internal/models"
	"oas-cloud-go/internal/notify"
	"oas-cloud-go/internal/scheduler"
	"oas-cloud-go/internal/taskmeta"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//go:embed static/super_console.html
var staticFS embed.FS

type Server struct {
	cfg              config.Config
	db               *gorm.DB
	redisStore       cache.Store
	generator        *scheduler.Generator
	tokenManager     *auth.TokenManager
	router           *gin.Engine
	auditCh          chan models.AuditLog
	auditOverflowSem chan struct{}
	notifyCh         chan notify.NotifyRequest
	notifier         *notify.Notifier
	scanWSHub        *ScanWSHub
}

var errInvalidTaskConfigPatch = errors.New("invalid task config patch")

func New(cfg config.Config, db *gorm.DB, redisStore cache.Store) *Server {
	app := &Server{
		cfg:              cfg,
		db:               db,
		redisStore:       redisStore,
		tokenManager:     auth.NewTokenManager(cfg.JWTSecret),
		router:           gin.New(),
		auditCh:          make(chan models.AuditLog, 1024),
		auditOverflowSem: make(chan struct{}, 10),
		notifyCh:         make(chan notify.NotifyRequest, 1024),
		notifier:         notify.NewNotifier(),
		scanWSHub:        newScanWSHub(),
	}
	if cfg.SchedulerEnabled {
		app.generator = scheduler.NewGenerator(cfg, db, redisStore)
		app.generator.Start()
	}
	go app.auditWorker()
	for i := 0; i < 8; i++ {
		go app.notifyWorker()
	}
	go app.scanJobTimeoutWorker()
	app.router.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		SkipPaths: []string{"/health"},
	}), gin.Recovery(), gzip.Gzip(gzip.BestSpeed))
	app.mountRoutes()
	return app
}

func (s *Server) Run() error {
	srv := &http.Server{
		Addr:         s.cfg.Addr,
		Handler:      s.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("server listening", "addr", s.cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	case sig := <-quit:
		slog.Info("received shutdown signal", "signal", sig)
	}

	if s.generator != nil {
		s.generator.Stop()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("server forced to shutdown: %w", err)
	}
	slog.Info("server exited gracefully")
	return nil
}

func (s *Server) mountRoutes() {
	var (
		healthMu     sync.Mutex
		lastCheck    time.Time
		cachedResult gin.H
		cachedCode   int
	)

	s.router.GET("/health", func(c *gin.Context) {
		healthMu.Lock()
		if time.Since(lastCheck) < 5*time.Second && cachedResult != nil {
			result, code := cachedResult, cachedCode
			healthMu.Unlock()
			c.JSON(code, result)
			return
		}
		healthMu.Unlock()

		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		status := gin.H{"status": "ok"}
		httpStatus := http.StatusOK

		// Check Redis
		redisErr := s.redisStore.Ping(ctx)
		if redisErr != nil {
			status["redis"] = "down"
			status["status"] = "degraded"
			httpStatus = http.StatusServiceUnavailable
		} else {
			status["redis"] = "up"
		}

		// Check DB
		sqlDB, dbErr := s.db.DB()
		if dbErr != nil {
			status["db"] = "down"
			status["status"] = "degraded"
			httpStatus = http.StatusServiceUnavailable
		} else if err := sqlDB.PingContext(ctx); err != nil {
			status["db"] = "down"
			status["status"] = "degraded"
			httpStatus = http.StatusServiceUnavailable
		} else {
			status["db"] = "up"
		}

		healthMu.Lock()
		cachedResult = status
		cachedCode = httpStatus
		lastCheck = time.Now()
		healthMu.Unlock()

		c.JSON(httpStatus, status)
	})
	s.router.GET("/super/console", s.superConsole)

	api := s.router.Group("/api/v1")
	{
		api.GET("/bootstrap/status", s.bootstrapStatus)
		api.GET("/scheduler/status", s.schedulerStatus)
		api.GET("/task-templates", s.taskTemplates)
		api.POST("/bootstrap/init", s.bootstrapInit)

		// Auth endpoints with stricter rate limiting (20 req/min per IP)
		authRL := s.rateLimitByIP("auth", 20, time.Minute)
		api.POST("/super/auth/login", authRL, s.superLogin)
		api.POST("/manager/auth/register", authRL, s.managerRegister)
		api.POST("/manager/auth/login", authRL, s.managerLogin)
		api.POST("/user/auth/register-by-code", authRL, s.userRegisterByCode)
		api.POST("/user/auth/login", authRL, s.userLogin)
		api.POST("/agent/auth/login", authRL, s.agentLogin)
	}

	superGroup := api.Group("/super")
	superGroup.Use(s.requireJWT(models.ActorTypeSuper))
	{
		superGroup.POST("/manager-renewal-keys", s.superCreateManagerRenewalKey)
		superGroup.GET("/manager-renewal-keys", s.superListManagerRenewalKeys)
		superGroup.PATCH("/manager-renewal-keys/:id/status", s.superPatchManagerRenewalKeyStatus)
		superGroup.GET("/managers", s.superListManagers)
		superGroup.PATCH("/managers/:id/lifecycle", s.superPatchManagerLifecycle)
		superGroup.PATCH("/managers/:id/password", s.superResetManagerPassword)
		superGroup.POST("/managers/batch-lifecycle", s.superBatchManagerLifecycle)
		superGroup.POST("/manager-renewal-keys/batch-revoke", s.superBatchRevokeRenewalKeys)
		superGroup.DELETE("/manager-renewal-keys/:id", s.superDeleteManagerRenewalKey)
		superGroup.POST("/manager-renewal-keys/batch-delete", s.superBatchDeleteRenewalKeys)
		superGroup.GET("/audit-logs", s.superListAuditLogs)
		superGroup.POST("/bloggers", s.superCreateBlogger)
		superGroup.GET("/bloggers", s.superListBloggers)
		superGroup.DELETE("/bloggers/:id", s.superDeleteBlogger)
	}

	managerAuthGroup := api.Group("/manager")
	managerAuthGroup.Use(s.requireJWT(models.ActorTypeManager))
	{
		managerAuthGroup.GET("/auth/me", s.managerGetMe)
		managerAuthGroup.POST("/auth/redeem-renewal-key", s.managerRedeemRenewalKey)
	}

	managerGroup := api.Group("/manager")
	managerGroup.Use(s.requireJWT(models.ActorTypeManager), s.requireManagerActive())
	{
		managerGroup.GET("/overview", s.managerOverview)
		managerGroup.PUT("/me/alias", s.managerPutMeAlias)
		managerGroup.GET("/task-pool", s.managerListTaskPool)
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
		managerGroup.DELETE("/users/:user_id/logs", s.managerDeleteUserLogs)
		managerGroup.PATCH("/users/:user_id/settings", s.managerPatchUserSettings)
		managerGroup.POST("/users/batch-lifecycle", s.managerBatchUserLifecycle)
		managerGroup.POST("/users/batch-assets", s.managerBatchUserAssets)
		managerGroup.DELETE("/users/:user_id", s.managerDeleteUser)
		managerGroup.POST("/users/batch-delete", s.managerBatchDeleteUsers)
		managerGroup.POST("/activation-codes/batch-revoke", s.managerBatchRevokeActivationCodes)
		managerGroup.DELETE("/activation-codes/:id", s.managerDeleteActivationCode)
		managerGroup.POST("/activation-codes/batch-delete", s.managerBatchDeleteActivationCodes)
		managerGroup.GET("/duiyi-answers", s.managerGetDuiyiAnswers)
		managerGroup.PUT("/duiyi-answers", s.managerPutDuiyiAnswers)
		managerGroup.GET("/bloggers", s.managerListBloggers)
		managerGroup.GET("/blogger-answers/:blogger_id", s.managerGetBloggerAnswers)
		managerGroup.PUT("/blogger-answers/:blogger_id", s.managerPutBloggerAnswer)
	}

	userGroup := api.Group("/user")
	userGroup.Use(s.requireUserToken())
	{
		userGroup.POST("/auth/logout", s.userLogout)
		userGroup.POST("/auth/redeem-code", s.userRedeemCode)
		userGroup.GET("/me/profile", s.userGetMeProfile)
		userGroup.PUT("/me/profile", s.userPutMeProfile)
		userGroup.GET("/me/assets", s.userGetMeAssets)
		userGroup.GET("/me/tasks", s.userGetMeTasks)
		userGroup.PUT("/me/tasks", s.userPutMeTasks)
		userGroup.GET("/me/logs", s.userGetMeLogs)
		userGroup.GET("/me/lineup", s.userGetMeLineup)
		userGroup.PUT("/me/lineup", s.userPutMeLineup)
		userGroup.POST("/scan/create", s.userScanCreate)
		userGroup.GET("/scan/status", s.userScanStatus)
		userGroup.POST("/scan/choice", s.userScanChoice)
		userGroup.POST("/scan/cancel", s.userScanCancel)
		userGroup.POST("/scan/heartbeat", s.userScanHeartbeat)

		// Friend system (jingzhi users only)
		userGroup.GET("/friends", s.userListFriends)
		userGroup.GET("/friend-requests", s.userListFriendRequests)
		userGroup.POST("/friends/request", s.userSendFriendRequest)
		userGroup.POST("/friends/:id/accept", s.userAcceptFriendRequest)
		userGroup.POST("/friends/:id/reject", s.userRejectFriendRequest)
		userGroup.DELETE("/friends/:id", s.userDeleteFriend)
		userGroup.GET("/friends/candidates", s.userListFriendCandidates)

		// Team Yuhun (jingzhi users only)
		userGroup.POST("/team-yuhun/request", s.userSendTeamYuhunRequest)
		userGroup.GET("/team-yuhun/requests", s.userListTeamYuhunRequests)
		userGroup.GET("/team-yuhun/booked-slots", s.userListTeamYuhunBookedSlots)
		userGroup.POST("/team-yuhun/:id/accept", s.userAcceptTeamYuhunRequest)
		userGroup.POST("/team-yuhun/:id/reject", s.userRejectTeamYuhunRequest)
		userGroup.DELETE("/team-yuhun/:id", s.userCancelTeamYuhunRequest)

		// Duiyi answer source (duiyi users)
		userGroup.GET("/duiyi-answer-sources", s.userGetDuiyiAnswerSources)
		userGroup.PUT("/duiyi-answer-source", s.userPutDuiyiAnswerSource)
	}
	// WebSocket endpoint (no middleware — token validated inside handler)
	api.GET("/user/scan/ws", s.userScanWS)

	agentGroup := api.Group("/agent")
	agentGroup.Use(s.requireJWT(models.ActorTypeAgent))
	{
		agentGroup.POST("/poll-jobs", s.rateLimitByActor("poll", 2, time.Second), s.agentPollJobs)
		agentGroup.POST("/jobs/:job_id/start", s.agentJobStart)
		agentGroup.POST("/jobs/:job_id/heartbeat", s.agentJobHeartbeat)
		agentGroup.POST("/jobs/:job_id/complete", s.agentJobComplete)
		agentGroup.POST("/jobs/:job_id/fail", s.agentJobFail)
		agentGroup.GET("/users/:user_id/full-config", s.agentGetUserFullConfig)
		agentGroup.PATCH("/users/:user_id/game-profile", s.agentUpdateUserGameProfile)
		agentGroup.PUT("/users/:user_id/explore-progress", s.agentUpdateExploreProgress)
		agentGroup.POST("/users/:user_id/logs", s.agentReportLogs)
		agentGroup.POST("/scan/poll", s.agentScanPoll)
		agentGroup.POST("/scan/:scan_id/start", s.agentScanStart)
		agentGroup.POST("/scan/:scan_id/phase", s.agentScanPhase)
		agentGroup.GET("/scan/:scan_id/choice", s.agentScanGetChoice)
		agentGroup.POST("/scan/:scan_id/heartbeat", s.agentScanHeartbeat)
		agentGroup.POST("/scan/:scan_id/complete", s.agentScanComplete)
		agentGroup.POST("/scan/:scan_id/fail", s.agentScanFail)
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
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效的用户类型"})
		return
	}
	userType := models.NormalizeUserType(rawType)
	c.Header("Cache-Control", "public, max-age=3600")
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
		assetsGroup := s.router.Group("/assets")
		assetsGroup.Use(func(c *gin.Context) {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
			c.Next()
		})
		assetsGroup.Static("/", assetsPath)
	}
	s.router.GET("/", func(c *gin.Context) {
		c.File(indexPath)
	})

	s.router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api/") || path == "/health" || strings.HasPrefix(path, "/super/") {
			c.JSON(http.StatusNotFound, gin.H{"detail": "记录不存在"})
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
		c.JSON(http.StatusConflict, gin.H{"detail": "超级管理员已初始化"})
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "密码加密失败"})
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
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "创建超级管理员失败"})
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
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "账号或密码错误"})
		return
	}
	if !auth.VerifyPassword(req.Password, admin.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "账号或密码错误"})
		return
	}
	token, err := s.tokenManager.IssueJWT(models.ActorTypeSuper, admin.ID, 0, s.cfg.JWTTTL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "令牌签发失败"})
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
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "密码加密失败"})
		return
	}
	now := time.Now().UTC()
	manager := models.Manager{
		Username:     req.Username,
		PasswordHash: hash,
		ExpiresAt:    &now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.db.Create(&manager).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"detail": "用户名已存在"})
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
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "账号或密码错误"})
		return
	}
	if !auth.VerifyPassword(req.Password, manager.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "账号或密码错误"})
		return
	}
	now := time.Now().UTC()
	token, err := s.tokenManager.IssueJWT(models.ActorTypeManager, manager.ID, manager.ID, s.cfg.JWTTTL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "令牌签发失败"})
		return
	}
	expired := manager.ExpiresAt == nil || !manager.ExpiresAt.After(now)
	msg := "登录成功"
	if expired {
		msg = "登录成功，账号已过期，请使用续费密钥续费"
	}
	c.JSON(http.StatusOK, gin.H{
		"token":      token,
		"role":       models.ActorTypeManager,
		"manager_id": manager.ID,
		"expires_at": manager.ExpiresAt,
		"expired":    expired,
		"message":    msg,
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
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "生成密钥失败"})
		return
	}
	actorID := getUint(c, ctxActorIDKey)
	now := time.Now().UTC()
	key := models.ManagerRenewalKey{
		Code:                  code,
		DurationDays:          req.DurationDays,
		ManagerType:           models.NormalizeManagerType(req.ManagerType),
		Status:                models.CodeStatusUnused,
		CreatedBySuperAdminID: actorID,
		CreatedAt:             now,
	}
	if err := s.db.Create(&key).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "保存密钥失败"})
		return
	}
	s.audit(models.ActorTypeSuper, actorID, "create_manager_renewal_key", "manager_renewal_key", key.ID, datatypes.JSONMap{"duration_days": req.DurationDays, "manager_type": key.ManagerType}, c.ClientIP())
	c.JSON(http.StatusCreated, gin.H{"code": key.Code, "duration_days": key.DurationDays, "manager_type": key.ManagerType})
}

func (s *Server) superListManagerRenewalKeys(c *gin.Context) {
	status := strings.TrimSpace(c.Query("status"))
	keyword := strings.TrimSpace(c.Query("keyword"))
	pg := readPagination(c, 50, 200)
	if status != "" && !isCodeStatus(status) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效的状态值"})
		return
	}

	baseQuery := s.db.Model(&models.ManagerRenewalKey{})
	if status != "" {
		baseQuery = baseQuery.Where("status = ?", status)
	}
	if keyword != "" {
		baseQuery = baseQuery.Where("code LIKE ?", "%"+keyword+"%")
	}

	var filteredTotal int64
	if err := baseQuery.Count(&filteredTotal).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "统计续费密钥失败"})
		return
	}

	var keys []models.ManagerRenewalKey
	if err := baseQuery.Order("id desc").Offset(pg.Offset).Limit(pg.PageSize).Find(&keys).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询续费密钥失败"})
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
	var totalCount, unusedCount, usedCount, revokedCount int64
	type statusAgg struct {
		Status string `gorm:"column:status"`
		Cnt    int64  `gorm:"column:cnt"`
	}
	var aggResults []statusAgg
	s.db.Model(&models.ManagerRenewalKey{}).Select("status, COUNT(*) as cnt").Group("status").Find(&aggResults)
	for _, r := range aggResults {
		totalCount += r.Cnt
		switch r.Status {
		case models.CodeStatusUnused:
			unusedCount = r.Cnt
		case models.CodeStatusUsed:
			usedCount = r.Cnt
		case models.CodeStatusRevoked:
			revokedCount = r.Cnt
		}
	}
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
			"manager_type":              key.ManagerType,
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
		"total":     filteredTotal,
		"page":      pg.Page,
		"page_size": pg.PageSize,
	})
}

func (s *Server) superListManagers(c *gin.Context) {
	keyword := strings.TrimSpace(c.Query("keyword"))
	pg := readPagination(c, 50, 200)

	baseQuery := s.db.Model(&models.Manager{})
	if keyword != "" {
		baseQuery = baseQuery.Where("username LIKE ?", "%"+keyword+"%")
	}

	var filteredTotal int64
	if err := baseQuery.Count(&filteredTotal).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "统计管理员失败"})
		return
	}

	var managers []models.Manager
	if err := baseQuery.Order("id desc").Offset(pg.Offset).Limit(pg.PageSize).Find(&managers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询管理员失败"})
		return
	}

	now := time.Now().UTC()
	expiringThreshold := now.Add(7 * 24 * time.Hour)

	// Summary counts using single query with conditional aggregation
	type managerSummary struct {
		TotalAll     int64 `gorm:"column:total_all"`
		ActiveAll    int64 `gorm:"column:active_all"`
		Expiring7d   int64 `gorm:"column:expiring_7d"`
	}
	var ms managerSummary
	s.db.Model(&models.Manager{}).Select(
		"COUNT(*) as total_all, "+
			"SUM(CASE WHEN expires_at IS NOT NULL AND expires_at > ? THEN 1 ELSE 0 END) as active_all, "+
			"SUM(CASE WHEN expires_at IS NOT NULL AND expires_at > ? AND expires_at < ? THEN 1 ELSE 0 END) as expiring_7d",
		now, now, expiringThreshold,
	).Scan(&ms)
	totalAll := ms.TotalAll
	activeAll := ms.ActiveAll
	expiredAll := totalAll - activeAll
	expiring7dAll := ms.Expiring7d

	// Per-manager user statistics
	type managerUserStats struct {
		ManagerID    uint  `gorm:"column:manager_id"`
		TotalUsers   int64 `gorm:"column:total_users"`
		ActiveUsers  int64 `gorm:"column:active_users"`
		ExpiredUsers int64 `gorm:"column:expired_users"`
	}
	statsMap := make(map[uint]managerUserStats)
	if len(managers) > 0 {
		managerIDs := make([]uint, len(managers))
		for i, m := range managers {
			managerIDs[i] = m.ID
		}
		var statsList []managerUserStats
		s.db.Model(&models.User{}).
			Select("manager_id, COUNT(*) as total_users, SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) as active_users, SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) as expired_users", models.UserStatusActive, models.UserStatusExpired).
			Where("manager_id IN ?", managerIDs).
			Group("manager_id").
			Scan(&statsList)
		for _, st := range statsList {
			statsMap[st.ManagerID] = st
		}
	}

	items := make([]gin.H, 0, len(managers))
	for _, manager := range managers {
		expiresAt := manager.ExpiresAt
		isExpired := expiresAt == nil || !expiresAt.After(now)
		st := statsMap[manager.ID]
		items = append(items, gin.H{
			"id":            manager.ID,
			"username":      manager.Username,
			"manager_type":  manager.ManagerType,
			"expires_at":    manager.ExpiresAt,
			"is_expired":    isExpired,
			"created_at":    manager.CreatedAt,
			"updated_at":    manager.UpdatedAt,
			"total_users":   st.TotalUsers,
			"active_users":  st.ActiveUsers,
			"expired_users": st.ExpiredUsers,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"summary": gin.H{
			"total":       totalAll,
			"active":      activeAll,
			"expired":     expiredAll,
			"expiring_7d": expiring7dAll,
		},
		"total":     filteredTotal,
		"page":      pg.Page,
		"page_size": pg.PageSize,
	})
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
			c.JSON(http.StatusNotFound, gin.H{"detail": "续费密钥不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询续费密钥失败"})
		return
	}
	if key.Status == models.CodeStatusUsed {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "已使用的续费密钥不可撤销"})
		return
	}
	if key.Status == models.CodeStatusRevoked {
		c.JSON(http.StatusOK, gin.H{"message": "renewal key already revoked"})
		return
	}

	if err := s.db.Model(&models.ManagerRenewalKey{}).Where("id = ?", keyID).Updates(map[string]any{
		"status": models.CodeStatusRevoked,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "撤销续费密钥失败"})
		return
	}
	actorID := getUint(c, ctxActorIDKey)
	s.audit(models.ActorTypeSuper, actorID, "patch_manager_renewal_key_status", "manager_renewal_key", keyID, datatypes.JSONMap{
		"status": req.Status,
	}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"message": "renewal key revoked"})
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
	if strings.TrimSpace(req.ExpiresAt) == "" && req.ExtendDays == 0 && strings.TrimSpace(req.ManagerType) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "至少需要提供一个字段"})
		return
	}

	now := time.Now().UTC()
	var manager models.Manager
	if err := s.db.Where("id = ?", managerID).First(&manager).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"detail": "管理员不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询管理员失败"})
		return
	}

	updates := map[string]any{"updated_at": now}
	if rawExpires := strings.TrimSpace(req.ExpiresAt); rawExpires != "" {
		parsed, err := parseFlexibleDateTime(rawExpires)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "到期时间格式无效"})
			return
		}
		updates["expires_at"] = parsed
	}
	if req.ExtendDays != 0 {
		if req.ExtendDays < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "延长天数必须为正数"})
			return
		}
		newExpire := extendExpiry(manager.ExpiresAt, req.ExtendDays, now)
		updates["expires_at"] = newExpire
	}
	if mt := strings.TrimSpace(req.ManagerType); mt != "" {
		updates["manager_type"] = models.NormalizeManagerType(mt)
	}

	if err := s.db.Model(&models.Manager{}).Where("id = ?", managerID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "更新管理员生命周期失败"})
		return
	}
	actorID := getUint(c, ctxActorIDKey)
	s.audit(models.ActorTypeSuper, actorID, "patch_manager_lifecycle", "manager", managerID, datatypes.JSONMap{
		"expires_at":   req.ExpiresAt,
		"extend_days":  req.ExtendDays,
		"manager_type": req.ManagerType,
	}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"message": "manager lifecycle updated"})
}

func (s *Server) superResetManagerPassword(c *gin.Context) {
	managerID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}

	var req superResetManagerPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	var manager models.Manager
	if err := s.db.Where("id = ?", managerID).First(&manager).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"detail": "管理员不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询管理员失败"})
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "密码加密失败"})
		return
	}

	now := time.Now().UTC()
	if err := s.db.Model(&models.Manager{}).Where("id = ?", managerID).Updates(map[string]any{
		"password_hash": hash,
		"updated_at":    now,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "重置密码失败"})
		return
	}

	actorID := getUint(c, ctxActorIDKey)
	s.audit(models.ActorTypeSuper, actorID, "reset_manager_password", "manager", managerID, datatypes.JSONMap{
		"manager_username": manager.Username,
	}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"message": "manager password reset"})
}

// ── Super batch handlers ──────────────────────────────

func (s *Server) superBatchManagerLifecycle(c *gin.Context) {
	var req batchManagerLifecycleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	if strings.TrimSpace(req.ExpiresAt) == "" && req.ExtendDays == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "至少需要提供一个字段"})
		return
	}
	if req.ExtendDays < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "延长天数必须为正数"})
		return
	}

	var parsedExpires time.Time
	hasExpires := false
	if rawExpires := strings.TrimSpace(req.ExpiresAt); rawExpires != "" {
		parsed, err := parseFlexibleDateTime(rawExpires)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "到期时间格式无效"})
			return
		}
		parsedExpires = parsed
		hasExpires = true
	}

	now := time.Now().UTC()
	actorID := getUint(c, ctxActorIDKey)
	var updated int64

	err := s.db.Transaction(func(tx *gorm.DB) error {
		var managers []models.Manager
		if err := tx.Where("id IN ?", req.ManagerIDs).Find(&managers).Error; err != nil {
			return err
		}
		if len(managers) != len(req.ManagerIDs) {
			return fmt.Errorf("some manager IDs not found")
		}

		// Fast path: no ExtendDays, all managers get same updates → single bulk UPDATE
		if req.ExtendDays == 0 && hasExpires {
			updates := map[string]any{"updated_at": now, "expires_at": parsedExpires}
			result := tx.Model(&models.Manager{}).Where("id IN ?", req.ManagerIDs).Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			updated = result.RowsAffected
			return nil
		}

		// Slow path: ExtendDays > 0, each manager has different expires_at
		for _, manager := range managers {
			updates := map[string]any{"updated_at": now}
			if hasExpires {
				updates["expires_at"] = parsedExpires
			}
			if req.ExtendDays > 0 {
				newExpire := extendExpiry(manager.ExpiresAt, req.ExtendDays, now)
				updates["expires_at"] = newExpire
			}
			if err := tx.Model(&models.Manager{}).Where("id = ?", manager.ID).Updates(updates).Error; err != nil {
				return err
			}
			updated++
		}
		return nil
	})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "批量更新管理员生命周期失败"})
		return
	}
	s.audit(models.ActorTypeSuper, actorID, "batch_manager_lifecycle", "manager", 0, datatypes.JSONMap{
		"manager_ids": req.ManagerIDs,
		"extend_days": req.ExtendDays,
		"expires_at":  req.ExpiresAt,
		"updated":     updated,
	}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"updated": updated})
}

func (s *Server) superBatchRevokeRenewalKeys(c *gin.Context) {
	var req batchRenewalKeyRevokeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	actorID := getUint(c, ctxActorIDKey)

	result := s.db.Model(&models.ManagerRenewalKey{}).
		Where("id IN ? AND status = ?", req.KeyIDs, models.CodeStatusUnused).
		Updates(map[string]any{"status": models.CodeStatusRevoked})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "批量撤销续费密钥失败"})
		return
	}
	s.audit(models.ActorTypeSuper, actorID, "batch_revoke_renewal_keys", "manager_renewal_key", 0, datatypes.JSONMap{
		"key_ids": req.KeyIDs,
		"revoked": result.RowsAffected,
	}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"revoked": result.RowsAffected, "requested": len(req.KeyIDs)})
}

func (s *Server) superDeleteManagerRenewalKey(c *gin.Context) {
	keyID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	var key models.ManagerRenewalKey
	if err := s.db.Where("id = ?", keyID).First(&key).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"detail": "续费密钥不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询续费密钥失败"})
		return
	}
	if err := s.db.Delete(&models.ManagerRenewalKey{}, keyID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "删除续费密钥失败"})
		return
	}
	actorID := getUint(c, ctxActorIDKey)
	s.audit(models.ActorTypeSuper, actorID, "delete_manager_renewal_key", "manager_renewal_key", keyID, datatypes.JSONMap{
		"code":   key.Code,
		"status": key.Status,
	}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"message": "renewal key deleted"})
}

func (s *Server) superBatchDeleteRenewalKeys(c *gin.Context) {
	var req batchRenewalKeyDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	actorID := getUint(c, ctxActorIDKey)

	result := s.db.Where("id IN ? AND status IN ?", req.IDs, []string{models.CodeStatusUnused, models.CodeStatusUsed}).Delete(&models.ManagerRenewalKey{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "批量删除续费密钥失败"})
		return
	}
	s.audit(models.ActorTypeSuper, actorID, "batch_delete_renewal_keys", "manager_renewal_key", 0, datatypes.JSONMap{
		"ids":     req.IDs,
		"deleted": result.RowsAffected,
	}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"deleted": result.RowsAffected, "requested": len(req.IDs)})
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
		updates := map[string]any{
			"expires_at": newExpire,
			"updated_at": now,
		}
		if key.ManagerType != "" {
			updates["manager_type"] = models.NormalizeManagerType(key.ManagerType)
		}
		if err := tx.Model(&models.Manager{}).Where("id = ?", managerID).Updates(updates).Error; err != nil {
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
			c.JSON(http.StatusNotFound, gin.H{"detail": "续费密钥不存在"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	s.audit(models.ActorTypeManager, managerID, "redeem_manager_renewal_key", "manager", managerID, datatypes.JSONMap{"code": req.Code}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"message": "renewal success"})
}

func (s *Server) managerGetMe(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	var manager models.Manager
	if err := s.db.Where("id = ?", managerID).First(&manager).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"detail": "管理员不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询管理员失败"})
		return
	}
	now := time.Now().UTC()
	expired := manager.ExpiresAt == nil || !manager.ExpiresAt.After(now)
	c.JSON(http.StatusOK, gin.H{
		"id":           manager.ID,
		"username":     manager.Username,
		"alias":        manager.Alias,
		"manager_type": manager.ManagerType,
		"expires_at":   manager.ExpiresAt,
		"expired":      expired,
	})
}

func (s *Server) managerPutMeAlias(c *gin.Context) {
	var req managerPutAliasRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	managerID := getUint(c, ctxActorIDKey)
	alias := strings.TrimSpace(req.Alias)
	if err := s.db.Model(&models.Manager{}).Where("id = ?", managerID).Update("alias", alias).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "更新别称失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"alias": alias})
}

func (s *Server) managerCreateActivationCode(c *gin.Context) {
	var req createActivationCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	managerID := getUint(c, ctxActorIDKey)

	var manager models.Manager
	if err := s.db.Where("id = ?", managerID).First(&manager).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询管理员失败"})
		return
	}

	userType := models.NormalizeUserType(req.UserType)
	if manager.ManagerType != models.ManagerTypeAll {
		userType = manager.ManagerType
	} else if !models.ManagerCanCreateUserType(manager.ManagerType, userType) {
		c.JSON(http.StatusForbidden, gin.H{"detail": "无权创建该类型的激活码"})
		return
	}

	code, err := auth.GenerateOpaqueToken("uac", 12)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "生成激活码失败"})
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
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "创建激活码失败"})
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
	pg := readPagination(c, 50, 200)

	if status != "" && !isCodeStatus(status) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效的状态值"})
		return
	}
	if userType != "" && !models.IsValidUserType(userType) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效的用户类型"})
		return
	}

	baseQuery := s.db.Model(&models.UserActivationCode{}).
		Where("manager_id = ?", managerID)
	if status != "" {
		baseQuery = baseQuery.Where("status = ?", status)
	}
	if userType != "" {
		baseQuery = baseQuery.Where("user_type = ?", models.NormalizeUserType(userType))
	}
	if keyword != "" {
		baseQuery = baseQuery.Where("code LIKE ?", "%"+keyword+"%")
	}

	var filteredTotal int64
	if err := baseQuery.Count(&filteredTotal).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "统计激活码失败"})
		return
	}

	var codes []models.UserActivationCode
	if err := baseQuery.Order("id desc").Offset(pg.Offset).Limit(pg.PageSize).Find(&codes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询激活码失败"})
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

	var totalCount, unusedCount, usedCount, revokedCount int64
	type codeStatusAgg struct {
		Status string `gorm:"column:status"`
		Cnt    int64  `gorm:"column:cnt"`
	}
	var codeAggResults []codeStatusAgg
	s.db.Model(&models.UserActivationCode{}).Select("status, COUNT(*) as cnt").Where("manager_id = ?", managerID).Group("status").Find(&codeAggResults)
	for _, r := range codeAggResults {
		totalCount += r.Cnt
		switch r.Status {
		case models.CodeStatusUnused:
			unusedCount = r.Cnt
		case models.CodeStatusUsed:
			usedCount = r.Cnt
		case models.CodeStatusRevoked:
			revokedCount = r.Cnt
		}
	}

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
		"total":     filteredTotal,
		"page":      pg.Page,
		"page_size": pg.PageSize,
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
			c.JSON(http.StatusNotFound, gin.H{"detail": "激活码不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询激活码失败"})
		return
	}
	if code.Status == models.CodeStatusUsed {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "已使用的激活码不可撤销"})
		return
	}
	if code.Status == models.CodeStatusRevoked {
		c.JSON(http.StatusOK, gin.H{"message": "activation code already revoked"})
		return
	}
	if err := s.db.Model(&models.UserActivationCode{}).Where("id = ?", codeID).Updates(map[string]any{
		"status": models.CodeStatusRevoked,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "撤销激活码失败"})
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

	var manager models.Manager
	if err := s.db.Where("id = ?", managerID).First(&manager).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询管理员失败"})
		return
	}

	userType := models.NormalizeUserType(req.UserType)
	if manager.ManagerType != models.ManagerTypeAll {
		userType = manager.ManagerType
	}

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
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "快速创建用户失败"})
		return
	}
	s.audit(models.ActorTypeManager, managerID, "quick_create_user", "user", createdUser.ID, datatypes.JSONMap{
		"duration_days": req.DurationDays,
		"user_type":     createdUser.UserType,
	}, c.ClientIP())
	c.JSON(http.StatusCreated, gin.H{
		"account_no": createdUser.AccountNo,
		"login_id":   createdUser.LoginID,
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
	loginID := strings.TrimSpace(c.Query("login_id"))
	pg := readPagination(c, 50, 200)
	if status != "" && !isUserStatus(status) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效的状态值"})
		return
	}
	if userType != "" && !models.IsValidUserType(userType) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效的用户类型"})
		return
	}

	baseQuery := s.db.Model(&models.User{}).Where("manager_id = ?", managerID)
	if status != "" {
		baseQuery = baseQuery.Where("status = ?", status)
	}
	if userType != "" {
		baseQuery = baseQuery.Where("user_type = ?", models.NormalizeUserType(userType))
	}
	if keyword != "" {
		baseQuery = baseQuery.Where("account_no LIKE ? OR login_id LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	if loginID != "" {
		baseQuery = baseQuery.Where("login_id = ?", loginID)
	}

	var filteredTotal int64
	if err := baseQuery.Count(&filteredTotal).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "统计用户失败"})
		return
	}

	var users []models.User
	if err := baseQuery.Order("id asc").Offset(pg.Offset).Limit(pg.PageSize).Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询用户失败"})
		return
	}

	now := time.Now().UTC()

	// Summary counts using GROUP BY (replaces 7 individual COUNT queries)
	var totalAll, activeAll, expiredAll, disabledAll int64
	type userStatusAgg struct {
		Status string `gorm:"column:status"`
		Cnt    int64  `gorm:"column:cnt"`
	}
	var statusAggs []userStatusAgg
	s.db.Model(&models.User{}).Select("status, COUNT(*) as cnt").Where("manager_id = ?", managerID).Group("status").Find(&statusAggs)
	for _, r := range statusAggs {
		totalAll += r.Cnt
		switch r.Status {
		case models.UserStatusActive:
			activeAll = r.Cnt
		case models.UserStatusExpired:
			expiredAll = r.Cnt
		case models.UserStatusDisabled:
			disabledAll = r.Cnt
		}
	}
	var dailyAll, duiyiAll, shuakaAll int64
	type userTypeAgg struct {
		UserType string `gorm:"column:user_type"`
		Cnt      int64  `gorm:"column:cnt"`
	}
	var typeAggs []userTypeAgg
	s.db.Model(&models.User{}).Select("user_type, COUNT(*) as cnt").Where("manager_id = ?", managerID).Group("user_type").Find(&typeAggs)
	for _, r := range typeAggs {
		switch r.UserType {
		case models.UserTypeDaily:
			dailyAll = r.Cnt
		case models.UserTypeDuiyi:
			duiyiAll = r.Cnt
		case models.UserTypeShuaka:
			shuakaAll = r.Cnt
		}
	}

	items := make([]gin.H, 0, len(users))
	for _, user := range users {
		user.UserType = models.NormalizeUserType(user.UserType)
		isExpired := user.ExpiresAt == nil || !user.ExpiresAt.After(now)
		items = append(items, gin.H{
			"id":             user.ID,
			"account_no":     user.AccountNo,
			"login_id":       user.LoginID,
			"manager_id":     user.ManagerID,
			"user_type":      user.UserType,
			"status":         user.Status,
			"archive_status": user.ArchiveStatus,
			"server":         user.Server,
			"username":       user.Username,
			"is_expired":     isExpired,
			"expires_at":     user.ExpiresAt,
			"created_by":     user.CreatedBy,
			"created_at":     user.CreatedAt,
			"updated_at":     user.UpdatedAt,
			"can_view_logs":  user.CanViewLogs,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"summary": gin.H{
			"total":    totalAll,
			"active":   activeAll,
			"expired":  expiredAll,
			"disabled": disabledAll,
			"daily":    dailyAll,
			"duiyi":    duiyiAll,
			"shuaka":   shuakaAll,
		},
		"total":     filteredTotal,
		"page":      pg.Page,
		"page_size": pg.PageSize,
	})
}

func (s *Server) managerOverview(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	now := time.Now().UTC()

	userStats := gin.H{
		"total":   int64(0),
		"active":  int64(0),
		"expired": int64(0),
	}
	jobStats := gin.H{
		"pending": int64(0),
		"leased":  int64(0),
		"running": int64(0),
		"success": int64(0),
		"failed":  int64(0),
	}

	// User stats via GROUP BY (replaces 2 individual COUNT queries)
	type overviewAgg struct {
		Status string `gorm:"column:status"`
		Cnt    int64  `gorm:"column:cnt"`
	}
	var userAggs []overviewAgg
	if err := s.db.Model(&models.User{}).Select("status, COUNT(*) as cnt").Where("manager_id = ?", managerID).Group("status").Find(&userAggs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询用户概览失败"})
		return
	}
	var totalUsers int64
	for _, r := range userAggs {
		userStats[r.Status] = r.Cnt
		totalUsers += r.Cnt
	}
	userStats["total"] = totalUsers

	// Job stats via GROUP BY (replaces 5 individual COUNT queries)
	var jobAggs []overviewAgg
	if err := s.db.Model(&models.TaskJob{}).Select("status, COUNT(*) as cnt").Where("manager_id = ?", managerID).Group("status").Find(&jobAggs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询任务概览失败"})
		return
	}
	for _, r := range jobAggs {
		jobStats[r.Status] = r.Cnt
	}

	var recentFailures int64
	since := now.Add(-24 * time.Hour)
	if err := s.db.Model(&models.TaskJobEvent{}).
		Joins("JOIN task_jobs ON task_jobs.id = task_job_events.job_id").
		Where("task_jobs.manager_id = ? AND task_job_events.event_type = ? AND task_job_events.event_at >= ?", managerID, "fail", since).
		Count(&recentFailures).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询近期失败任务失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user_stats":          userStats,
		"job_stats":           jobStats,
		"recent_failures_24h": recentFailures,
		"generated_at":        now,
	})
}

func (s *Server) managerListTaskPool(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	status := strings.TrimSpace(c.Query("status"))
	keyword := strings.TrimSpace(c.Query("keyword"))
	pg := readPagination(c, 50, 200)

	activeStatuses := []string{models.JobStatusPending, models.JobStatusLeased, models.JobStatusRunning}

	type taskPoolRow struct {
		ID           uint       `json:"id"`
		TaskType     string     `json:"task_type"`
		Status       string     `json:"status"`
		Priority     int        `json:"priority"`
		ScheduledAt  time.Time  `json:"scheduled_at"`
		CreatedAt    time.Time  `json:"created_at"`
		Attempts     int        `json:"attempts"`
		MaxAttempts  int        `json:"max_attempts"`
		LeasedByNode string     `json:"leased_by_node"`
		LeaseUntil   *time.Time `json:"lease_until"`
		UserID       uint       `json:"user_id"`
		AccountNo    string     `json:"account_no"`
		LoginID      string     `json:"login_id"`
		UserType     string     `json:"user_type"`
		Server       string     `json:"server"`
		Username     string     `json:"username"`
	}

	baseQuery := s.db.Table("task_jobs").
		Select("task_jobs.id, task_jobs.task_type, task_jobs.status, task_jobs.priority, task_jobs.scheduled_at, task_jobs.created_at, task_jobs.attempts, task_jobs.max_attempts, task_jobs.leased_by_node, task_jobs.lease_until, task_jobs.user_id, users.account_no, users.login_id, users.user_type, users.server, users.username").
		Joins("JOIN users ON users.id = task_jobs.user_id").
		Where("task_jobs.manager_id = ?", managerID)

	if status != "" {
		baseQuery = baseQuery.Where("task_jobs.status = ?", status)
	} else {
		baseQuery = baseQuery.Where("task_jobs.status IN ?", activeStatuses)
	}
	if keyword != "" {
		baseQuery = baseQuery.Where("users.account_no LIKE ?", "%"+keyword+"%")
	}

	var filteredTotal int64
	if err := baseQuery.Count(&filteredTotal).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "统计任务池失败"})
		return
	}

	var rows []taskPoolRow
	if err := baseQuery.Order("task_jobs.priority desc, task_jobs.scheduled_at asc").
		Offset(pg.Offset).Limit(pg.PageSize).Scan(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询任务池失败"})
		return
	}

	summary := gin.H{"pending": int64(0), "leased": int64(0), "running": int64(0)}
	type poolSummaryAgg struct {
		Status string `gorm:"column:status"`
		Cnt    int64  `gorm:"column:cnt"`
	}
	var poolAggs []poolSummaryAgg
	s.db.Model(&models.TaskJob{}).Select("status, COUNT(*) as cnt").Where("manager_id = ? AND status IN ?", managerID, activeStatuses).Group("status").Find(&poolAggs)
	for _, r := range poolAggs {
		summary[r.Status] = r.Cnt
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     rows,
		"summary":   summary,
		"total":     filteredTotal,
		"page":      pg.Page,
		"page_size": pg.PageSize,
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
		c.JSON(http.StatusNotFound, gin.H{"detail": "用户不存在"})
		return
	}
	config, err := s.getOrCreateTaskConfig(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "加载任务配置失败"})
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
	if strings.TrimSpace(req.ExpiresAt) == "" && req.ExtendDays == 0 && strings.TrimSpace(req.Status) == "" && strings.TrimSpace(req.ArchiveStatus) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "至少需要提供一个字段"})
		return
	}

	now := time.Now().UTC()
	var user models.User
	if err := s.db.Where("id = ? AND manager_id = ?", userID, managerID).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "用户不存在"})
		return
	}

	updates := map[string]any{"updated_at": now}
	hasExpiryUpdate := false
	if strings.TrimSpace(req.ExpiresAt) != "" {
		parsed, err := parseFlexibleDateTime(req.ExpiresAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "到期时间格式无效"})
			return
		}
		updates["expires_at"] = parsed
		hasExpiryUpdate = true
	}
	if req.ExtendDays != 0 {
		if req.ExtendDays < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "延长天数必须为正数"})
			return
		}
		newExpire := extendExpiry(user.ExpiresAt, req.ExtendDays, now)
		updates["expires_at"] = newExpire
		hasExpiryUpdate = true
	}

	if status := strings.TrimSpace(req.Status); status != "" {
		if status != models.UserStatusActive && status != models.UserStatusExpired && status != models.UserStatusDisabled {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "无效的状态值"})
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

	if archiveStatus := strings.TrimSpace(req.ArchiveStatus); archiveStatus != "" {
		if archiveStatus != "normal" && archiveStatus != "invalid" {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "无效的存档状态值，仅支持 normal 或 invalid"})
			return
		}
		updates["archive_status"] = archiveStatus
	}

	if err := s.db.Model(&models.User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "更新用户生命周期失败"})
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
		c.JSON(http.StatusNotFound, gin.H{"detail": "用户不存在"})
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
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "更新用户资产失败"})
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
			c.JSON(http.StatusBadRequest, gin.H{"detail": "任务配置包含该用户类型不允许的任务"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "更新任务配置失败"})
		return
	}
	var user models.User
	if err := s.db.Where("id = ? AND manager_id = ?", userID, managerID).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "用户不存在"})
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
	pg := readPagination(c, 50, 200)
	items, total, err := s.queryUserLogsPaginated(managerID, userID, pg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询日志失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      pg.Page,
		"page_size": pg.PageSize,
	})
}

func (s *Server) managerDeleteUserLogs(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	userID, ok := parseUintParam(c, "user_id")
	if !ok {
		return
	}
	if !s.managerOwnsUser(c, managerID, userID) {
		return
	}
	if err := s.db.Exec(
		"DELETE FROM task_job_events WHERE job_id IN (SELECT id FROM task_jobs WHERE manager_id = ? AND user_id = ?)",
		managerID, userID,
	).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "删除日志失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "logs cleared"})
}

func (s *Server) managerPatchUserSettings(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	userID, ok := parseUintParam(c, "user_id")
	if !ok {
		return
	}
	if !s.managerOwnsUser(c, managerID, userID) {
		return
	}

	var req managerPatchUserSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	if req.CanViewLogs == nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "至少需要提供一个字段"})
		return
	}

	updates := map[string]any{"updated_at": time.Now().UTC()}
	if req.CanViewLogs != nil {
		updates["can_view_logs"] = *req.CanViewLogs
	}

	if err := s.db.Model(&models.User{}).Where("id = ? AND manager_id = ?", userID, managerID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "更新用户设置失败"})
		return
	}
	s.audit(models.ActorTypeManager, managerID, "patch_user_settings", "user", userID, datatypes.JSONMap{
		"can_view_logs": req.CanViewLogs,
	}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"message": "user settings updated"})
}

func (s *Server) managerDeleteUser(c *gin.Context) {
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
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"detail": "用户不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询用户失败"})
		return
	}

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM task_job_events WHERE job_id IN (SELECT id FROM task_jobs WHERE user_id = ? AND manager_id = ?)", userID, managerID).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ? AND manager_id = ?", userID, managerID).Delete(&models.TaskJob{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.UserTaskConfig{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.UserToken{}).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ? AND manager_id = ?", userID, managerID).Delete(&models.User{}).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "删除用户失败"})
		return
	}

	s.audit(models.ActorTypeManager, managerID, "delete_user", "user", userID, datatypes.JSONMap{
		"account_no": user.AccountNo,
		"login_id":   user.LoginID,
	}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"message": "user deleted"})
}

func (s *Server) managerBatchDeleteUsers(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	var req batchUserDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	var deleted int64
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM task_job_events WHERE job_id IN (SELECT id FROM task_jobs WHERE user_id IN ? AND manager_id = ?)", req.UserIDs, managerID).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id IN ? AND manager_id = ?", req.UserIDs, managerID).Delete(&models.TaskJob{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id IN ?", req.UserIDs).Delete(&models.UserTaskConfig{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id IN ?", req.UserIDs).Delete(&models.UserToken{}).Error; err != nil {
			return err
		}
		result := tx.Where("id IN ? AND manager_id = ?", req.UserIDs, managerID).Delete(&models.User{})
		if result.Error != nil {
			return result.Error
		}
		deleted = result.RowsAffected
		return nil
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "批量删除用户失败"})
		return
	}

	s.audit(models.ActorTypeManager, managerID, "batch_delete_users", "user", 0, datatypes.JSONMap{
		"user_ids": req.UserIDs,
		"deleted":  deleted,
	}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"deleted": deleted, "requested": len(req.UserIDs)})
}

// ── Manager batch handlers ────────────────────────────

func (s *Server) managerBatchUserLifecycle(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	var req batchUserLifecycleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	if strings.TrimSpace(req.ExpiresAt) == "" && req.ExtendDays == 0 && strings.TrimSpace(req.Status) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "至少需要提供一个字段"})
		return
	}
	if req.ExtendDays < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "延长天数必须为正数"})
		return
	}
	if rawStatus := strings.TrimSpace(req.Status); rawStatus != "" {
		if rawStatus != models.UserStatusActive && rawStatus != models.UserStatusExpired && rawStatus != models.UserStatusDisabled {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "无效的状态值"})
			return
		}
	}

	var parsedExpires time.Time
	hasExpires := false
	if rawExpires := strings.TrimSpace(req.ExpiresAt); rawExpires != "" {
		parsed, err := parseFlexibleDateTime(rawExpires)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "到期时间格式无效"})
			return
		}
		parsedExpires = parsed
		hasExpires = true
	}

	now := time.Now().UTC()
	var updated int64

	err := s.db.Transaction(func(tx *gorm.DB) error {
		var users []models.User
		if err := tx.Where("id IN ? AND manager_id = ?", req.UserIDs, managerID).Find(&users).Error; err != nil {
			return err
		}
		if len(users) != len(req.UserIDs) {
			return fmt.Errorf("some user IDs not found or not owned by this manager")
		}

		// Fast path: when ExtendDays is 0, all users get the same updates → single bulk UPDATE
		if req.ExtendDays == 0 {
			updates := map[string]any{"updated_at": now}
			if hasExpires {
				updates["expires_at"] = parsedExpires
			}
			if rawStatus := strings.TrimSpace(req.Status); rawStatus != "" {
				updates["status"] = rawStatus
			} else if hasExpires {
				if parsedExpires.After(now) {
					updates["status"] = models.UserStatusActive
				} else {
					updates["status"] = models.UserStatusExpired
				}
			}
			result := tx.Model(&models.User{}).Where("id IN ? AND manager_id = ?", req.UserIDs, managerID).Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			updated = result.RowsAffected
			return nil
		}

		// Slow path: ExtendDays > 0, each user has different expires_at based on current value
		// Build batch CASE SQL to update all users in a single query
		rawStatus := strings.TrimSpace(req.Status)
		expiresCase := "CASE id "
		statusCase := "CASE id "
		for _, user := range users {
			newExpire := extendExpiry(user.ExpiresAt, req.ExtendDays, now)
			expiresCase += fmt.Sprintf("WHEN %d THEN '%s' ", user.ID, newExpire.UTC().Format(time.RFC3339))
			if rawStatus != "" {
				statusCase += fmt.Sprintf("WHEN %d THEN '%s' ", user.ID, rawStatus)
			} else if newExpire.After(now) {
				statusCase += fmt.Sprintf("WHEN %d THEN '%s' ", user.ID, models.UserStatusActive)
			} else {
				statusCase += fmt.Sprintf("WHEN %d THEN '%s' ", user.ID, models.UserStatusExpired)
			}
		}
		expiresCase += "END"
		statusCase += "END"

		result := tx.Model(&models.User{}).
			Where("id IN ? AND manager_id = ?", req.UserIDs, managerID).
			Updates(map[string]any{
				"expires_at": gorm.Expr(expiresCase),
				"status":     gorm.Expr(statusCase),
				"updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		updated = result.RowsAffected
		return nil
	})
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "not owned") {
			c.JSON(http.StatusForbidden, gin.H{"detail": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "批量更新用户生命周期失败"})
		return
	}
	s.audit(models.ActorTypeManager, managerID, "batch_user_lifecycle", "user", 0, datatypes.JSONMap{
		"user_ids":    req.UserIDs,
		"extend_days": req.ExtendDays,
		"expires_at":  req.ExpiresAt,
		"status":      req.Status,
		"updated":     updated,
	}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"updated": updated})
}

func (s *Server) managerBatchUserAssets(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	var req batchUserAssetsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	for key := range req.Assets {
		if err := taskmeta.ValidateAssetKey(key); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
			return
		}
	}

	now := time.Now().UTC()
	var updated int64

	err := s.db.Session(&gorm.Session{PrepareStmt: true}).Transaction(func(tx *gorm.DB) error {
		var users []models.User
		if err := tx.Where("id IN ? AND manager_id = ?", req.UserIDs, managerID).Find(&users).Error; err != nil {
			return err
		}
		if len(users) != len(req.UserIDs) {
			return fmt.Errorf("some user IDs not found or not owned by this manager")
		}
		for _, user := range users {
			base := deepMergeMap(taskmeta.BuildDefaultUserAssets(), map[string]any(user.Assets))
			for key, value := range req.Assets {
				base[key] = taskmeta.ParseAssetInt(value, taskmeta.ParseAssetInt(base[key], 0))
			}
			if err := tx.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
				"assets":     datatypes.JSONMap(base),
				"updated_at": now,
			}).Error; err != nil {
				return err
			}
			updated++
		}
		return nil
	})
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "not owned") {
			c.JSON(http.StatusForbidden, gin.H{"detail": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "批量更新用户资产失败"})
		return
	}
	s.audit(models.ActorTypeManager, managerID, "batch_user_assets", "user", 0, datatypes.JSONMap{
		"user_ids": req.UserIDs,
		"assets":   req.Assets,
		"updated":  updated,
	}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"updated": updated})
}

func (s *Server) managerBatchRevokeActivationCodes(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	var req batchCodeRevokeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	result := s.db.Model(&models.UserActivationCode{}).
		Where("id IN ? AND manager_id = ? AND status = ?", req.CodeIDs, managerID, models.CodeStatusUnused).
		Updates(map[string]any{"status": models.CodeStatusRevoked})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "批量撤销激活码失败"})
		return
	}
	s.audit(models.ActorTypeManager, managerID, "batch_revoke_activation_codes", "user_activation_code", 0, datatypes.JSONMap{
		"code_ids": req.CodeIDs,
		"revoked":  result.RowsAffected,
	}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"revoked": result.RowsAffected, "requested": len(req.CodeIDs)})
}

func (s *Server) managerDeleteActivationCode(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	codeID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}

	var code models.UserActivationCode
	if err := s.db.Where("id = ? AND manager_id = ?", codeID, managerID).First(&code).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"detail": "激活码不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询激活码失败"})
		return
	}
	if err := s.db.Delete(&models.UserActivationCode{}, codeID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "删除激活码失败"})
		return
	}
	s.audit(models.ActorTypeManager, managerID, "delete_activation_code", "user_activation_code", codeID, datatypes.JSONMap{
		"code":   code.Code,
		"status": code.Status,
	}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"message": "activation code deleted"})
}

func (s *Server) managerBatchDeleteActivationCodes(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	var req batchActivationCodeDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	result := s.db.Where("id IN ? AND manager_id = ? AND status IN ?", req.CodeIDs, managerID, []string{models.CodeStatusUnused, models.CodeStatusUsed}).Delete(&models.UserActivationCode{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "批量删除激活码失败"})
		return
	}
	s.audit(models.ActorTypeManager, managerID, "batch_delete_activation_codes", "user_activation_code", 0, datatypes.JSONMap{
		"code_ids": req.CodeIDs,
		"deleted":  result.RowsAffected,
	}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"deleted": result.RowsAffected, "requested": len(req.CodeIDs)})
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
			c.JSON(http.StatusNotFound, gin.H{"detail": "激活码不存在"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	rawToken, tokenExpire, err := s.issueUserToken(createdUser.ID, "register")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "签发用户令牌失败"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"account_no": createdUser.AccountNo,
		"login_id":   createdUser.LoginID,
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
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "账号不存在"})
		return
	}
	now := time.Now().UTC()
	if user.Status != models.UserStatusActive {
		c.JSON(http.StatusForbidden, gin.H{"detail": "用户账号未激活"})
		return
	}
	if user.ExpiresAt == nil || !user.ExpiresAt.After(now) {
		c.JSON(http.StatusForbidden, gin.H{"detail": "用户账号已过期"})
		return
	}
	rawToken, tokenExpire, err := s.issueUserToken(user.ID, req.DeviceInfo)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "签发用户令牌失败"})
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
		if models.NormalizeUserType(code.UserType) != models.NormalizeUserType(user.UserType) {
			return fmt.Errorf("user_type mismatch")
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
			c.JSON(http.StatusNotFound, gin.H{"detail": "激活码不存在"})
			return
		}
		if strings.Contains(err.Error(), "forbidden") {
			c.JSON(http.StatusForbidden, gin.H{"detail": "激活码不属于您的管理员"})
			return
		}
		if strings.Contains(err.Error(), "user_type mismatch") {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "激活码类型与账号类型不匹配"})
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
		c.JSON(http.StatusNotFound, gin.H{"detail": "用户不存在"})
		return
	}

	var token models.UserToken
	if err := s.db.Where("id = ? AND user_id = ?", tokenID, userID).First(&token).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "用户令牌不存在"})
		return
	}

	var managerAlias string
	var manager models.Manager
	if err := s.db.Select("alias").Where("id = ?", user.ManagerID).First(&manager).Error; err == nil {
		managerAlias = manager.Alias
	}

	c.JSON(http.StatusOK, gin.H{
		"user_id":        user.ID,
		"account_no":     user.AccountNo,
		"login_id":       user.LoginID,
		"manager_id":     user.ManagerID,
		"manager_alias":  managerAlias,
		"user_type":      models.NormalizeUserType(user.UserType),
		"status":         user.Status,
		"archive_status": user.ArchiveStatus,
		"server":         user.Server,
		"username":       user.Username,
		"expires_at":     user.ExpiresAt,
		"assets":         deepMergeMap(taskmeta.BuildDefaultUserAssets(), map[string]any(user.Assets)),
		"token_exp":      token.ExpiresAt,
		"token_created":  token.CreatedAt,
		"last_used_at":   token.LastUsedAt,
		"notify_config":  user.NotifyConfig,
		"can_view_logs":  user.CanViewLogs,
	})
}

func (s *Server) userLogout(c *gin.Context) {
	userID := getUint(c, ctxUserIDKey)
	tokenID := getUint(c, ctxUserTokenIDKey)
	now := time.Now().UTC()

	// Load token hash before revoking so we can clear the cache
	var token models.UserToken
	if err := s.db.Select("id, token_hash").Where("id = ? AND user_id = ?", tokenID, userID).First(&token).Error; err == nil && token.TokenHash != "" {
		_ = s.redisStore.ClearUserTokenCache(c.Request.Context(), token.TokenHash)
	}

	result := s.db.Model(&models.UserToken{}).
		Where("id = ? AND user_id = ? AND revoked_at IS NULL", tokenID, userID).
		Updates(map[string]any{"revoked_at": now})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "撤销令牌失败"})
		return
	}

	s.audit(models.ActorTypeUser, userID, "user_logout", "user_token", tokenID, datatypes.JSONMap{}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"message": "logout success", "revoked": result.RowsAffected})
}

func (s *Server) userPutMeProfile(c *gin.Context) {
	var req struct {
		Server       *string         `json:"server"`
		Username     *string         `json:"username"`
		NotifyConfig *map[string]any `json:"notify_config"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	userID := getUint(c, ctxUserIDKey)
	updates := map[string]any{}
	if req.Server != nil {
		s := *req.Server
		if len(s) > 128 {
			s = s[:128]
		}
		updates["server"] = s
	}
	if req.Username != nil {
		u := *req.Username
		if len(u) > 128 {
			u = u[:128]
		}
		updates["username"] = u
	}
	if req.NotifyConfig != nil {
		nc := *req.NotifyConfig
		emailEnabled, _ := nc["email_enabled"].(bool)
		email, _ := nc["email"].(string)
		if len(email) > 254 {
			email = email[:254]
		}
		if emailEnabled && email != "" {
			if _, err := mail.ParseAddress(email); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"detail": "邮箱格式不正确"})
				return
			}
		}

		wechatEnabled, _ := nc["wechat_enabled"].(bool)
		wechatMiaoCode, _ := nc["wechat_miao_code"].(string)
		wechatMiaoCode = strings.TrimSpace(wechatMiaoCode)
		if len(wechatMiaoCode) > 64 {
			wechatMiaoCode = wechatMiaoCode[:64]
		}
		if wechatMiaoCode != "" {
			for _, r := range wechatMiaoCode {
				if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
					c.JSON(http.StatusBadRequest, gin.H{"detail": "喵码只能包含字母和数字"})
					return
				}
			}
		}
		if wechatEnabled && wechatMiaoCode == "" {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "启用微信通知需要填写喵码"})
			return
		}

		updates["notify_config"] = datatypes.JSONMap{
			"email_enabled":    emailEnabled,
			"email":            email,
			"wechat_enabled":   wechatEnabled,
			"wechat_miao_code": wechatMiaoCode,
		}
	}
	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "没有可更新的字段"})
		return
	}

	if err := s.db.Model(&models.User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "更新用户信息失败"})
		return
	}

	s.audit(models.ActorTypeUser, userID, "user_update_profile", "user", userID, datatypes.JSONMap(updates), c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"message": "更新成功"})
}

func (s *Server) userGetMeAssets(c *gin.Context) {
	userID := getUint(c, ctxUserIDKey)

	var user models.User
	if err := s.db.Where("id = ?", userID).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "用户不存在"})
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
		c.JSON(http.StatusNotFound, gin.H{"detail": "用户不存在"})
		return
	}
	config, err := s.getOrCreateTaskConfig(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "加载任务配置失败"})
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
			c.JSON(http.StatusBadRequest, gin.H{"detail": "任务配置包含该用户类型不允许的任务"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "更新任务配置失败"})
		return
	}
	var user models.User
	if err := s.db.Where("id = ?", userID).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "用户不存在"})
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

	var user models.User
	if err := s.db.Select("can_view_logs").Where("id = ?", userID).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "用户不存在"})
		return
	}
	if !user.CanViewLogs {
		c.JSON(http.StatusForbidden, gin.H{"detail": "管理员未开放日志查看权限"})
		return
	}

	pg := readPagination(c, 50, 200)
	items, total, err := s.queryUserLogsPaginated(managerID, userID, pg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询日志失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      pg.Page,
		"page_size": pg.PageSize,
	})
}

// 支持阵容切换的任务列表
var lineupSupportedTasks = []string{"逢魔", "地鬼", "探索", "结界突破", "道馆", "秘闻", "御魂"}

func defaultLineupConfig() map[string]any {
	result := map[string]any{}
	for _, task := range lineupSupportedTasks {
		result[task] = map[string]any{"group": float64(0), "position": float64(0)}
	}
	return result
}

func mergeLineupWithDefaults(userConfig map[string]any) map[string]any {
	result := defaultLineupConfig()
	if userConfig == nil {
		return result
	}
	for _, task := range lineupSupportedTasks {
		if val, ok := userConfig[task]; ok {
			if valMap, ok2 := val.(map[string]any); ok2 {
				merged := result[task].(map[string]any)
				if g, ok3 := valMap["group"]; ok3 {
					merged["group"] = g
				}
				if p, ok3 := valMap["position"]; ok3 {
					merged["position"] = p
				}
				result[task] = merged
			}
		}
	}
	return result
}

func toIntLineup(v any) (int, error) {
	switch val := v.(type) {
	case float64:
		return int(val), nil
	case int:
		return val, nil
	case int64:
		return int(val), nil
	default:
		return 0, fmt.Errorf("not a number")
	}
}

func (s *Server) userGetMeLineup(c *gin.Context) {
	userID := getUint(c, ctxUserIDKey)
	var user models.User
	if err := s.db.Where("id = ?", userID).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "用户不存在"})
		return
	}
	merged := mergeLineupWithDefaults(map[string]any(user.LineupConfig))
	c.JSON(http.StatusOK, gin.H{"lineup_config": merged})
}

func (s *Server) userPutMeLineup(c *gin.Context) {
	userID := getUint(c, ctxUserIDKey)

	var req struct {
		LineupConfig map[string]any `json:"lineup_config" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	// 验证
	validTasks := map[string]bool{}
	for _, t := range lineupSupportedTasks {
		validTasks[t] = true
	}
	for key, val := range req.LineupConfig {
		if !validTasks[key] {
			c.JSON(http.StatusBadRequest, gin.H{"detail": fmt.Sprintf("不支持的任务类型: %s", key)})
			return
		}
		valMap, ok := val.(map[string]any)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"detail": fmt.Sprintf("任务 %s 的配置格式错误", key)})
			return
		}
		groupVal, gOk := valMap["group"]
		posVal, pOk := valMap["position"]
		if !gOk || !pOk {
			c.JSON(http.StatusBadRequest, gin.H{"detail": fmt.Sprintf("任务 %s 缺少 group 或 position", key)})
			return
		}
		groupNum, gErr := toIntLineup(groupVal)
		posNum, pErr := toIntLineup(posVal)
		if gErr != nil || pErr != nil || groupNum < 0 || groupNum > 7 || posNum < 0 || posNum > 7 {
			c.JSON(http.StatusBadRequest, gin.H{"detail": fmt.Sprintf("任务 %s 的 group/position 必须为 0-7 之间的整数", key)})
			return
		}
	}

	var user models.User
	if err := s.db.Where("id = ?", userID).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "用户不存在"})
		return
	}

	current := map[string]any(user.LineupConfig)
	if current == nil {
		current = map[string]any{}
	}
	for key, val := range req.LineupConfig {
		current[key] = val
	}

	if err := s.db.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"lineup_config": datatypes.JSONMap(current),
		"updated_at":    time.Now().UTC(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "更新阵容配置失败"})
		return
	}

	s.audit(models.ActorTypeUser, userID, "user_update_lineup", "user", userID, datatypes.JSONMap(req.LineupConfig), c.ClientIP())

	merged := mergeLineupWithDefaults(current)
	c.JSON(http.StatusOK, gin.H{"lineup_config": merged})
}

func (s *Server) agentLogin(c *gin.Context) {
	var req agentLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	var manager models.Manager
	if err := s.db.Where("username = ?", req.Username).First(&manager).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "账号或密码错误"})
		return
	}
	if !auth.VerifyPassword(req.Password, manager.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "账号或密码错误"})
		return
	}
	now := time.Now().UTC()
	if manager.ExpiresAt == nil || !manager.ExpiresAt.After(now) {
		c.JSON(http.StatusForbidden, gin.H{"detail": "管理员账号未激活或已过期"})
		return
	}
	if err := s.upsertAgentNode(manager.ID, req.NodeID, req.Version, now); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "更新节点信息失败"})
		return
	}
	token, err := s.tokenManager.IssueJWT(models.ActorTypeAgent, manager.ID, manager.ID, s.cfg.AgentJWTTTL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "令牌签发失败"})
		return
	}
	if err := s.redisStore.SaveAgentSession(
		c.Request.Context(),
		token,
		manager.ID,
		req.NodeID,
		s.cfg.AgentJWTTTL,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "保存Redis Agent会话失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "manager_id": manager.ID, "node_id": req.NodeID, "manager_type": manager.ManagerType})
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

	// Upsert agent node (outside main transaction)
	_ = s.upsertAgentNodeTx(s.db, managerID, req.NodeID, "", now)

	// Phase 1: Reset expired leases (outside main transaction to reduce lock scope)
	s.resetExpiredJobLeases(ctx, managerID, now)

	// Phase 2: Acquire candidates with SKIP LOCKED (short transaction)
	candidates := make([]models.TaskJob, 0, req.Limit)
	err := s.db.Transaction(func(tx *gorm.DB) error {
		query := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("task_jobs.manager_id = ? AND task_jobs.status = ? AND task_jobs.scheduled_at <= ?", managerID, models.JobStatusPending, now)
		if len(req.UserTypes) > 0 {
			query = query.Joins("JOIN users ON users.id = task_jobs.user_id").
				Where("users.user_type IN ?", req.UserTypes)
		}
		return query.Order("task_jobs.priority desc").Order("task_jobs.scheduled_at asc").Limit(req.Limit).Find(&candidates).Error
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "获取任务失败"})
		return
	}

	// Phase 3: Try Redis leases (outside DB transaction)
	type leasedCandidate struct {
		job models.TaskJob
	}
	var leased []leasedCandidate
	for _, job := range candidates {
		acquired, err := s.redisStore.AcquireJobLease(ctx, managerID, job.ID, req.NodeID, leaseTTL)
		if err != nil || !acquired {
			continue
		}
		leased = append(leased, leasedCandidate{job: job})
	}

	if len(leased) == 0 {
		c.JSON(http.StatusOK, gin.H{"jobs": []models.TaskJob{}, "lease_until": leaseUntil})
		return
	}

	// Phase 4: Update leased jobs in DB (short transaction)
	leasedJobs := make([]models.TaskJob, 0, len(leased))
	err = s.db.Transaction(func(tx *gorm.DB) error {
		for _, lc := range leased {
			job := lc.job
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

			event := models.TaskJobEvent{JobID: job.ID, EventType: "leased", Message: fmt.Sprintf("被节点 %s 获取", req.NodeID), EventAt: now}
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
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "获取任务失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"jobs": leasedJobs, "lease_until": leaseUntil})
}

// resetExpiredJobLeases resets expired leased/running jobs back to pending.
func (s *Server) resetExpiredJobLeases(ctx context.Context, managerID uint, now time.Time) {
	var expiredJobs []models.TaskJob
	if err := s.db.Where("manager_id = ? AND status IN ? AND lease_until < ?", managerID, []string{models.JobStatusLeased, models.JobStatusRunning}, now).
		Find(&expiredJobs).Error; err != nil || len(expiredJobs) == 0 {
		return
	}
	expiredIDs := make([]uint, 0, len(expiredJobs))
	for _, item := range expiredJobs {
		expiredIDs = append(expiredIDs, item.ID)
	}
	_ = s.db.Model(&models.TaskJob{}).
		Where("id IN ?", expiredIDs).
		Updates(map[string]any{"status": models.JobStatusPending, "leased_by_node": "", "lease_until": nil, "updated_at": now, "attempts": gorm.Expr("attempts + 1")}).Error
	for _, id := range expiredIDs {
		_ = s.redisStore.ClearJobLease(ctx, managerID, id)
	}
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
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "检查Redis租约所有者失败"})
		return
	}
	if !owned {
		c.JSON(http.StatusForbidden, gin.H{"detail": "Redis租约所有者不匹配"})
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
			c.JSON(http.StatusNotFound, gin.H{"detail": "任务不存在"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	if eventType == "heartbeat" || eventType == "start" {
		refreshed, leaseErr := s.redisStore.RefreshJobLease(ctx, managerID, jobID, req.NodeID, leaseTTL)
		if leaseErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "刷新Redis租约失败"})
			return
		}
		if !refreshed {
			c.JSON(http.StatusConflict, gin.H{"detail": "Redis租约刷新冲突"})
			return
		}
	}

	if eventType == "success" || eventType == "fail" {
		if leaseErr := s.redisStore.ReleaseJobLease(ctx, managerID, jobID, req.NodeID); leaseErr != nil {
			c.JSON(http.StatusOK, gin.H{"message": "ok", "lease_warning": leaseErr.Error()})
			return
		}
	}

	// Update next_time in UserTaskConfig after success or fail
	if eventType == "success" || eventType == "fail" {
		s.updateTaskNextTime(jobID, eventType, now)
		if req.Result != nil {
			s.syncAgentResult(jobID, req.Result, now)
		}
		s.triggerTaskNotification(jobID, eventType, req.Message)
	}

	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

// updateTaskNextTime updates the next_time in UserTaskConfig after job success or fail.
func (s *Server) updateTaskNextTime(jobID uint, eventType string, now time.Time) {
	var job models.TaskJob
	if err := s.db.Where("id = ?", jobID).First(&job).Error; err != nil {
		return
	}

	var cfg models.UserTaskConfig
	if err := s.db.Where("user_id = ?", job.UserID).First(&cfg).Error; err != nil {
		return
	}

	taskConfig := map[string]any(cfg.TaskConfig)
	if taskConfig == nil {
		return
	}

	rawTaskCfg, ok := taskConfig[job.TaskType]
	if !ok {
		return
	}
	taskMap, ok := rawTaskCfg.(map[string]any)
	if !ok {
		return
	}

	var newNextTime time.Time
	if eventType == "success" {
		rule := taskmeta.GetNextTimeRule(job.TaskType)
		if rule == "" || rule == "on_demand" {
			return
		}
		newNextTime = taskmeta.CalcNextTime(rule, now)
	} else if eventType == "fail" {
		failDelay := taskmeta.ParseAssetInt(taskMap["fail_delay"], 30)
		if failDelay <= 0 {
			failDelay = 30
		}
		newNextTime = now.Add(time.Duration(failDelay) * time.Minute)
	}

	if newNextTime.IsZero() {
		return
	}

	taskMap["next_time"] = newNextTime.In(taskmeta.BJLoc).Format("2006-01-02 15:04")
	taskConfig[job.TaskType] = taskMap

	_ = s.db.Model(&models.UserTaskConfig{}).
		Where("id = ?", cfg.ID).
		Updates(map[string]any{
			"task_config": datatypes.JSONMap(taskConfig),
			"updated_at":  now,
			"version":     gorm.Expr("version + 1"),
		}).Error
}

// syncAgentResult syncs the agent-reported result (assets, status, explore_progress) back to the User record.
func (s *Server) syncAgentResult(jobID uint, result map[string]any, now time.Time) {
	var job models.TaskJob
	if err := s.db.Where("id = ?", jobID).First(&job).Error; err != nil {
		return
	}

	updates := map[string]any{"updated_at": now}

	if status, ok := result["account_status"].(string); ok && status != "" {
		// 验证并映射 account_status → archive_status
		switch status {
		case "active", "normal":
			updates["archive_status"] = "normal"
		case "invalid":
			updates["archive_status"] = "invalid"
		// 其他非法值忽略，不写入 archive_status
		}
	}

	if assets, ok := result["assets"].(map[string]any); ok && len(assets) > 0 {
		updates["assets"] = datatypes.JSONMap(assets)
	}

	if progress, ok := result["explore_progress"].(map[string]any); ok && len(progress) > 0 {
		updates["explore_progress"] = datatypes.JSONMap(progress)
	}

	if len(updates) > 1 {
		_ = s.db.Model(&models.User{}).Where("id = ?", job.UserID).Updates(updates).Error
	}

	// Apply agent-reported task_next_times for on_demand tasks (e.g. 放卡)
	if taskNextTimes, ok := result["task_next_times"].(map[string]any); ok && len(taskNextTimes) > 0 {
		s.applyTaskNextTimes(job.UserID, taskNextTimes, now)
	}
}

// applyTaskNextTimes applies agent-reported next_time values to UserTaskConfig.
// Used for on_demand tasks where the executor determines the next execution time
// (e.g., 放卡 sets next_time based on card duration via OCR).
func (s *Server) applyTaskNextTimes(userID uint, taskNextTimes map[string]any, now time.Time) {
	var cfg models.UserTaskConfig
	if err := s.db.Where("user_id = ?", userID).First(&cfg).Error; err != nil {
		return
	}

	taskConfig := map[string]any(cfg.TaskConfig)
	if taskConfig == nil {
		return
	}

	changed := false
	for taskName, rawNextTime := range taskNextTimes {
		nextTimeStr, ok := rawNextTime.(string)
		if !ok || nextTimeStr == "" {
			continue
		}

		// Only allow on_demand tasks to be updated by agent
		rule := taskmeta.GetNextTimeRule(taskName)
		if rule != "on_demand" {
			continue
		}

		// Validate time format
		parsed, err := time.ParseInLocation("2006-01-02 15:04", nextTimeStr, taskmeta.BJLoc)
		if err != nil {
			continue
		}

		// Only accept future times to prevent immediate re-scheduling
		if !parsed.After(now) {
			continue
		}

		rawTaskCfg, exists := taskConfig[taskName]
		if !exists {
			continue
		}
		taskMap, ok := rawTaskCfg.(map[string]any)
		if !ok {
			continue
		}

		taskMap["next_time"] = nextTimeStr
		taskConfig[taskName] = taskMap
		changed = true
	}

	if changed {
		_ = s.db.Model(&models.UserTaskConfig{}).
			Where("id = ?", cfg.ID).
			Updates(map[string]any{
				"task_config": datatypes.JSONMap(taskConfig),
				"updated_at":  now,
				"version":     gorm.Expr("version + 1"),
			}).Error
	}
}

// notifyWorker consumes NotifyRequests from notifyCh and sends notifications.
func (s *Server) notifyWorker() {
	for req := range s.notifyCh {
		var user models.User
		if err := s.db.Select("id, account_no, username, notify_config").
			Where("id = ?", req.UserID).First(&user).Error; err != nil {
			slog.Warn("failed to load user for notification", "user_id", req.UserID, "error", err)
			continue
		}

		nc := map[string]any(user.NotifyConfig)
		if nc == nil {
			continue
		}

		req.AccountNo = user.AccountNo
		req.Username = user.Username

		wechatEnabled, _ := nc["wechat_enabled"].(bool)
		miaoCode, _ := nc["wechat_miao_code"].(string)
		if wechatEnabled && miaoCode != "" {
			text := notify.BuildNotificationText(req)
			if err := s.notifier.SendMiaoTiXing(miaoCode, text); err != nil {
				slog.Warn("wechat notification send failed", "user_id", req.UserID, "error", err)
			}
		}
	}
}

// triggerTaskNotification sends a non-blocking notification request for a completed/failed job.
func (s *Server) triggerTaskNotification(jobID uint, eventType string, message string) {
	var job models.TaskJob
	if err := s.db.Select("id, user_id, task_type").Where("id = ?", jobID).First(&job).Error; err != nil {
		return
	}

	req := notify.NotifyRequest{
		UserID:    job.UserID,
		TaskType:  job.TaskType,
		EventType: eventType,
		Message:   message,
	}

	select {
	case s.notifyCh <- req:
	default:
		// Retry once after short delay rather than dropping immediately
		go func() {
			time.Sleep(2 * time.Second)
			select {
			case s.notifyCh <- req:
			default:
				slog.Warn("notify channel still full after retry, dropping notification", "job_id", jobID)
			}
		}()
	}
}

func (s *Server) agentGetUserFullConfig(c *gin.Context) {
	userID, ok := parseUintParam(c, "user_id")
	if !ok {
		return
	}
	managerID := getUint(c, ctxManagerIDKey)

	var user models.User
	if err := s.db.Where("id = ? AND manager_id = ?", userID, managerID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"detail": "用户不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询用户失败"})
		return
	}

	userType := models.NormalizeUserType(user.UserType)

	var cfg models.UserTaskConfig
	taskConfig := map[string]any{}
	if err := s.db.Where("user_id = ?", userID).First(&cfg).Error; err == nil {
		taskConfig = map[string]any(cfg.TaskConfig)
	}
	taskConfig = taskmeta.NormalizeTaskConfigByType(taskConfig, userType)

	c.JSON(http.StatusOK, gin.H{
		"login_id":         user.LoginID,
		"user_type":        userType,
		"task_config":      taskConfig,
		"rest_config":      user.RestConfig,
		"lineup_config":    user.LineupConfig,
		"shikigami_config": user.ShikigamiConfig,
		"explore_progress": user.ExploreProgress,
	})
}

func (s *Server) agentUpdateUserGameProfile(c *gin.Context) {
	userID, ok := parseUintParam(c, "user_id")
	if !ok {
		return
	}
	managerID := getUint(c, ctxManagerIDKey)

	var req map[string]any
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	now := time.Now().UTC()
	updates := map[string]any{"updated_at": now}

	if v, ok := req["archive_status"].(string); ok && v != "" {
		updates["archive_status"] = v
	}
	if v, ok := req["server"].(string); ok {
		updates["server"] = v
	}
	if v, ok := req["username"].(string); ok {
		updates["username"] = v
	}

	if len(updates) <= 1 {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "没有可更新的字段"})
		return
	}

	result := s.db.Model(&models.User{}).Where("id = ? AND manager_id = ?", userID, managerID).Updates(updates)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "更新失败"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"detail": "用户不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

func (s *Server) agentUpdateExploreProgress(c *gin.Context) {
	userID, ok := parseUintParam(c, "user_id")
	if !ok {
		return
	}
	managerID := getUint(c, ctxManagerIDKey)

	var req struct {
		Progress map[string]any `json:"progress" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	result := s.db.Model(&models.User{}).Where("id = ? AND manager_id = ?", userID, managerID).
		Updates(map[string]any{
			"explore_progress": datatypes.JSONMap(req.Progress),
			"updated_at":       time.Now().UTC(),
		})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "更新失败"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"detail": "用户不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

func (s *Server) agentReportLogs(c *gin.Context) {
	userID, ok := parseUintParam(c, "user_id")
	if !ok {
		return
	}
	managerID := getUint(c, ctxManagerIDKey)

	var req struct {
		Logs []struct {
			Type    string `json:"type"`
			Level   string `json:"level"`
			Message string `json:"message"`
			Ts      string `json:"ts"`
		} `json:"logs"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	// Verify user belongs to this manager
	var count int64
	if err := s.db.Model(&models.User{}).Where("id = ? AND manager_id = ?", userID, managerID).Count(&count).Error; err != nil || count == 0 {
		c.JSON(http.StatusNotFound, gin.H{"detail": "用户不存在"})
		return
	}

	now := time.Now().UTC()
	if len(req.Logs) > 0 {
		entries := make([]models.AuditLog, 0, len(req.Logs))
		for _, logEntry := range req.Logs {
			entries = append(entries, models.AuditLog{
				ActorType:  "agent",
				ActorID:    managerID,
				Action:     logEntry.Type,
				TargetType: "user",
				TargetID:   userID,
				Detail:     datatypes.JSONMap{"level": logEntry.Level, "message": logEntry.Message, "ts": logEntry.Ts},
				CreatedAt:  now,
			})
		}
		if err := s.db.Create(&entries).Error; err != nil {
			slog.Error("batch audit log insert failed", "error", err)
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok", "count": len(req.Logs)})
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
	loginID, err := s.nextLoginID(tx, code.ManagerID)
	if err != nil {
		return nil, err
	}
	newExpire := extendExpiry(nil, code.DurationDays, now)
	user := models.User{
		AccountNo: accountNo,
		ManagerID: code.ManagerID,
		LoginID:   loginID,
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

func (s *Server) nextLoginID(tx *gorm.DB, managerID uint) (string, error) {
	var maxVal int64
	row := tx.Model(&models.User{}).
		Where("manager_id = ?", managerID).
		Select("COALESCE(MAX(CAST(login_id AS INTEGER)), 0)").Row()
	if row != nil {
		_ = row.Scan(&maxVal)
	}
	candidate := maxVal + 1
	for i := 0; i < 8; i++ {
		candidateStr := strconv.FormatInt(candidate, 10)
		var count int64
		if err := tx.Model(&models.User{}).
			Where("manager_id = ? AND login_id = ?", managerID, candidateStr).
			Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return candidateStr, nil
		}
		candidate++
	}
	return "", fmt.Errorf("failed to generate unique login_id")
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
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "验证用户归属失败"})
		return false
	}
	if count == 0 {
		c.JSON(http.StatusForbidden, gin.H{"detail": "该用户不在此管理员下"})
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
	// 如果归一化后的任务数与数据库中不同，说明有多余任务，写回 DB 清理脏数据
	if len(normalized) != len(cfg.TaskConfig) {
		cfg.TaskConfig = datatypes.JSONMap(normalized)
		cfg.Version = cfg.Version + 1
		cfg.UpdatedAt = time.Now().UTC()
		_ = s.db.Model(&models.UserTaskConfig{}).Where("id = ?", cfg.ID).Updates(map[string]any{
			"task_config": cfg.TaskConfig,
			"updated_at":  cfg.UpdatedAt,
			"version":     cfg.Version,
		}).Error
	} else {
		cfg.TaskConfig = datatypes.JSONMap(normalized)
	}
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

		// Snapshot old next_time values before merge
		oldNextTimes := make(map[string]string)
		for taskType, rawCfg := range base {
			if taskMap, ok := rawCfg.(map[string]any); ok {
				if nt, ok := taskMap["next_time"].(string); ok {
					oldNextTimes[taskType] = nt
				}
			}
		}

		merged := deepMergeMap(base, filteredPatch)

		// Detect next_time changes and expire stale pending tasks
		var changedTaskTypes []string
		for taskType, rawCfg := range merged {
			if taskMap, ok := rawCfg.(map[string]any); ok {
				newNT, _ := taskMap["next_time"].(string)
				if newNT != oldNextTimes[taskType] {
					changedTaskTypes = append(changedTaskTypes, taskType)
				}
			}
		}
		if len(changedTaskTypes) > 0 {
			activeStatuses := []string{models.JobStatusPending}
			var staleJobs []models.TaskJob
			if err := tx.Where("user_id = ? AND task_type IN ? AND status IN ?",
				userID, changedTaskTypes, activeStatuses).
				Find(&staleJobs).Error; err == nil && len(staleJobs) > 0 {
				staleIDs := make([]uint, 0, len(staleJobs))
				for _, job := range staleJobs {
					staleIDs = append(staleIDs, job.ID)
				}
				_ = tx.Model(&models.TaskJob{}).Where("id IN ?", staleIDs).
					Updates(map[string]any{"status": models.JobStatusFailed, "updated_at": now}).Error
				events := make([]models.TaskJobEvent, 0, len(staleIDs))
				for _, id := range staleIDs {
					events = append(events, models.TaskJobEvent{
						JobID:     id,
						EventType: "expired",
						Message:   "任务时间已变更，自动失败",
						EventAt:   now,
					})
				}
				if len(events) > 0 {
					_ = tx.Create(&events).Error
				}
			}
		}

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

func generateChineseDescription(eventType, message, errorCode, leasedByNode string) string {
	switch eventType {
	case "leased":
		if leasedByNode != "" {
			return fmt.Sprintf("被节点 %s 获取", leasedByNode)
		}
		return "已被节点获取"
	case "start":
		return "开始执行"
	case "success":
		return "执行成功"
	case "fail":
		desc := "执行失败"
		if errorCode != "" {
			desc += "：" + chineseErrorCode(errorCode)
		}
		return desc
	default:
		if message != "" {
			return message
		}
		return "-"
	}
}

func chineseErrorCode(code string) string {
	switch code {
	case "LOCAL_ACCOUNT_NOT_MAPPED":
		return "本地账号未映射"
	case "LOCAL_BATCH_FAILED":
		return "本地执行失败"
	case "LOCAL_ACCOUNT_MISSING":
		return "缺少本地账号"
	case "TASK_TYPE_INVALID":
		return "任务类型无效"
	default:
		return code
	}
}

func (s *Server) queryUserLogsPaginated(managerID, userID uint, pg paginationParams) ([]gin.H, int64, error) {
	baseQuery := s.db.Model(&models.TaskJobEvent{}).
		Joins("JOIN task_jobs ON task_jobs.id = task_job_events.job_id").
		Where("task_jobs.manager_id = ? AND task_jobs.user_id = ?", managerID, userID).
		Where("task_job_events.event_type NOT IN ?", []string{"timeout_requeued", "heartbeat", "leased"})

	var total int64
	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	type logRow struct {
		ID           uint      `gorm:"column:id"`
		JobID        uint      `gorm:"column:job_id"`
		EventType    string    `gorm:"column:event_type"`
		Message      string    `gorm:"column:message"`
		ErrorCode    string    `gorm:"column:error_code"`
		EventAt      time.Time `gorm:"column:event_at"`
		TaskType     string    `gorm:"column:task_type"`
		LeasedByNode string    `gorm:"column:leased_by_node"`
	}
	var rows []logRow
	if err := baseQuery.
		Select("task_job_events.id, task_job_events.job_id, task_job_events.event_type, task_job_events.message, task_job_events.error_code, task_job_events.event_at, task_jobs.task_type, task_jobs.leased_by_node").
		Order("task_job_events.event_at DESC").
		Offset(pg.Offset).Limit(pg.PageSize).
		Scan(&rows).Error; err != nil {
		return nil, 0, err
	}

	result := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		result = append(result, gin.H{
			"job_id":         r.JobID,
			"task_type":      r.TaskType,
			"event_type":     r.EventType,
			"message":        generateChineseDescription(r.EventType, r.Message, r.ErrorCode, r.LeasedByNode),
			"error_code":     r.ErrorCode,
			"event_at":       r.EventAt,
			"leased_by_node": r.LeasedByNode,
		})
	}
	return result, total, nil
}

func isCodeStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case models.CodeStatusUnused, models.CodeStatusUsed, models.CodeStatusRevoked:
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
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效的" + key})
		return 0, false
	}
	return uint(id), true
}

type paginationParams struct {
	Page     int
	PageSize int
	Offset   int
}

func readPagination(c *gin.Context, defaultPageSize, maxPageSize int) paginationParams {
	page := readQueryInt(c, "page", 1, 1, 100000)
	pageSize := readQueryInt(c, "page_size", defaultPageSize, 1, maxPageSize)
	return paginationParams{
		Page:     page,
		PageSize: pageSize,
		Offset:   (page - 1) * pageSize,
	}
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
	select {
	case s.auditCh <- entry:
	default:
		// Channel full, write asynchronously with bounded concurrency
		select {
		case s.auditOverflowSem <- struct{}{}:
			go func() {
				defer func() { <-s.auditOverflowSem }()
				_ = s.db.Create(&entry).Error
			}()
			slog.Warn("audit channel full, async fallback", "action", entry.Action)
		default:
			slog.Warn("audit overflow limit reached, dropping log", "action", entry.Action)
		}
	}
}

func (s *Server) auditWorker() {
	batch := make([]models.AuditLog, 0, 50)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := s.db.Create(&batch).Error; err != nil {
			slog.Error("audit batch insert failed", "error", err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case entry, ok := <-s.auditCh:
			if !ok {
				flush()
				return
			}
			batch = append(batch, entry)
			if len(batch) >= 50 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (s *Server) superListAuditLogs(c *gin.Context) {
	action := strings.TrimSpace(c.Query("action"))
	keyword := strings.TrimSpace(c.Query("keyword"))
	pg := readPagination(c, 50, 200)

	// Only return logs relevant to the super-admin perspective:
	// 1. All operations performed by super admins
	// 2. Manager redeeming renewal keys
	// 3. Manager configuring duiyi/blogger answers
	baseQuery := s.db.Model(&models.AuditLog{}).Where(
		"(actor_type = ? OR (actor_type = ? AND action IN ?))",
		models.ActorTypeSuper, models.ActorTypeManager,
		[]string{"redeem_manager_renewal_key", "set_blogger_answer", "set_duiyi_answer"},
	)

	if action != "" {
		baseQuery = baseQuery.Where("action = ?", action)
	}

	if keyword != "" {
		var superIDs []uint
		s.db.Model(&models.SuperAdmin{}).Where("username LIKE ?", "%"+keyword+"%").Pluck("id", &superIDs)
		var managerIDs []uint
		s.db.Model(&models.Manager{}).Where("username LIKE ?", "%"+keyword+"%").Pluck("id", &managerIDs)

		if len(superIDs) == 0 && len(managerIDs) == 0 {
			c.JSON(http.StatusOK, gin.H{"items": []gin.H{}, "total": 0, "page": pg.Page, "page_size": pg.PageSize})
			return
		}
		baseQuery = baseQuery.Where(
			"(actor_type = ? AND actor_id IN ?) OR (actor_type = ? AND actor_id IN ?)",
			models.ActorTypeSuper, superIDs,
			models.ActorTypeManager, managerIDs,
		)
	}

	var total int64
	if err := baseQuery.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "统计审计日志失败"})
		return
	}

	var logs []models.AuditLog
	if err := baseQuery.Order("id desc").Offset(pg.Offset).Limit(pg.PageSize).Find(&logs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询审计日志失败"})
		return
	}

	// Batch-resolve actor IDs to usernames
	superIDSet := map[uint]struct{}{}
	managerIDSet := map[uint]struct{}{}
	for _, log := range logs {
		switch log.ActorType {
		case models.ActorTypeSuper:
			superIDSet[log.ActorID] = struct{}{}
		case models.ActorTypeManager:
			managerIDSet[log.ActorID] = struct{}{}
		}
	}

	superNameMap := map[uint]string{}
	if len(superIDSet) > 0 {
		ids := uintSetKeys(superIDSet)
		var admins []models.SuperAdmin
		if err := s.db.Where("id IN ?", ids).Find(&admins).Error; err == nil {
			for _, a := range admins {
				superNameMap[a.ID] = a.Username
			}
		}
	}

	managerNameMap := map[uint]string{}
	if len(managerIDSet) > 0 {
		ids := uintSetKeys(managerIDSet)
		var managers []models.Manager
		if err := s.db.Where("id IN ?", ids).Find(&managers).Error; err == nil {
			for _, m := range managers {
				managerNameMap[m.ID] = m.Username
			}
		}
	}

	items := make([]gin.H, 0, len(logs))
	for _, log := range logs {
		actorName := ""
		switch log.ActorType {
		case models.ActorTypeSuper:
			actorName = superNameMap[log.ActorID]
		case models.ActorTypeManager:
			actorName = managerNameMap[log.ActorID]
		}
		if actorName == "" {
			actorName = fmt.Sprintf("[已删除#%d]", log.ActorID)
		}
		items = append(items, gin.H{
			"id":          log.ID,
			"actor_type":  log.ActorType,
			"actor_id":    log.ActorID,
			"actor_name":  actorName,
			"action":      log.Action,
			"target_type": log.TargetType,
			"target_id":   log.TargetID,
			"detail":      log.Detail,
			"ip":          log.IP,
			"created_at":  log.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      pg.Page,
		"page_size": pg.PageSize,
	})
}

func uintSetKeys(m map[uint]struct{}) []uint {
	keys := make([]uint, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ── Duiyi Answer Config ─────────────────────────────────

var duiyiValidWindows = map[string]bool{
	"10:00": true, "12:00": true, "14:00": true, "16:00": true,
	"18:00": true, "20:00": true, "22:00": true,
}

var duiyiWindowList = []string{"10:00", "12:00", "14:00", "16:00", "18:00", "20:00", "22:00"}

// currentDuiyiWindowStr returns the current duiyi window string (e.g. "14:00")
// for the given UTC time, or "" if outside the 10:00-22:00 range.
func currentDuiyiWindowStr(now time.Time) string {
	bjLoc := time.FixedZone("Asia/Shanghai", 8*60*60)
	bjHour := now.In(bjLoc).Hour()
	if bjHour < 10 {
		return ""
	}
	result := ""
	for _, w := range duiyiWindowList {
		h, _ := strconv.Atoi(strings.Split(w, ":")[0])
		if bjHour >= h {
			result = w
		}
	}
	return result
}

func (s *Server) managerGetDuiyiAnswers(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	bjLoc := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Now().UTC()
	todayBJ := now.In(bjLoc).Format("2006-01-02")
	currentWindow := currentDuiyiWindowStr(now)

	var cfg models.DuiyiAnswerConfig
	err := s.db.Where("manager_id = ?", managerID).First(&cfg).Error

	answers := make(map[string]any, 7)
	for _, w := range duiyiWindowList {
		answers[w] = nil
	}

	var dateOut any = nil
	if err == nil && cfg.Date == todayBJ {
		dateOut = cfg.Date
		stored := map[string]any(cfg.Answers)
		for _, w := range duiyiWindowList {
			if v, ok := stored[w]; ok {
				answers[w] = v
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"date":           dateOut,
			"answers":        answers,
			"current_window": currentWindow,
		},
	})
}

func (s *Server) managerPutDuiyiAnswers(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)

	var req putSingleWindowAnswerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求格式错误"})
		return
	}

	now := time.Now().UTC()
	currentWindow := currentDuiyiWindowStr(now)
	if currentWindow == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "当前不在对弈竞猜时间范围内 (10:00-22:00)"})
		return
	}
	if req.Window != currentWindow {
		c.JSON(http.StatusBadRequest, gin.H{"detail": fmt.Sprintf("只能配置当前窗口 %s 的答案", currentWindow)})
		return
	}
	if req.Answer != "左" && req.Answer != "右" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": fmt.Sprintf("无效的答案值: %s（仅支持 左/右）", req.Answer)})
		return
	}

	bjLoc := time.FixedZone("Asia/Shanghai", 8*60*60)
	todayBJ := now.In(bjLoc).Format("2006-01-02")

	// Read existing config and merge the current window answer
	var existing models.DuiyiAnswerConfig
	err := s.db.Where("manager_id = ?", managerID).First(&existing).Error

	answers := make(map[string]any, 7)
	for _, w := range duiyiWindowList {
		answers[w] = nil
	}
	if err == nil && existing.Date == todayBJ {
		stored := map[string]any(existing.Answers)
		for _, w := range duiyiWindowList {
			if v, ok := stored[w]; ok {
				answers[w] = v
			}
		}
	}
	answers[req.Window] = req.Answer

	if err != nil && errors.Is(err, gorm.ErrRecordNotFound) {
		cfg := models.DuiyiAnswerConfig{
			ManagerID: managerID,
			Date:      todayBJ,
			Answers:   datatypes.JSONMap(answers),
			UpdatedAt: now,
		}
		if err := s.db.Create(&cfg).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "保存失败"})
			return
		}
	} else {
		if err := s.db.Model(&models.DuiyiAnswerConfig{}).
			Where("manager_id = ?", managerID).
			Updates(map[string]any{
				"date":       todayBJ,
				"answers":    datatypes.JSONMap(answers),
				"updated_at": now,
			}).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "保存失败"})
			return
		}
	}

	s.audit(models.ActorTypeManager, managerID, "set_duiyi_answer", "duiyi_answer_config", managerID,
		datatypes.JSONMap{"window": req.Window, "answer": req.Answer}, c.ClientIP())

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"date":           todayBJ,
			"answers":        answers,
			"current_window": currentWindow,
		},
	})
}

// ── Blogger Management (Super Admin) ─────────────────────

func (s *Server) superCreateBlogger(c *gin.Context) {
	var req createBloggerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求格式错误"})
		return
	}

	now := time.Now().UTC()
	blogger := models.Blogger{
		Name:      strings.TrimSpace(req.Name),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.db.Create(&blogger).Error; err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "UNIQUE") {
			c.JSON(http.StatusConflict, gin.H{"detail": "博主名称已存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "创建博主失败"})
		return
	}

	actorID := getUint(c, ctxActorIDKey)
	s.audit(models.ActorTypeSuper, actorID, "create_blogger", "blogger", blogger.ID,
		datatypes.JSONMap{"name": blogger.Name}, c.ClientIP())

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"id":         blogger.ID,
			"name":       blogger.Name,
			"created_at": blogger.CreatedAt,
		},
	})
}

func (s *Server) superListBloggers(c *gin.Context) {
	var bloggers []models.Blogger
	if err := s.db.Order("id asc").Find(&bloggers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询博主失败"})
		return
	}

	items := make([]gin.H, 0, len(bloggers))
	for _, b := range bloggers {
		items = append(items, gin.H{
			"id":         b.ID,
			"name":       b.Name,
			"created_at": b.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

func (s *Server) superDeleteBlogger(c *gin.Context) {
	bloggerID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效的博主ID"})
		return
	}

	var blogger models.Blogger
	if err := s.db.Where("id = ?", bloggerID).First(&blogger).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "博主不存在"})
		return
	}

	// Clean up: reset users referencing this blogger
	s.db.Model(&models.User{}).
		Where("duiyi_blogger_id = ?", bloggerID).
		Updates(map[string]any{
			"duiyi_answer_source": "manager",
			"duiyi_blogger_id":    nil,
		})

	// Delete blogger answer configs
	s.db.Where("blogger_id = ?", bloggerID).Delete(&models.BloggerAnswerConfig{})

	// Delete the blogger
	if err := s.db.Delete(&blogger).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "删除博主失败"})
		return
	}

	actorID := getUint(c, ctxActorIDKey)
	s.audit(models.ActorTypeSuper, actorID, "delete_blogger", "blogger", uint(bloggerID),
		datatypes.JSONMap{"name": blogger.Name}, c.ClientIP())

	c.JSON(http.StatusOK, gin.H{"data": "ok"})
}

// ── Blogger Answers (Manager) ────────────────────────────

func (s *Server) managerListBloggers(c *gin.Context) {
	var bloggers []models.Blogger
	if err := s.db.Order("id asc").Find(&bloggers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询博主失败"})
		return
	}

	bjLoc := time.FixedZone("Asia/Shanghai", 8*60*60)
	todayBJ := time.Now().UTC().In(bjLoc).Format("2006-01-02")

	// Batch load today's blogger answer configs
	bloggerIDs := make([]uint, 0, len(bloggers))
	for _, b := range bloggers {
		bloggerIDs = append(bloggerIDs, b.ID)
	}
	hasTodayMap := make(map[uint]bool, len(bloggers))
	if len(bloggerIDs) > 0 {
		var configs []models.BloggerAnswerConfig
		s.db.Where("blogger_id IN ? AND date = ?", bloggerIDs, todayBJ).Find(&configs)
		for _, cfg := range configs {
			hasTodayMap[cfg.BloggerID] = true
		}
	}

	items := make([]gin.H, 0, len(bloggers))
	for _, b := range bloggers {
		items = append(items, gin.H{
			"id":                b.ID,
			"name":              b.Name,
			"has_today_answers": hasTodayMap[b.ID],
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

func (s *Server) managerGetBloggerAnswers(c *gin.Context) {
	bloggerID, err := strconv.ParseUint(c.Param("blogger_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效的博主ID"})
		return
	}

	var blogger models.Blogger
	if err := s.db.Where("id = ?", bloggerID).First(&blogger).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "博主不存在"})
		return
	}

	bjLoc := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Now().UTC()
	todayBJ := now.In(bjLoc).Format("2006-01-02")
	currentWindow := currentDuiyiWindowStr(now)

	answers := make(map[string]any, 7)
	for _, w := range duiyiWindowList {
		answers[w] = nil
	}

	var cfg models.BloggerAnswerConfig
	if err := s.db.Where("blogger_id = ? AND date = ?", bloggerID, todayBJ).First(&cfg).Error; err == nil {
		stored := map[string]any(cfg.Answers)
		for _, w := range duiyiWindowList {
			if v, ok := stored[w]; ok {
				answers[w] = v
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"blogger_id":     bloggerID,
			"blogger_name":   blogger.Name,
			"date":           todayBJ,
			"answers":        answers,
			"current_window": currentWindow,
		},
	})
}

func (s *Server) managerPutBloggerAnswer(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	bloggerID, err := strconv.ParseUint(c.Param("blogger_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效的博主ID"})
		return
	}

	var req putSingleWindowAnswerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求格式错误"})
		return
	}

	now := time.Now().UTC()
	currentWindow := currentDuiyiWindowStr(now)
	if currentWindow == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "当前不在对弈竞猜时间范围内 (10:00-22:00)"})
		return
	}
	if req.Window != currentWindow {
		c.JSON(http.StatusBadRequest, gin.H{"detail": fmt.Sprintf("只能配置当前窗口 %s 的答案", currentWindow)})
		return
	}
	if req.Answer != "左" && req.Answer != "右" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": fmt.Sprintf("无效的答案值: %s（仅支持 左/右）", req.Answer)})
		return
	}

	var blogger models.Blogger
	if err := s.db.Where("id = ?", bloggerID).First(&blogger).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "博主不存在"})
		return
	}

	bjLoc := time.FixedZone("Asia/Shanghai", 8*60*60)
	todayBJ := now.In(bjLoc).Format("2006-01-02")

	// Read existing config and merge
	var existing models.BloggerAnswerConfig
	dbErr := s.db.Where("blogger_id = ? AND date = ?", bloggerID, todayBJ).First(&existing).Error

	answers := make(map[string]any, 7)
	for _, w := range duiyiWindowList {
		answers[w] = nil
	}
	if dbErr == nil {
		stored := map[string]any(existing.Answers)
		for _, w := range duiyiWindowList {
			if v, ok := stored[w]; ok {
				answers[w] = v
			}
		}
	}
	answers[req.Window] = req.Answer

	if dbErr != nil && errors.Is(dbErr, gorm.ErrRecordNotFound) {
		cfg := models.BloggerAnswerConfig{
			BloggerID: uint(bloggerID),
			Date:      todayBJ,
			Answers:   datatypes.JSONMap(answers),
			UpdatedBy: managerID,
			UpdatedAt: now,
		}
		if err := s.db.Create(&cfg).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "保存失败"})
			return
		}
	} else {
		if err := s.db.Model(&models.BloggerAnswerConfig{}).
			Where("blogger_id = ? AND date = ?", bloggerID, todayBJ).
			Updates(map[string]any{
				"answers":    datatypes.JSONMap(answers),
				"updated_by": managerID,
				"updated_at": now,
			}).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "保存失败"})
			return
		}
	}

	s.audit(models.ActorTypeManager, managerID, "set_blogger_answer", "blogger_answer_config", uint(bloggerID),
		datatypes.JSONMap{
			"blogger_id":   bloggerID,
			"blogger_name": blogger.Name,
			"window":       req.Window,
			"answer":       req.Answer,
		}, c.ClientIP())

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"date":           todayBJ,
			"answers":        answers,
			"current_window": currentWindow,
		},
	})
}

// ── User Duiyi Answer Source ─────────────────────────────

func (s *Server) userGetDuiyiAnswerSources(c *gin.Context) {
	userID := getUint(c, ctxUserIDKey)

	var user models.User
	if err := s.db.Where("id = ?", userID).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "用户不存在"})
		return
	}

	var bloggers []models.Blogger
	s.db.Order("id asc").Find(&bloggers)

	bloggerItems := make([]gin.H, 0, len(bloggers))
	for _, b := range bloggers {
		bloggerItems = append(bloggerItems, gin.H{
			"id":   b.ID,
			"name": b.Name,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"current_source":     user.DuiyiAnswerSource,
			"current_blogger_id": user.DuiyiBloggerID,
			"bloggers":           bloggerItems,
		},
	})
}

func (s *Server) userPutDuiyiAnswerSource(c *gin.Context) {
	userID := getUint(c, ctxUserIDKey)

	var req putDuiyiAnswerSourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求格式错误"})
		return
	}

	updates := map[string]any{
		"duiyi_answer_source": req.Source,
		"updated_at":          time.Now().UTC(),
	}

	if req.Source == "blogger" {
		if req.BloggerID == nil {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "选择博主答案时必须指定博主"})
			return
		}
		var blogger models.Blogger
		if err := s.db.Where("id = ?", *req.BloggerID).First(&blogger).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "指定的博主不存在"})
			return
		}
		updates["duiyi_blogger_id"] = *req.BloggerID
	} else {
		updates["duiyi_blogger_id"] = nil
	}

	if err := s.db.Model(&models.User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "保存失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"source":     req.Source,
			"blogger_id": req.BloggerID,
		},
	})
}

// ── Friend system handlers ───────────────────────────────

// requireJingzhiUser loads the user from DB and checks user_type == jingzhi.
// Returns the user on success, or writes an error response and returns nil.
func (s *Server) requireJingzhiUser(c *gin.Context) *models.User {
	userID := getUint(c, ctxUserIDKey)
	var user models.User
	if err := s.db.Where("id = ?", userID).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "用户不存在"})
		return nil
	}
	if user.UserType != models.UserTypeJingzhi {
		c.JSON(http.StatusForbidden, gin.H{"detail": "仅精致日常用户可使用此功能"})
		return nil
	}
	return &user
}

func (s *Server) userListFriends(c *gin.Context) {
	user := s.requireJingzhiUser(c)
	if user == nil {
		return
	}

	var friendships []models.Friendship
	if err := s.db.Where(
		"(user_id = ? OR friend_id = ?) AND status = ?",
		user.ID, user.ID, models.FriendshipStatusAccepted,
	).Find(&friendships).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "获取好友列表失败"})
		return
	}

	// Collect friend user IDs
	friendIDs := make([]uint, 0, len(friendships))
	for _, f := range friendships {
		if f.UserID == user.ID {
			friendIDs = append(friendIDs, f.FriendID)
		} else {
			friendIDs = append(friendIDs, f.UserID)
		}
	}

	if len(friendIDs) == 0 {
		c.JSON(http.StatusOK, gin.H{"data": []any{}})
		return
	}

	var friends []models.User
	if err := s.db.Select("id, account_no, login_id, username, server, user_type, status").
		Where("id IN ?", friendIDs).Find(&friends).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "获取好友信息失败"})
		return
	}

	// Build friendship ID map for deletion reference
	friendshipMap := make(map[uint]uint) // friend_user_id -> friendship_id
	for _, f := range friendships {
		if f.UserID == user.ID {
			friendshipMap[f.FriendID] = f.ID
		} else {
			friendshipMap[f.UserID] = f.ID
		}
	}

	result := make([]gin.H, 0, len(friends))
	for _, f := range friends {
		result = append(result, gin.H{
			"friendship_id": friendshipMap[f.ID],
			"user_id":       f.ID,
			"account_no":    f.AccountNo,
			"login_id":      f.LoginID,
			"username":      f.Username,
			"server":        f.Server,
			"status":        f.Status,
		})
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (s *Server) userListFriendRequests(c *gin.Context) {
	user := s.requireJingzhiUser(c)
	if user == nil {
		return
	}

	var requests []models.Friendship
	if err := s.db.Where(
		"friend_id = ? AND status = ?",
		user.ID, models.FriendshipStatusPending,
	).Order("created_at desc").Find(&requests).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "获取好友请求失败"})
		return
	}

	if len(requests) == 0 {
		c.JSON(http.StatusOK, gin.H{"data": []any{}})
		return
	}

	// Load requester info
	requesterIDs := make([]uint, 0, len(requests))
	for _, r := range requests {
		requesterIDs = append(requesterIDs, r.UserID)
	}
	var requesters []models.User
	if err := s.db.Select("id, account_no, login_id, username, server").
		Where("id IN ?", requesterIDs).Find(&requesters).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "获取请求方信息失败"})
		return
	}
	requesterMap := make(map[uint]models.User)
	for _, u := range requesters {
		requesterMap[u.ID] = u
	}

	result := make([]gin.H, 0, len(requests))
	for _, r := range requests {
		u := requesterMap[r.UserID]
		result = append(result, gin.H{
			"id":         r.ID,
			"user_id":    r.UserID,
			"account_no": u.AccountNo,
			"login_id":   u.LoginID,
			"username":   u.Username,
			"server":     u.Server,
			"created_at": r.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (s *Server) userSendFriendRequest(c *gin.Context) {
	user := s.requireJingzhiUser(c)
	if user == nil {
		return
	}

	var req userFriendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	// Find the friend by username within same manager
	var friendList []models.User
	if err := s.db.Where("username = ? AND manager_id = ?", req.FriendUsername, user.ManagerID).Find(&friendList).Error; err != nil || len(friendList) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"detail": "用户名不存在"})
		return
	}
	if len(friendList) > 1 {
		c.JSON(http.StatusConflict, gin.H{"detail": "存在多个同名用户，请联系管理员确认账号"})
		return
	}
	friend := friendList[0]

	// Validation
	if friend.ID == user.ID {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "不能添加自己为好友"})
		return
	}
	if friend.UserType != models.UserTypeJingzhi {
		c.JSON(http.StatusForbidden, gin.H{"detail": "只能添加精致日常用户为好友"})
		return
	}

	// Check for existing friendship/pending request (in both directions)
	var existing models.Friendship
	err := s.db.Where(
		"((user_id = ? AND friend_id = ?) OR (user_id = ? AND friend_id = ?)) AND status IN ?",
		user.ID, friend.ID, friend.ID, user.ID,
		[]string{models.FriendshipStatusPending, models.FriendshipStatusAccepted},
	).First(&existing).Error
	if err == nil {
		if existing.Status == models.FriendshipStatusAccepted {
			c.JSON(http.StatusConflict, gin.H{"detail": "已经是好友"})
		} else {
			c.JSON(http.StatusConflict, gin.H{"detail": "已有待处理的好友请求"})
		}
		return
	}

	now := time.Now().UTC()
	friendship := models.Friendship{
		ManagerID: user.ManagerID,
		UserID:    user.ID,
		FriendID:  friend.ID,
		Status:    models.FriendshipStatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.db.Create(&friendship).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "发送好友请求失败"})
		return
	}

	s.audit(models.ActorTypeUser, user.ID, "friend_request_send", "friendship", friendship.ID, datatypes.JSONMap{"friend_id": friend.ID}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": friendship.ID, "status": friendship.Status}})
}

func (s *Server) userAcceptFriendRequest(c *gin.Context) {
	user := s.requireJingzhiUser(c)
	if user == nil {
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效的ID"})
		return
	}

	var friendship models.Friendship
	if err := s.db.Where("id = ? AND friend_id = ? AND status = ?", id, user.ID, models.FriendshipStatusPending).First(&friendship).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "好友请求不存在或已处理"})
		return
	}

	now := time.Now().UTC()
	if err := s.db.Model(&friendship).Updates(map[string]any{
		"status":     models.FriendshipStatusAccepted,
		"updated_at": now,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "接受好友请求失败"})
		return
	}

	s.audit(models.ActorTypeUser, user.ID, "friend_request_accept", "friendship", friendship.ID, datatypes.JSONMap{"user_id": friendship.UserID}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": friendship.ID, "status": models.FriendshipStatusAccepted}})
}

func (s *Server) userRejectFriendRequest(c *gin.Context) {
	user := s.requireJingzhiUser(c)
	if user == nil {
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效的ID"})
		return
	}

	var friendship models.Friendship
	if err := s.db.Where("id = ? AND friend_id = ? AND status = ?", id, user.ID, models.FriendshipStatusPending).First(&friendship).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "好友请求不存在或已处理"})
		return
	}

	now := time.Now().UTC()
	if err := s.db.Model(&friendship).Updates(map[string]any{
		"status":     models.FriendshipStatusRejected,
		"updated_at": now,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "拒绝好友请求失败"})
		return
	}

	s.audit(models.ActorTypeUser, user.ID, "friend_request_reject", "friendship", friendship.ID, datatypes.JSONMap{"user_id": friendship.UserID}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": friendship.ID, "status": models.FriendshipStatusRejected}})
}

func (s *Server) userDeleteFriend(c *gin.Context) {
	user := s.requireJingzhiUser(c)
	if user == nil {
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效的ID"})
		return
	}

	var friendship models.Friendship
	if err := s.db.Where(
		"id = ? AND (user_id = ? OR friend_id = ?) AND status = ?",
		id, user.ID, user.ID, models.FriendshipStatusAccepted,
	).First(&friendship).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "好友关系不存在"})
		return
	}

	if err := s.db.Delete(&friendship).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "删除好友失败"})
		return
	}

	friendID := friendship.FriendID
	if friendID == user.ID {
		friendID = friendship.UserID
	}
	s.audit(models.ActorTypeUser, user.ID, "friend_delete", "friendship", friendship.ID, datatypes.JSONMap{"friend_id": friendID}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"deleted": true}})
}

func (s *Server) userListFriendCandidates(c *gin.Context) {
	user := s.requireJingzhiUser(c)
	if user == nil {
		return
	}

	// Get already-connected user IDs (accepted or pending)
	var existingFriendships []models.Friendship
	s.db.Where(
		"(user_id = ? OR friend_id = ?) AND status IN ?",
		user.ID, user.ID,
		[]string{models.FriendshipStatusPending, models.FriendshipStatusAccepted},
	).Find(&existingFriendships)

	excludeIDs := map[uint]bool{user.ID: true}
	for _, f := range existingFriendships {
		excludeIDs[f.UserID] = true
		excludeIDs[f.FriendID] = true
	}

	excludeList := make([]uint, 0, len(excludeIDs))
	for id := range excludeIDs {
		excludeList = append(excludeList, id)
	}

	var candidates []models.User
	if err := s.db.Select("id, account_no, login_id, username, server").
		Where("manager_id = ? AND user_type = ? AND status = ? AND id NOT IN ?",
			user.ManagerID, models.UserTypeJingzhi, models.UserStatusActive, excludeList,
		).Find(&candidates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "获取候选好友列表失败"})
		return
	}

	result := make([]gin.H, 0, len(candidates))
	for _, u := range candidates {
		result = append(result, gin.H{
			"user_id":    u.ID,
			"account_no": u.AccountNo,
			"login_id":   u.LoginID,
			"username":   u.Username,
			"server":     u.Server,
		})
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// ── Team Yuhun handlers ───────────────────────────────

func (s *Server) userSendTeamYuhunRequest(c *gin.Context) {
	user := s.requireJingzhiUser(c)
	if user == nil {
		return
	}

	var req userTeamYuhunCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	// Parse scheduled_at
	scheduledAt, err := time.Parse(time.RFC3339, req.ScheduledAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "预约时间格式无效，使用 RFC3339 格式"})
		return
	}
	if !scheduledAt.After(time.Now().UTC()) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "预约时间必须在未来"})
		return
	}

	// Check friend exists and is a friend
	var friendship models.Friendship
	if err := s.db.Where(
		"((user_id = ? AND friend_id = ?) OR (user_id = ? AND friend_id = ?)) AND status = ?",
		user.ID, req.FriendID, req.FriendID, user.ID, models.FriendshipStatusAccepted,
	).First(&friendship).Error; err != nil {
		c.JSON(http.StatusForbidden, gin.H{"detail": "对方不是您的好友"})
		return
	}

	// Check for existing pending request between these two users
	var existingCount int64
	s.db.Model(&models.TeamYuhunRequest{}).Where(
		"((requester_id = ? AND receiver_id = ?) OR (requester_id = ? AND receiver_id = ?)) AND status IN ?",
		user.ID, req.FriendID, req.FriendID, user.ID,
		[]string{models.TeamYuhunStatusPending, models.TeamYuhunStatusAccepted},
	).Count(&existingCount)
	if existingCount > 0 {
		c.JSON(http.StatusConflict, gin.H{"detail": "已有待处理或已接受的组队请求"})
		return
	}

	// Check for time slot conflicts within the same manager (±30 minutes)
	var timeConflict int64
	s.db.Model(&models.TeamYuhunRequest{}).Where(
		"manager_id = ? AND status IN ? AND ABS(EXTRACT(EPOCH FROM (scheduled_at - ?::timestamptz))) < 1800",
		user.ManagerID,
		[]string{models.TeamYuhunStatusPending, models.TeamYuhunStatusAccepted},
		scheduledAt.Format(time.RFC3339),
	).Count(&timeConflict)
	if timeConflict > 0 {
		c.JSON(http.StatusConflict, gin.H{"detail": "该时间段已有其他用户预约（±30分钟内），请选择其他时间"})
		return
	}

	now := time.Now().UTC()
	teamReq := models.TeamYuhunRequest{
		ManagerID:       user.ManagerID,
		RequesterID:     user.ID,
		ReceiverID:      req.FriendID,
		ScheduledAt:     scheduledAt.UTC(),
		Status:          models.TeamYuhunStatusPending,
		RequesterRole:   req.Role,
		RequesterLineup: datatypes.JSONMap(req.Lineup),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.db.Create(&teamReq).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "发送组队请求失败"})
		return
	}

	s.audit(models.ActorTypeUser, user.ID, "team_yuhun_request_send", "team_yuhun_request", teamReq.ID,
		datatypes.JSONMap{"receiver_id": req.FriendID, "role": req.Role, "scheduled_at": req.ScheduledAt}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": teamReq.ID, "status": teamReq.Status}})
}

func (s *Server) userListTeamYuhunRequests(c *gin.Context) {
	user := s.requireJingzhiUser(c)
	if user == nil {
		return
	}

	var requests []models.TeamYuhunRequest
	if err := s.db.Where(
		"requester_id = ? OR receiver_id = ?", user.ID, user.ID,
	).Order("created_at desc").Find(&requests).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "获取组队请求列表失败"})
		return
	}

	// Collect all related user IDs
	userIDSet := make(map[uint]bool)
	for _, r := range requests {
		userIDSet[r.RequesterID] = true
		userIDSet[r.ReceiverID] = true
	}
	userIDs := make([]uint, 0, len(userIDSet))
	for id := range userIDSet {
		userIDs = append(userIDs, id)
	}

	userMap := make(map[uint]models.User)
	if len(userIDs) > 0 {
		var users []models.User
		s.db.Select("id, account_no, login_id, username, server").Where("id IN ?", userIDs).Find(&users)
		for _, u := range users {
			userMap[u.ID] = u
		}
	}

	result := make([]gin.H, 0, len(requests))
	for _, r := range requests {
		requester := userMap[r.RequesterID]
		receiver := userMap[r.ReceiverID]
		direction := "sent"
		if r.ReceiverID == user.ID {
			direction = "received"
		}
		result = append(result, gin.H{
			"id":           r.ID,
			"direction":    direction,
			"status":       r.Status,
			"scheduled_at": r.ScheduledAt,
			"requester": gin.H{
				"user_id":    r.RequesterID,
				"account_no": requester.AccountNo,
				"login_id":   requester.LoginID,
				"username":   requester.Username,
				"server":     requester.Server,
				"role":       r.RequesterRole,
				"lineup":     r.RequesterLineup,
			},
			"receiver": gin.H{
				"user_id":    r.ReceiverID,
				"account_no": receiver.AccountNo,
				"login_id":   receiver.LoginID,
				"username":   receiver.Username,
				"server":     receiver.Server,
				"role":       r.ReceiverRole,
				"lineup":     r.ReceiverLineup,
			},
			"created_at": r.CreatedAt,
			"updated_at": r.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (s *Server) userListTeamYuhunBookedSlots(c *gin.Context) {
	user := s.requireJingzhiUser(c)
	if user == nil {
		return
	}

	// 返回同一 manager 下所有 pending/accepted 请求的预约时间（排除当前用户自己的请求）
	var requests []models.TeamYuhunRequest
	if err := s.db.Select("scheduled_at").Where(
		"manager_id = ? AND status IN ? AND requester_id != ? AND receiver_id != ?",
		user.ManagerID,
		[]string{models.TeamYuhunStatusPending, models.TeamYuhunStatusAccepted},
		user.ID, user.ID,
	).Find(&requests).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "获取已预约时间失败"})
		return
	}

	slots := make([]string, 0, len(requests))
	for _, r := range requests {
		slots = append(slots, r.ScheduledAt.UTC().Format(time.RFC3339))
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"booked_slots": slots}})
}

func (s *Server) userAcceptTeamYuhunRequest(c *gin.Context) {
	user := s.requireJingzhiUser(c)
	if user == nil {
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效的ID"})
		return
	}

	var req userTeamYuhunAcceptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	var teamReq models.TeamYuhunRequest
	if err := s.db.Where("id = ? AND receiver_id = ? AND status = ?", id, user.ID, models.TeamYuhunStatusPending).First(&teamReq).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "组队请求不存在或已处理"})
		return
	}

	// Roles must be different
	if req.Role == teamReq.RequesterRole {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "角色不能与发起方相同，需要一个司机一个打手"})
		return
	}

	now := time.Now().UTC()
	if err := s.db.Model(&teamReq).Updates(map[string]any{
		"status":          models.TeamYuhunStatusAccepted,
		"receiver_role":   req.Role,
		"receiver_lineup": datatypes.JSONMap(req.Lineup),
		"updated_at":      now,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "接受组队请求失败"})
		return
	}

	s.audit(models.ActorTypeUser, user.ID, "team_yuhun_request_accept", "team_yuhun_request", teamReq.ID,
		datatypes.JSONMap{"role": req.Role}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": teamReq.ID, "status": models.TeamYuhunStatusAccepted}})
}

func (s *Server) userRejectTeamYuhunRequest(c *gin.Context) {
	user := s.requireJingzhiUser(c)
	if user == nil {
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效的ID"})
		return
	}

	var teamReq models.TeamYuhunRequest
	if err := s.db.Where("id = ? AND receiver_id = ? AND status = ?", id, user.ID, models.TeamYuhunStatusPending).First(&teamReq).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "组队请求不存在或已处理"})
		return
	}

	now := time.Now().UTC()
	if err := s.db.Model(&teamReq).Updates(map[string]any{
		"status":     models.TeamYuhunStatusRejected,
		"updated_at": now,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "拒绝组队请求失败"})
		return
	}

	s.audit(models.ActorTypeUser, user.ID, "team_yuhun_request_reject", "team_yuhun_request", teamReq.ID, datatypes.JSONMap{}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": teamReq.ID, "status": models.TeamYuhunStatusRejected}})
}

func (s *Server) userCancelTeamYuhunRequest(c *gin.Context) {
	user := s.requireJingzhiUser(c)
	if user == nil {
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效的ID"})
		return
	}

	var teamReq models.TeamYuhunRequest
	// 双方（发起方或接收方）均可取消 pending 或 accepted 状态的请求
	if err := s.db.Where(
		"id = ? AND (requester_id = ? OR receiver_id = ?) AND status IN ?",
		id, user.ID, user.ID,
		[]string{models.TeamYuhunStatusPending, models.TeamYuhunStatusAccepted},
	).First(&teamReq).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "组队请求不存在或无法取消"})
		return
	}

	now := time.Now().UTC()
	if err := s.db.Model(&teamReq).Updates(map[string]any{
		"status":     models.TeamYuhunStatusCancelled,
		"updated_at": now,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "取消组队请求失败"})
		return
	}

	s.audit(models.ActorTypeUser, user.ID, "team_yuhun_request_cancel", "team_yuhun_request", teamReq.ID,
		datatypes.JSONMap{"requester_id": teamReq.RequesterID, "receiver_id": teamReq.ReceiverID}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": teamReq.ID, "status": models.TeamYuhunStatusCancelled}})
}
