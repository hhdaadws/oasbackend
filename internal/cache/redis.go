package cache

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"oas-cloud-go/internal/auth"
	"oas-cloud-go/internal/config"

	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	client *redis.Client
	prefix string
}

type Store interface {
	Close() error
	Ping(ctx context.Context) error
	SaveAgentSession(ctx context.Context, token string, managerID uint, nodeID string, ttl time.Duration) error
	ValidateAgentSession(ctx context.Context, token string, managerID uint) (bool, error)
	AcquireJobLease(ctx context.Context, managerID uint, jobID uint, nodeID string, ttl time.Duration) (bool, error)
	IsJobLeaseOwner(ctx context.Context, managerID uint, jobID uint, nodeID string) (bool, error)
	RefreshJobLease(ctx context.Context, managerID uint, jobID uint, nodeID string, ttl time.Duration) (bool, error)
	ReleaseJobLease(ctx context.Context, managerID uint, jobID uint, nodeID string) error
	ClearJobLease(ctx context.Context, managerID uint, jobID uint) error
	AcquireScheduleSlot(
		ctx context.Context,
		managerID uint,
		userID uint,
		taskType string,
		slot string,
		ttl time.Duration,
	) (bool, error)
	GetManagerExpiry(ctx context.Context, managerID uint) (time.Time, error)
	SetManagerExpiry(ctx context.Context, managerID uint, expiresAt time.Time, ttl time.Duration) error
	// Scan job methods
	AcquireScanLease(ctx context.Context, scanJobID uint, nodeID string, ttl time.Duration) (bool, error)
	RefreshScanLease(ctx context.Context, scanJobID uint, nodeID string, ttl time.Duration) (bool, error)
	ReleaseScanLease(ctx context.Context, scanJobID uint, nodeID string) error
	IsScanLeaseOwner(ctx context.Context, scanJobID uint, nodeID string) (bool, error)
	ClearScanLease(ctx context.Context, scanJobID uint) error
	SetScanCooldown(ctx context.Context, userID uint, count int, lastAt time.Time) error
	GetScanCooldown(ctx context.Context, userID uint) (int, time.Time, error)
	SetScanUserChoice(ctx context.Context, scanJobID uint, choiceType string, value string) error
	GetScanUserChoice(ctx context.Context, scanJobID uint) (map[string]string, error)
	ClearScanUserChoice(ctx context.Context, scanJobID uint) error
	SetScanUserHeartbeat(ctx context.Context, scanJobID uint) error
	IsScanUserOnline(ctx context.Context, scanJobID uint) (bool, error)
	// User token cache
	SetUserTokenCache(ctx context.Context, tokenHash string, userID uint, managerID uint, status string, expiresAt time.Time, tokenExpiresAt time.Time, tokenID uint, ttl time.Duration) error
	GetUserTokenCache(ctx context.Context, tokenHash string) (userID uint, managerID uint, status string, expiresAt time.Time, tokenExpiresAt time.Time, tokenID uint, found bool, err error)
	ClearUserTokenCache(ctx context.Context, tokenHash string) error
	// Rate limiting
	CheckRateLimit(ctx context.Context, key string, limit int, window time.Duration) (allowed bool, err error)
}

func NewRedisStore(cfg config.Config) (*RedisStore, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         cfg.RedisAddr,
		Password:     cfg.RedisPassword,
		DB:           cfg.RedisDB,
		PoolSize:     cfg.RedisPoolSize,
		MinIdleConns: cfg.RedisMinIdleConns,
		PoolTimeout:  cfg.RedisPoolTimeout,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	prefix := strings.TrimSuffix(cfg.RedisKeyPrefix, ":")
	if prefix == "" {
		prefix = "oas:cloud"
	}
	return &RedisStore{
		client: client,
		prefix: prefix,
	}, nil
}

func (r *RedisStore) Close() error {
	return r.client.Close()
}

func (r *RedisStore) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

