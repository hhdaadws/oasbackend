package server

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"oas-cloud-go/internal/auth"
	"oas-cloud-go/internal/cache"
	"oas-cloud-go/internal/config"
	"oas-cloud-go/internal/models"

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
	redisStore   *cache.RedisStore
	tokenManager *auth.TokenManager
	router       *gin.Engine
}

func New(cfg config.Config, db *gorm.DB, redisStore *cache.RedisStore) *Server {
	app := &Server{
		cfg:          cfg,
		db:           db,
		redisStore:   redisStore,
		tokenManager: auth.NewTokenManager(cfg.JWTSecret),
		router:       gin.New(),
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
		superGroup.GET("/managers", s.superListManagers)
		superGroup.PATCH("/managers/:id/status", s.superPatchManagerStatus)
	}

	managerGroup := api.Group("/manager")
	managerGroup.Use(s.requireJWT(models.ActorTypeManager))
	{
		managerGroup.POST("/auth/redeem-renewal-key", s.managerRedeemRenewalKey)
		managerGroup.POST("/activation-codes", s.managerCreateActivationCode)
		managerGroup.POST("/users/quick-create", s.managerQuickCreateUser)
		managerGroup.GET("/users", s.managerListUsers)
		managerGroup.GET("/users/:user_id/tasks", s.managerGetUserTasks)
		managerGroup.PUT("/users/:user_id/tasks", s.managerPutUserTasks)
		managerGroup.GET("/users/:user_id/logs", s.managerGetUserLogs)
	}

	userGroup := api.Group("/user")
	userGroup.Use(s.requireUserToken())
	{
		userGroup.POST("/auth/redeem-code", s.userRedeemCode)
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
}

func (s *Server) superConsole(c *gin.Context) {
	content, err := staticFS.ReadFile("static/super_console.html")
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load page")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", content)
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
	if manager.ExpiresAt == nil || !manager.ExpiresAt.After(now) {
		c.JSON(http.StatusForbidden, gin.H{"detail": "manager expired"})
		return
	}
	token, err := s.tokenManager.IssueJWT(models.ActorTypeManager, manager.ID, manager.ID, s.cfg.JWTTTL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to issue token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "role": models.ActorTypeManager, "manager_id": manager.ID})
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

func (s *Server) superListManagers(c *gin.Context) {
	var managers []models.Manager
	if err := s.db.Order("id asc").Find(&managers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to query managers"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": managers})
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
	if err := s.db.Model(&models.Manager{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to patch manager"})
		return
	}
	actorID := getUint(c, ctxActorIDKey)
	s.audit(models.ActorTypeSuper, actorID, "patch_manager_status", "manager", uint(id), datatypes.JSONMap{"status": req.Status}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"message": "manager status updated"})
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
	code, err := auth.GenerateOpaqueToken("uac", 12)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to generate activation code"})
		return
	}
	now := time.Now().UTC()
	activation := models.UserActivationCode{
		ManagerID:    managerID,
		Code:         code,
		DurationDays: req.DurationDays,
		Status:       models.CodeStatusUnused,
		CreatedAt:    now,
	}
	if err := s.db.Create(&activation).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to create activation code"})
		return
	}
	s.audit(models.ActorTypeManager, managerID, "create_activation_code", "user_activation_code", activation.ID, datatypes.JSONMap{"duration_days": req.DurationDays}, c.ClientIP())
	c.JSON(http.StatusCreated, gin.H{"code": activation.Code, "duration_days": activation.DurationDays})
}

func (s *Server) managerQuickCreateUser(c *gin.Context) {
	var req quickCreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	managerID := getUint(c, ctxActorIDKey)
	now := time.Now().UTC()

	var createdUser models.User
	err := s.db.Transaction(func(tx *gorm.DB) error {
		code, err := auth.GenerateOpaqueToken("uac", 12)
		if err != nil {
			return err
		}
		activation := models.UserActivationCode{
			ManagerID:    managerID,
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
	s.audit(models.ActorTypeManager, managerID, "quick_create_user", "user", createdUser.ID, datatypes.JSONMap{"duration_days": req.DurationDays}, c.ClientIP())
	c.JSON(http.StatusCreated, gin.H{"account_no": createdUser.AccountNo, "user_id": createdUser.ID, "expires_at": createdUser.ExpiresAt})
}

func (s *Server) managerListUsers(c *gin.Context) {
	managerID := getUint(c, ctxActorIDKey)
	var users []models.User
	if err := s.db.Where("manager_id = ?", managerID).Order("id asc").Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to query users"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": users})
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
	config, err := s.getOrCreateTaskConfig(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load task config"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user_id": userID, "task_config": config.TaskConfig, "version": config.Version})
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
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to update task config"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user_id": userID, "task_config": updated.TaskConfig, "version": updated.Version})
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
	c.JSON(http.StatusOK, gin.H{"token": rawToken, "account_no": user.AccountNo, "token_exp": tokenExpire})
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
		if err := tx.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
			"expires_at": newExpire,
			"status":     models.UserStatusActive,
			"updated_at": now,
		}).Error; err != nil {
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
	c.JSON(http.StatusOK, gin.H{"message": "renewal success"})
}

func (s *Server) userGetMeTasks(c *gin.Context) {
	userID := getUint(c, ctxUserIDKey)
	config, err := s.getOrCreateTaskConfig(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load task config"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"task_config": config.TaskConfig, "version": config.Version})
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
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to update task config"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"task_config": updated.TaskConfig, "version": updated.Version})
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
	accountNo, err := s.generateAccountNo(tx)
	if err != nil {
		return nil, err
	}
	newExpire := extendExpiry(nil, code.DurationDays, now)
	user := models.User{
		AccountNo: accountNo,
		ManagerID: code.ManagerID,
		Status:    models.UserStatusActive,
		ExpiresAt: &newExpire,
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

	cfg := models.UserTaskConfig{UserID: user.ID, TaskConfig: datatypes.JSONMap{}, UpdatedAt: now, Version: 1}
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
	var cfg models.UserTaskConfig
	err := s.db.Where("user_id = ?", userID).First(&cfg).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		now := time.Now().UTC()
		cfg = models.UserTaskConfig{UserID: userID, TaskConfig: datatypes.JSONMap{}, UpdatedAt: now, Version: 1}
		if err := s.db.Create(&cfg).Error; err != nil {
			return nil, err
		}
		return &cfg, nil
	}
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (s *Server) mergeTaskConfig(userID uint, patch map[string]any) (*models.UserTaskConfig, error) {
	now := time.Now().UTC()
	var result models.UserTaskConfig
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var cfg models.UserTaskConfig
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&cfg).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			cfg = models.UserTaskConfig{UserID: userID, TaskConfig: datatypes.JSONMap{}, UpdatedAt: now, Version: 1}
			if err := tx.Create(&cfg).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}

		base := map[string]any(cfg.TaskConfig)
		if base == nil {
			base = map[string]any{}
		}
		merged := deepMergeMap(base, patch)
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
