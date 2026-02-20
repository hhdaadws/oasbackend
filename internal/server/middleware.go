package server

import (
	"net/http"
	"time"

	"gorm.io/gorm"
	"oas-cloud-go/internal/auth"
	"oas-cloud-go/internal/models"

	"github.com/gin-gonic/gin"
)

const (
	ctxActorRoleKey   = "actor_role"
	ctxActorIDKey     = "actor_id"
	ctxManagerIDKey   = "manager_id"
	ctxUserIDKey      = "user_id"
	ctxUserTokenIDKey = "user_token_id"
)

func (s *Server) requireJWT(roles ...string) gin.HandlerFunc {
	allowed := map[string]struct{}{}
	for _, role := range roles {
		allowed[role] = struct{}{}
	}

	return func(c *gin.Context) {
		raw := auth.BearerToken(c.GetHeader("Authorization"))
		if raw == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "缺少Bearer令牌"})
			c.Abort()
			return
		}

		claims, err := s.tokenManager.ParseJWT(raw)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "无效的令牌"})
			c.Abort()
			return
		}
		if _, ok := allowed[claims.Role]; !ok {
			c.JSON(http.StatusForbidden, gin.H{"detail": "权限不足"})
			c.Abort()
			return
		}
		if claims.Role == models.ActorTypeAgent {
			ok, err := s.redisStore.ValidateAgentSession(c.Request.Context(), raw, claims.ManagerID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"detail": "Redis会话检查失败"})
				c.Abort()
				return
			}
			if !ok {
				c.JSON(http.StatusUnauthorized, gin.H{"detail": "Agent会话已过期"})
				c.Abort()
				return
			}
		}

		c.Set(ctxActorRoleKey, claims.Role)
		c.Set(ctxActorIDKey, claims.SubjectID)
		c.Set(ctxManagerIDKey, claims.ManagerID)
		c.Next()
	}
}

func (s *Server) requireUserToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := auth.BearerToken(c.GetHeader("Authorization"))
		if raw == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "缺少Bearer令牌"})
			c.Abort()
			return
		}

		hash := auth.HashToken(raw)
		now := time.Now().UTC()
		var token models.UserToken
		if err := s.db.Where("token_hash = ? AND revoked_at IS NULL AND expires_at > ?", hash, now).First(&token).Error; err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "无效的用户令牌"})
			c.Abort()
			return
		}
		var user models.User
		if err := s.db.Where("id = ?", token.UserID).First(&user).Error; err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "用户不存在"})
			c.Abort()
			return
		}
		if user.Status != models.UserStatusActive {
			c.JSON(http.StatusForbidden, gin.H{"detail": "用户账号未激活"})
			c.Abort()
			return
		}
		if user.ExpiresAt == nil || !user.ExpiresAt.After(now) {
			c.JSON(http.StatusForbidden, gin.H{"detail": "用户账号已过期"})
			c.Abort()
			return
		}

		// Throttle last_used_at updates: only write if >5 minutes since last update
		if token.LastUsedAt == nil || now.Sub(*token.LastUsedAt) > 5*time.Minute {
			_ = s.db.Model(&models.UserToken{}).Where("id = ?", token.ID).Update("last_used_at", now).Error
		}

		c.Set(ctxActorRoleKey, models.ActorTypeUser)
		c.Set(ctxActorIDKey, user.ID)
		c.Set(ctxUserIDKey, user.ID)
		c.Set(ctxUserTokenIDKey, token.ID)
		c.Set(ctxManagerIDKey, user.ManagerID)
		c.Next()
	}
}

func (s *Server) requireManagerActive() gin.HandlerFunc {
	return func(c *gin.Context) {
		managerID := getUint(c, ctxActorIDKey)
		if managerID == 0 {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "缺少管理员身份信息"})
			c.Abort()
			return
		}

		now := time.Now().UTC()

		// Try Redis cache first (avoids DB query on every request)
		if expiresAt, err := s.redisStore.GetManagerExpiry(c.Request.Context(), managerID); err == nil {
			if expiresAt.After(now) {
				c.Next()
				return
			}
			c.JSON(http.StatusForbidden, gin.H{"detail": "管理员账号已过期"})
			c.Abort()
			return
		}

		// Cache miss: query DB
		var manager models.Manager
		if err := s.db.Where("id = ?", managerID).First(&manager).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				c.JSON(http.StatusUnauthorized, gin.H{"detail": "管理员不存在"})
				c.Abort()
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询管理员失败"})
			c.Abort()
			return
		}

		if manager.ExpiresAt == nil || !manager.ExpiresAt.After(now) {
			c.JSON(http.StatusForbidden, gin.H{"detail": "管理员账号已过期"})
			c.Abort()
			return
		}

		// Cache manager expiry in Redis for 1 minute
		_ = s.redisStore.SetManagerExpiry(c.Request.Context(), managerID, *manager.ExpiresAt, time.Minute)
		c.Next()
	}
}

func getUint(c *gin.Context, key string) uint {
	value, exists := c.Get(key)
	if !exists {
		return 0
	}
	casted, ok := value.(uint)
	if !ok {
		return 0
	}
	return casted
}