func (r *RedisStore) key(parts ...string) string {
	all := make([]string, 0, len(parts)+1)
	all = append(all, r.prefix)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		all = append(all, part)
	}
	return strings.Join(all, ":")
}

func (r *RedisStore) SaveAgentSession(
	ctx context.Context,
	token string,
	managerID uint,
	nodeID string,
	ttl time.Duration,
) error {
	if ttl <= 0 {
		ttl = time.Hour
	}
	key := r.key("agent", "session", auth.HashToken(token))
	fields := map[string]any{
		"manager_id": managerID,
		"node_id":    nodeID,
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	}
	pipe := r.client.TxPipeline()
	pipe.HSet(ctx, key, fields)
	pipe.Expire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (r *RedisStore) ValidateAgentSession(
	ctx context.Context,
	token string,
	managerID uint,
) (bool, error) {
	key := r.key("agent", "session", auth.HashToken(token))
	stored, err := r.client.HGet(ctx, key, "manager_id").Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	id, err := strconv.ParseUint(stored, 10, 64)
	if err != nil {
		return false, fmt.Errorf("invalid redis manager_id: %w", err)
	}
	return uint(id) == managerID, nil
}

func (r *RedisStore) AcquireJobLease(
	ctx context.Context,
	managerID uint,
	jobID uint,
	nodeID string,
	ttl time.Duration,
) (bool, error) {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	key := r.key("job", "lease", strconv.FormatUint(uint64(managerID), 10), strconv.FormatUint(uint64(jobID), 10))
	return r.client.SetNX(ctx, key, nodeID, ttl).Result()
}

func (r *RedisStore) IsJobLeaseOwner(
	ctx context.Context,
	managerID uint,
	jobID uint,
	nodeID string,
) (bool, error) {
	key := r.key("job", "lease", strconv.FormatUint(uint64(managerID), 10), strconv.FormatUint(uint64(jobID), 10))
	value, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == nodeID, nil
}

func (r *RedisStore) RefreshJobLease(
	ctx context.Context,
	managerID uint,
	jobID uint,
	nodeID string,
	ttl time.Duration,
) (bool, error) {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	key := r.key("job", "lease", strconv.FormatUint(uint64(managerID), 10), strconv.FormatUint(uint64(jobID), 10))
	script := redis.NewScript(`
local current = redis.call("GET", KEYS[1])
if not current then
  return 0
end
if current ~= ARGV[1] then
  return 0
end
redis.call("PEXPIRE", KEYS[1], ARGV[2])
return 1
`)
	res, err := script.Run(ctx, r.client, []string{key}, nodeID, ttl.Milliseconds()).Int()
	if err != nil {
		return false, err
	}
	return res == 1, nil
}

func (r *RedisStore) ReleaseJobLease(
	ctx context.Context,
	managerID uint,
	jobID uint,
	nodeID string,
) error {
	key := r.key("job", "lease", strconv.FormatUint(uint64(managerID), 10), strconv.FormatUint(uint64(jobID), 10))
	script := redis.NewScript(`
local current = redis.call("GET", KEYS[1])
if not current then
  return 1
end
if current ~= ARGV[1] then
  return 0
end
redis.call("DEL", KEYS[1])
return 1
`)
	res, err := script.Run(ctx, r.client, []string{key}, nodeID).Int()
	if err != nil {
		return err
	}
	if res == 0 {
		return fmt.Errorf("lease owner mismatch")
	}
	return nil
}

func (r *RedisStore) ClearJobLease(
	ctx context.Context,
	managerID uint,
	jobID uint,
) error {
	key := r.key("job", "lease", strconv.FormatUint(uint64(managerID), 10), strconv.FormatUint(uint64(jobID), 10))
	return r.client.Del(ctx, key).Err()
}

func (r *RedisStore) AcquireScheduleSlot(
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
	key := r.key(
		"scheduler",
		"slot",
		strconv.FormatUint(uint64(managerID), 10),
		strconv.FormatUint(uint64(userID), 10),
		taskType,
		slot,
	)
	return r.client.SetNX(ctx, key, "1", ttl).Result()
}

func (r *RedisStore) GetManagerExpiry(ctx context.Context, managerID uint) (time.Time, error) {
	key := r.key("manager", "expiry", strconv.FormatUint(uint64(managerID), 10))
	val, err := r.client.Get(ctx, key).Result()
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, val)
}

