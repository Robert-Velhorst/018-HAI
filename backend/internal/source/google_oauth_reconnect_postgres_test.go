package source

import (
	"fmt"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestGoogleOAuthReconnectStatusTransitionsPostgres(t *testing.T) {
	db := openTrelloRepositoryPostgresTestDB(t)
	withTrelloPostgresRollback(t, db, func(tx *gorm.DB) error {
		revokedAt := time.Now().UTC()
		sources := []models.ConnectedSource{
			{ID: uuid.New(), ConnectorKey: gmailConnectorKey, Name: "OAuth active test", Category: "email", Enabled: true, Status: "active"},
			{ID: uuid.New(), ConnectorKey: driveConnectorKey, Name: "OAuth paused test", Category: "documents", Enabled: true, Status: "paused"},
			{ID: uuid.New(), ConnectorKey: calendarConnectorKey, Name: "OAuth revoked test", Category: "calendar", Enabled: false, Status: "revoked", RevokedAt: &revokedAt},
			{ID: uuid.New(), ConnectorKey: "local-folder", Name: "Non-OAuth test", Category: "local", Enabled: true, Status: "active"},
		}
		if err := tx.Create(&sources).Error; err != nil {
			return fmt.Errorf("create OAuth status fixtures: %w", err)
		}
		repository := &GormRepository{DB: tx}
		changed, err := repository.SetGoogleOAuthReconnectRequired(sources[0].ID, true)
		if err != nil || !changed {
			return fmt.Errorf("mark active Google source changed=%v err=%v", changed, err)
		}
		for _, index := range []int{1, 2, 3} {
			changed, err = repository.SetGoogleOAuthReconnectRequired(sources[index].ID, true)
			if err != nil || changed {
				return fmt.Errorf("unsafe source %q changed=%v err=%v", sources[index].ConnectorKey, changed, err)
			}
		}
		changed, err = repository.SetGoogleOAuthReconnectRequired(sources[0].ID, false)
		if err != nil || !changed {
			return fmt.Errorf("clear active Google source status changed=%v err=%v", changed, err)
		}
		var got models.ConnectedSource
		if err := tx.First(&got, "id = ?", sources[0].ID).Error; err != nil {
			return fmt.Errorf("reload active Google source: %w", err)
		}
		if got.Status != "active" {
			return fmt.Errorf("reconnected Google source status = %q, want active", got.Status)
		}
		return nil
	})
}
