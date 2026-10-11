package service_mock

import (
	"github.com/stretchr/testify/mock"
	"time"
)

type MockBlockListService struct {
	mock.Mock
}

func (m *MockBlockListService) RegisterRefreshSession(refreshUUID, familyUUID string, expirationTime time.Duration) error {
	return m.Called(refreshUUID, familyUUID, expirationTime).Error(0)
}

func (m *MockBlockListService) IsRefreshSessionActive(refreshUUID, familyUUID string) (bool, error) {
	args := m.Called(refreshUUID, familyUUID)
	return args.Bool(0), args.Error(1)
}

func (m *MockBlockListService) AddToBlockList(jwtUUID string, expirationTime time.Duration) error {
	args := m.Called(jwtUUID, expirationTime)
	return args.Error(0)
}

func (m *MockBlockListService) IsInBlockList(jwtUUID string) (bool, error) {
	args := m.Called(jwtUUID)
	return args.Get(0).(bool), args.Error(1)
}

func (m *MockBlockListService) IsRefreshFamilyRevoked(familyUUID string) (bool, error) {
	args := m.Called(familyUUID)
	return args.Get(0).(bool), args.Error(1)
}

func (m *MockBlockListService) RevokeRefreshFamily(familyUUID string, expirationTime time.Duration) error {
	args := m.Called(familyUUID, expirationTime)
	return args.Error(0)
}

func (m *MockBlockListService) RotateRefreshToken(refreshUUID, familyUUID, replacementUUID string, expirationTime, replayGrace time.Duration, replacement string) (string, bool, error) {
	args := m.Called(refreshUUID, familyUUID, replacementUUID, expirationTime, replayGrace, replacement)
	return args.String(0), args.Bool(1), args.Error(2)
}
