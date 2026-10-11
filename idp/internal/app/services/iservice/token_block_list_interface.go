package iservice

import (
	"context"
	"time"
)

type TokenBlockListService interface {
	AddToBlockList(ctx context.Context, jwtUUID string, expirationTime time.Duration) error
	IsInBlockList(ctx context.Context, jwtUUID string) (bool, error)
}
