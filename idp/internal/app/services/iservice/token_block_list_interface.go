package iservice

import "time"

type TokenBlockListService interface {
	RegisterRefreshSession(refreshUUID, familyUUID string, expirationTime time.Duration) error
	IsRefreshSessionActive(refreshUUID, familyUUID string) (bool, error)
	AddToBlockList(jwtUUID string, expirationTime time.Duration) error
	IsInBlockList(jwtUUID string) (bool, error)
	IsRefreshFamilyRevoked(familyUUID string) (bool, error)
	RevokeRefreshFamily(familyUUID string, expirationTime time.Duration) error
	RotateRefreshToken(refreshUUID, familyUUID, replacementUUID string, expirationTime, replayGrace time.Duration, replacement string) (string, bool, error)
}
