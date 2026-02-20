package main

import (
	"log"
	"log/slog"
	"os"
	"strings"

	"oas-cloud-go/internal/cache"
	"oas-cloud-go/internal/config"
	"oas-cloud-go/internal/models"
	"oas-cloud-go/internal/server"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	cfg := config.Load()

	// Initialize structured logging
	var level slog.Level
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if strings.ToLower(cfg.LogFormat) == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(handler))

	db, err := gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect database: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("failed to get underlying sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(cfg.DBMaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.DBMaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.DBConnMaxLifetime)

	if err := models.AutoMigrate(db); err != nil {
		log.Fatalf("failed to migrate database: %v", err)
	}

	redisStore, err := cache.NewRedisStore(cfg)
	if err != nil {
		log.Fatalf("failed to connect redis: %v", err)
	}
	defer redisStore.Close()

	app := server.New(cfg, db, redisStore)
	if err := app.Run(); err != nil {
		log.Fatalf("server stopped with error: %v", err)
	}
}