func (r *RedisStore) SetManagerExpiry(ctx context.Context, managerID uint, expiresAt time.Time, ttl time.Duration) error {
	key := r.key("manager", "expiry", strconv.FormatUint(uint64(managerID), 10))
	return r.client.Set(ctx, key, expiresAt.Format(time.RFC3339), ttl).Err()
}

// ── Scan job lease ──────────────────────────────────

func (r *RedisStore) scanLeaseKey(scanJobID uint) string {
	return r.key("scan", "lease", strconv.FormatUint(uint64(scanJobID), 10))
}

func (r *RedisStore) AcquireScanLease(ctx context.Context, scanJobID uint, nodeID string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return r.client.SetNX(ctx, r.scanLeaseKey(scanJobID), nodeID, ttl).Result()
}

func (r *RedisStore) RefreshScanLease(ctx context.Context, scanJobID uint, nodeID string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	script := redis.NewScript(`
local current = redis.call("GET", KEYS[1])
if not current then
  return 0
end
if current ~= ARGV[1] then
  return 0
end
redis.call("PEXPIRE", KEYS[1], ARGV[2])
return 1
`)
	res, err := script.Run(ctx, r.client, []string{r.scanLeaseKey(scanJobID)}, nodeID, ttl.Milliseconds()).Int()
	if err != nil {
		return false, err
	}
	return res == 1, nil
}

func (r *RedisStore) ReleaseScanLease(ctx context.Context, scanJobID uint, nodeID string) error {
	script := redis.NewScript(`
local current = redis.call("GET", KEYS[1])
if not current then
  return 1
end
if current ~= ARGV[1] then
  return 0
end
redis.call("DEL", KEYS[1])
return 1
`)
	res, err := script.Run(ctx, r.client, []string{r.scanLeaseKey(scanJobID)}, nodeID).Int()
	if err != nil {
		return err
	}
	if res == 0 {
		return fmt.Errorf("scan lease owner mismatch")
	}
	return nil
}

func (r *RedisStore) IsScanLeaseOwner(ctx context.Context, scanJobID uint, nodeID string) (bool, error) {
	value, err := r.client.Get(ctx, r.scanLeaseKey(scanJobID)).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == nodeID, nil
}

func (r *RedisStore) ClearScanLease(ctx context.Context, scanJobID uint) error {
	return r.client.Del(ctx, r.scanLeaseKey(scanJobID)).Err()
}

// ── Scan cooldown ──────────────────────────────────

func (r *RedisStore) SetScanCooldown(ctx context.Context, userID uint, count int, lastAt time.Time) error {
	key := r.key("scan", "cooldown", strconv.FormatUint(uint64(userID), 10))
	fields := map[string]any{
		"count":   count,
		"last_at": lastAt.UTC().Format(time.RFC3339),
	}
	pipe := r.client.TxPipeline()
	pipe.HSet(ctx, key, fields)
	pipe.Expire(ctx, key, 24*time.Hour)
	_, err := pipe.Exec(ctx)
	return err
}

func (r *RedisStore) GetScanCooldown(ctx context.Context, userID uint) (int, time.Time, error) {
	key := r.key("scan", "cooldown", strconv.FormatUint(uint64(userID), 10))
	vals, err := r.client.HGetAll(ctx, key).Result()
	if err != nil {
		return 0, time.Time{}, err
	}
	if len(vals) == 0 {
		return 0, time.Time{}, nil
	}
	count, _ := strconv.Atoi(vals["count"])
	lastAt, _ := time.Parse(time.RFC3339, vals["last_at"])
	return count, lastAt, nil
}

// ── Scan user choice ──────────────────────────────────

