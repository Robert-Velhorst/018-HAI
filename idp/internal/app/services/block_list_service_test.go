package services

import (
	"context"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestTokenBlockListStopsBeforeRedisWhenRequestIsCancelled(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: time.Second,
	})
	t.Cleanup(func() { _ = client.Close() })
	service := &tokenBlockListServiceImpl{client: client}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := service.AddToBlockList(ctx, "cancelled-token", time.Minute)
	require.ErrorIs(t, err, context.Canceled)

	blocked, err := service.IsInBlockList(ctx, "cancelled-token")
	require.False(t, blocked)
	require.ErrorIs(t, err, context.Canceled)
}
