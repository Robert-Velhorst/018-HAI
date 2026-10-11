package services

import (
	"automation-hub-idp/internal/app/config"
	"automation-hub-idp/internal/app/services/iservice"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
)

// Repeated logout/replay requests must never shorten an existing revocation.
const preserveRevocationTTL = `
local function revoke(key, ttl)
  local current = redis.call('PTTL', key)
  if current == -2 or (current >= 0 and current < tonumber(ttl)) then
    redis.call('SET', key, '1', 'PX', ttl)
  end
end
`

var registerRefreshSessionScript = redis.NewScript(`
for _, key in ipairs(KEYS) do
  if redis.call('EXISTS', key) == 1 then
    return 0
  end
end
redis.call('SET', KEYS[3], '1', 'PX', ARGV[1])
redis.call('SET', KEYS[4], ARGV[2], 'PX', ARGV[1])
return 1
`)

var activeRefreshSessionScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1], KEYS[2]) ~= 0 then
  return 0
end
if redis.call('GET', KEYS[3]) ~= '1' or redis.call('GET', KEYS[4]) ~= ARGV[1] then
  return 0
end
if redis.call('PTTL', KEYS[3]) <= 0 or redis.call('PTTL', KEYS[4]) <= 0 then
  return 0
end
return 1
`)

// Positive session records are required: losing a revocation key cannot revive
// an old token. Consume authority before writes that can fail (Lua has no rollback).
var rotateRefreshTokenScript = redis.NewScript(preserveRevocationTTL + `
if redis.call('EXISTS', KEYS[3]) == 1 or redis.call('GET', KEYS[4]) ~= '1' then
  return {2, ''}
end
local ttl = math.min(tonumber(ARGV[1]), redis.call('PTTL', KEYS[4]))
if ttl <= 0 then
  return {2, ''}
end
local cached = redis.call('GET', KEYS[2])
if cached then
  return {0, cached}
end
if redis.call('EXISTS', KEYS[1]) == 1 or redis.call('GET', KEYS[5]) ~= ARGV[4] or redis.call('PTTL', KEYS[5]) <= 0 then
  redis.call('DEL', KEYS[4])
  revoke(KEYS[3], ttl)
  return {2, ''}
end
if redis.call('EXISTS', KEYS[6], KEYS[7]) ~= 0 then
  return {2, ''}
