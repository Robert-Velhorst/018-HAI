package source

import (
	"automation-hub-backend/internal/models"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestGoogleOAuthReconnectSQLUsesMonotoneRevision(t *testing.T) {
	for _, required := range []bool{true, false} {
		t.Run(fmt.Sprint(required), func(t *testing.T) {
			db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 user=unused dbname=unused"}), &gorm.Config{
				DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			var statement string
			if err := db.Callback().Update().After("gorm:update").Register("test:oauth-revision", func(tx *gorm.DB) {
				statement = tx.Statement.SQL.String()
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := (&GormRepository{DB: db}).SetGoogleOAuthReconnectRequired(uuid.New(), required); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(statement, "GREATEST(clock_timestamp(), updated_at + INTERVAL '1 microsecond')") {
				t.Fatalf("OAuth status update lacks a strictly advancing database revision: %s", statement)
			}
		})
	}
}

func TestGoogleOAuthReconnectRevisionMonotonePostgres(t *testing.T) {
	db := openTrelloRepositoryPostgresTestDB(t)
	withTrelloPostgresRollback(t, db, func(tx *gorm.DB) error {
		initial := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
		source := models.ConnectedSource{
			ID: uuid.New(), OwnerIdentity: "revision@example.test", ConnectorKey: gmailConnectorKey,
			Name: "Revision fixture", Category: "email", Enabled: true, Status: "active", UpdatedAt: initial,
		}
		if err := tx.Create(&source).Error; err != nil {
			return err
		}
		repository := &GormRepository{DB: tx}
		stale := source
		for _, required := range []bool{true, false} {
			changed, err := repository.SetGoogleOAuthReconnectRequired(source.ID, required)
			if err != nil || !changed {
				return fmt.Errorf("OAuth status transition changed=%v: %v", changed, err)
			}
			updated, err := repository.FindSource(source.ID)
			if err != nil {
				return err
			}
			wantStatus := "active"
			if required {
				wantStatus = "reconnect_required"
			}
			if !updated.UpdatedAt.After(source.UpdatedAt) || updated.Status != wantStatus || updated.OwnerIdentity != source.OwnerIdentity || updated.ConnectorKey != source.ConnectorKey {
				return fmt.Errorf("OAuth transition did not preserve identity and strictly advance its revision")
			}
			source = *updated
		}
		stale.Name = "stale replacement"
		if _, err := repository.UpdateSource(&stale); !errors.Is(err, ErrSourceChanged) {
			return fmt.Errorf("stale source update was not rejected after OAuth transitions: %v", err)
		}
		return nil
	})
}

func TestGoogleOAuthTokenFailureRevisionMonotonePostgres(t *testing.T) {
	db := openTrelloRepositoryPostgresTestDB(t)
	withTrelloPostgresRollback(t, db, func(tx *gorm.DB) error {
		initial := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
		source := models.ConnectedSource{
			ID: uuid.New(), OwnerIdentity: "token-revision@example.test", ConnectorKey: gmailConnectorKey,
			Name: "Token revision fixture", Category: "email", Enabled: true, Status: "active", UpdatedAt: initial,
		}
		if err := tx.Create(&source).Error; err != nil {
			return err
		}
		token := models.SourceOAuthToken{
			ID: uuid.New(), SourceID: source.ID, Provider: googleProvider,
			AccessToken: []byte("synthetic-encrypted-access"), RefreshToken: []byte("synthetic-encrypted-refresh"),
		}
		if err := tx.Create(&token).Error; err != nil {
			return err
		}
		repository := &GormRepository{DB: tx}
		expected, err := repository.FindOAuthToken(source.ID)
		if err != nil {
			return err
		}
		changed, err := repository.SetGoogleOAuthReconnectRequiredForToken(context.Background(), expected)
		if err != nil || !changed {
			return fmt.Errorf("token failure transition changed=%v: %v", changed, err)
		}
		updated, err := repository.FindSource(source.ID)
		if err != nil {
			return err
		}
		if !updated.UpdatedAt.After(source.UpdatedAt) || updated.Status != "reconnect_required" || updated.OwnerIdentity != source.OwnerIdentity {
			return fmt.Errorf("token failure did not preserve ownership and strictly advance source revision")
		}
		source.Name = "stale replacement"
		if _, err := repository.UpdateSource(&source); !errors.Is(err, ErrSourceChanged) {
			return fmt.Errorf("stale source update was not rejected after token failure: %v", err)
		}
		return nil
	})
}