func (r *RedisStore) SetScanUserChoice(ctx context.Context, scanJobID uint, choiceType string, value string) error {
	key := r.key("scan", "user_choice", strconv.FormatUint(uint64(scanJobID), 10))
	pipe := r.client.TxPipeline()
	pipe.HSet(ctx, key, choiceType, value)
	pipe.Expire(ctx, key, 10*time.Minute)
	_, err := pipe.Exec(ctx)
	return err
}

func (r *RedisStore) GetScanUserChoice(ctx context.Context, scanJobID uint) (map[string]string, error) {
	key := r.key("scan", "user_choice", strconv.FormatUint(uint64(scanJobID), 10))
	return r.client.HGetAll(ctx, key).Result()
}

func (r *RedisStore) ClearScanUserChoice(ctx context.Context, scanJobID uint) error {
	key := r.key("scan", "user_choice", strconv.FormatUint(uint64(scanJobID), 10))
	return r.client.Del(ctx, key).Err()
}

// ── Scan user heartbeat ──────────────────────────────────

func (r *RedisStore) SetScanUserHeartbeat(ctx context.Context, scanJobID uint) error {
	key := r.key("scan", "user_heartbeat", strconv.FormatUint(uint64(scanJobID), 10))
	return r.client.Set(ctx, key, time.Now().UTC().Format(time.RFC3339), 30*time.Second).Err()
}

func (r *RedisStore) IsScanUserOnline(ctx context.Context, scanJobID uint) (bool, error) {
	key := r.key("scan", "user_heartbeat", strconv.FormatUint(uint64(scanJobID), 10))
	_, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ── User token cache ──────────────────────────────────

func (r *RedisStore) SetUserTokenCache(ctx context.Context, tokenHash string, userID uint, managerID uint, status string, expiresAt time.Time, tokenExpiresAt time.Time, tokenID uint, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	key := r.key("user", "token", tokenHash)
	fields := map[string]any{
		"user_id":          userID,
		"manager_id":       managerID,
		"status":           status,
		"expires_at":       expiresAt.UTC().Format(time.RFC3339),
		"token_expires_at": tokenExpiresAt.UTC().Format(time.RFC3339),
		"token_id":         tokenID,
	}
	pipe := r.client.TxPipeline()
	pipe.HSet(ctx, key, fields)
	pipe.Expire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (r *RedisStore) GetUserTokenCache(ctx context.Context, tokenHash string) (uint, uint, string, time.Time, time.Time, uint, bool, error) {
	key := r.key("user", "token", tokenHash)
	vals, err := r.client.HGetAll(ctx, key).Result()
	if err != nil {
		return 0, 0, "", time.Time{}, time.Time{}, 0, false, err
	}
	if len(vals) == 0 {
		return 0, 0, "", time.Time{}, time.Time{}, 0, false, nil
	}
	userID, _ := strconv.ParseUint(vals["user_id"], 10, 64)
	managerID, _ := strconv.ParseUint(vals["manager_id"], 10, 64)
	status := vals["status"]
	expiresAt, _ := time.Parse(time.RFC3339, vals["expires_at"])
	tokenExpiresAt, _ := time.Parse(time.RFC3339, vals["token_expires_at"])
	tokenID, _ := strconv.ParseUint(vals["token_id"], 10, 64)
	return uint(userID), uint(managerID), status, expiresAt, tokenExpiresAt, uint(tokenID), true, nil
}

func (r *RedisStore) ClearUserTokenCache(ctx context.Context, tokenHash string) error {
	key := r.key("user", "token", tokenHash)
	return r.client.Del(ctx, key).Err()
}

// ── Rate limiting ──────────────────────────────────

func (r *RedisStore) CheckRateLimit(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	rkey := r.key("ratelimit", key)
	pipe := r.client.TxPipeline()
	incr := pipe.Incr(ctx, rkey)
	pipe.Expire(ctx, rkey, window)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return true, err // allow on error
	}
	return incr.Val() <= int64(limit), nil
}
