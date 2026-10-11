package service_mock

import (
	"context"
	"github.com/stretchr/testify/mock"
	"time"
)

type MockBlockListService struct {
	mock.Mock
}

func (m *MockBlockListService) AddToBlockList(ctx context.Context, jwtUUID string, expirationTime time.Duration) error {
	args := m.Called(ctx, jwtUUID, expirationTime)
	return args.Error(0)
}

func (m *MockBlockListService) IsInBlockList(ctx context.Context, jwtUUID string) (bool, error) {
	args := m.Called(ctx, jwtUUID)
	return args.Get(0).(bool), args.Error(1)
}
