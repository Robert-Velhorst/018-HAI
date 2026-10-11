package repositories

import "testing"

func TestGormUserRepositoryLoggingIsSafeWithoutLogger(t *testing.T) {
	repo := &GormUserRepository{}

	repo.logError("database operation failed: %v", "test")
}
