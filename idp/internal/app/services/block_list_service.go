package services

import (
	"automation-hub-idp/internal/app/config"
	"context"
	"github.com/go-redis/redis/v8"
	"time"
)
import "automation-hub-idp/internal/app/services/iservice"

type tokenBlockListServiceImpl struct {
	client *redis.Client
}

func NewRedisTokenBlockListService() iservice.TokenBlockListService {
	rdb := redis.NewClient(&redis.Options{
		Addr: config.RedisConfig.RedisAddr,
	})

	return &tokenBlockListServiceImpl{
		client: rdb,
	}
}

func (r *tokenBlockListServiceImpl) AddToBlockList(ctx context.Context, jwtUUID string, expirationTime time.Duration) error {
	err := r.client.Set(ctx, jwtUUID, 1, expirationTime).Err()
	return err
}

func (r *tokenBlockListServiceImpl) IsInBlockList(ctx context.Context, jwtUUID string) (bool, error) {
	_, err := r.client.Get(ctx, jwtUUID).Result()
	if err != nil {
		if err == redis.Nil {
			return false, nil
		}
		return false, err
	}
	return true, err
}
