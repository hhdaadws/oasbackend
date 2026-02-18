package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr               string
	DatabaseURL        string
	RedisAddr          string
	RedisPassword      string
	RedisDB            int
	RedisKeyPrefix     string
	JWTSecret          string
	JWTTTL             time.Duration
	AgentJWTTTL        time.Duration
	UserTokenTTL       time.Duration
	DefaultLeaseSecond int
	MaxPollLimit       int
}

func Load() Config {
	return Config{
		Addr:               getEnv("ADDR", ":8080"),
		DatabaseURL:        getEnv("DATABASE_URL", "postgres://postgres:postgres@127.0.0.1:5432/oas_cloud?sslmode=disable"),
		RedisAddr:          getEnv("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword:      getEnv("REDIS_PASSWORD", ""),
		RedisDB:            getIntEnv("REDIS_DB", 0),
		RedisKeyPrefix:     getEnv("REDIS_KEY_PREFIX", "oas:cloud"),
		JWTSecret:          getEnv("JWT_SECRET", "change-me-in-production"),
		JWTTTL:             getDurationEnv("JWT_TTL", 24*time.Hour),
		AgentJWTTTL:        getDurationEnv("AGENT_JWT_TTL", 12*time.Hour),
		UserTokenTTL:       getDurationEnv("USER_TOKEN_TTL", 180*24*time.Hour),
		DefaultLeaseSecond: getIntEnv("DEFAULT_LEASE_SECONDS", 90),
		MaxPollLimit:       getIntEnv("MAX_POLL_LIMIT", 20),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getIntEnv(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