end
ttl = math.min(ttl, redis.call('PTTL', KEYS[5]))
redis.call('DEL', KEYS[5])
revoke(KEYS[1], ttl)
redis.call('SET', KEYS[6], ARGV[4], 'PX', ttl)
redis.call('SET', KEYS[2], ARGV[3], 'PX', math.min(tonumber(ARGV[2]), ttl))
return {1, ARGV[3]}
`)

var revokeRefreshFamilyScript = redis.NewScript(preserveRevocationTTL + `
redis.call('DEL', KEYS[2])
revoke(KEYS[1], ARGV[1])
return 1
`)

var revokeTokenScript = redis.NewScript(preserveRevocationTTL + `
redis.call('DEL', KEYS[2], KEYS[3])
revoke(KEYS[1], ARGV[1])
return 1
`)

type tokenBlockListServiceImpl struct {
	client *redis.Client
	ctx    context.Context
}

func (r *tokenBlockListServiceImpl) Ping(ctx context.Context) error {
	if r == nil || r.client == nil {
		return errors.New("token revocation store is unavailable")
	}
	if ctx == nil {
		return errors.New("token revocation readiness requires a context")
	}
	return r.client.Ping(ctx).Err()
}

func NewRedisTokenBlockListService() iservice.TokenBlockListService {
	address := ""
	if config.RedisConfig != nil {
		address = config.RedisConfig.RedisAddr
	}
	rdb := NewIdentityRedisClient(address)

	ctx := context.TODO()

	return &tokenBlockListServiceImpl{
		client: rdb,
		ctx:    ctx,
	}
}

func (r *tokenBlockListServiceImpl) RegisterRefreshSession(refreshUUID, familyUUID string, expirationTime time.Duration) error {
	if err := r.available(); err != nil {
		return err
	}
	if strings.TrimSpace(refreshUUID) == "" || strings.TrimSpace(familyUUID) == "" || expirationTime <= 0 {
		return errors.New("refresh registration requires IDs and positive expiration")
	}
	result, err := registerRefreshSessionScript.Run(r.ctx, r.client,
		[]string{refreshUUID, refreshFamilyKey(familyUUID), activeRefreshFamilyKey(familyUUID), activeRefreshTokenKey(refreshUUID)},
		redisTTLMilliseconds(expirationTime), familyUUID).Int()
	if err != nil {
		return err
	}
	if result != 1 {
		return errors.New("refresh session already exists or is revoked")
	}
	return nil
}

func (r *tokenBlockListServiceImpl) IsRefreshSessionActive(refreshUUID, familyUUID string) (bool, error) {
	if err := r.available(); err != nil {
		return false, err
	}
	if strings.TrimSpace(refreshUUID) == "" || strings.TrimSpace(familyUUID) == "" {
		return false, errors.New("refresh session IDs are required")
	}
	result, err := activeRefreshSessionScript.Run(r.ctx, r.client,
		[]string{refreshUUID, refreshFamilyKey(familyUUID), activeRefreshFamilyKey(familyUUID), activeRefreshTokenKey(refreshUUID)}, familyUUID).Int()
	return result == 1 && err == nil, err
}

func (r *tokenBlockListServiceImpl) AddToBlockList(jwtUUID string, expirationTime time.Duration) error {
	if err := r.available(); err != nil {
		return err
	}
	if strings.TrimSpace(jwtUUID) == "" || expirationTime <= 0 {
		return errors.New("token revocation requires an ID and positive expiration")
	}
	return revokeTokenScript.Run(r.ctx, r.client, []string{jwtUUID, refreshRotationKey(jwtUUID), activeRefreshTokenKey(jwtUUID)}, redisTTLMilliseconds(expirationTime)).Err()
}

func (r *tokenBlockListServiceImpl) IsInBlockList(jwtUUID string) (bool, error) {
	if err := r.available(); err != nil {
		return false, err
	}
	if strings.TrimSpace(jwtUUID) == "" {
		return false, errors.New("token ID is required")
	}
	_, err := r.client.Get(r.ctx, jwtUUID).Result()
	if err != nil {
		if err == redis.Nil {
			return false, nil
		}
		return false, err
	}
	return true, err
}

func (r *tokenBlockListServiceImpl) IsRefreshFamilyRevoked(familyUUID string) (bool, error) {
	if err := r.available(); err != nil {
		return false, err
	}
	if strings.TrimSpace(familyUUID) == "" {
		return false, errors.New("refresh family ID is required")
	}
	_, err := r.client.Get(r.ctx, refreshFamilyKey(familyUUID)).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *tokenBlockListServiceImpl) RevokeRefreshFamily(familyUUID string, expirationTime time.Duration) error {
	if err := r.available(); err != nil {
		return err
	}
	if strings.TrimSpace(familyUUID) == "" || expirationTime <= 0 {
		return errors.New("refresh family revocation requires an ID and positive expiration")
	}
	return revokeRefreshFamilyScript.Run(r.ctx, r.client, []string{refreshFamilyKey(familyUUID), activeRefreshFamilyKey(familyUUID)}, redisTTLMilliseconds(expirationTime)).Err()
}

func (r *tokenBlockListServiceImpl) RotateRefreshToken(refreshUUID, familyUUID, replacementUUID string, expirationTime, replayGrace time.Duration, replacement string) (string, bool, error) {
	if err := r.available(); err != nil {
		return "", false, err
	}
	if strings.TrimSpace(refreshUUID) == "" || strings.TrimSpace(familyUUID) == "" || strings.TrimSpace(replacementUUID) == "" || replacementUUID == refreshUUID || strings.TrimSpace(replacement) == "" ||
		expirationTime <= 0 || replayGrace <= 0 {
		return "", false, errors.New("refresh rotation requires IDs, replacement data, and positive expirations")
	}
	if replayGrace > expirationTime {
		replayGrace = expirationTime
	}
	result, err := rotateRefreshTokenScript.Run(
		r.ctx,
		r.client,
		[]string{refreshUUID, refreshRotationKey(refreshUUID), refreshFamilyKey(familyUUID), activeRefreshFamilyKey(familyUUID), activeRefreshTokenKey(refreshUUID), activeRefreshTokenKey(replacementUUID), replacementUUID},
		redisTTLMilliseconds(expirationTime),
		redisTTLMilliseconds(replayGrace),
		replacement,
		familyUUID,
	).Result()
	if err != nil {
		return "", false, err
	}
	parts, ok := result.([]interface{})
	if !ok || len(parts) != 2 {
		return "", false, fmt.Errorf("unexpected refresh rotation response %T", result)
	}
	status, err := redisScriptInteger(parts[0])
	if err != nil {
		return "", false, err
	}
	value, ok := parts[1].(string)
	if !ok {
		if bytes, isBytes := parts[1].([]byte); isBytes {
			value = string(bytes)
		} else {
			return "", false, fmt.Errorf("unexpected refresh rotation payload %T", parts[1])
		}
	}
	switch status {
	case 1:
		return value, true, nil
	case 0:
		if value == "" {
			return "", false, errors.New("cached refresh rotation payload is empty")
		}
		return value, false, nil
	case 2:
		return "", false, nil
	default:
		return "", false, fmt.Errorf("unexpected refresh rotation status %d", status)
	}
}

func (r *tokenBlockListServiceImpl) available() error {
	if r == nil || r.client == nil || r.ctx == nil {
		return errors.New("token revocation store is unavailable")
	}
	return nil
}

func (r *tokenBlockListServiceImpl) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Close()
}

func redisTTLMilliseconds(duration time.Duration) int64 {
	milliseconds := duration.Milliseconds()
	if time.Duration(milliseconds)*time.Millisecond < duration {
		milliseconds++
	}
	if milliseconds < 1 {
		return 1
	}
	return milliseconds
}

func refreshRotationKey(refreshUUID string) string {
	return "refresh-rotation:" + refreshUUID
}

func refreshFamilyKey(familyUUID string) string {
	return "refresh-family:" + familyUUID
}

func activeRefreshFamilyKey(familyUUID string) string {
	return "refresh-family-active:" + familyUUID
}

func activeRefreshTokenKey(refreshUUID string) string {
	return "refresh-active:" + refreshUUID
}

func redisScriptInteger(value interface{}) (int64, error) {
	switch number := value.(type) {
	case int64:
		return number, nil
	case int:
		return int64(number), nil
	default:
		return 0, fmt.Errorf("unexpected refresh rotation status type %T", value)
	}
}
