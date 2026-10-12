package operations

import (
	"context"
	"errors"
	"math"
	"time"

	"gorm.io/gorm"
)

var ErrSourceConfigurationUnsupported = errors.New("operations: registry source configuration authority is unavailable")

type RegistrySourceConfigurationRepository interface {
	WithRegistrySourceConfiguration(context.Context, SourceObservationStart, bool, func() error) error
}

func (s *Service) SharesSourceConfigurationStore(other *Service) bool {
	if s == nil || other == nil || s.repo == nil || other.repo == nil {
		return s == other
	}
	type memoryAuthorityIdentity interface{ RegistrySourceConfigurationStore() *MemoryRepository }
	a, aOK := s.repo.(memoryAuthorityIdentity)
	b, bOK := other.repo.(memoryAuthorityIdentity)
	return aOK && bOK && a.RegistrySourceConfigurationStore() == b.RegistrySourceConfigurationStore()
}

func (r *MemoryRepository) RegistrySourceConfigurationStore() *MemoryRepository { return r }

// The registry calls this while holding its own configuration lock. The memory
// store retains its authority lock across the registry write; effects never
// acquire a registry lock in reverse order. Callbacks must not reenter either store.
func (s *Service) WithRegistrySourceConfiguration(ctx context.Context, start SourceObservationStart, enabled bool, commit func() error) error {
	if err := safeEffectAdmissionError(ctx); err != nil {
		return err
	}
	if s == nil || commit == nil {
		return ErrSourceConfigurationUnsupported
	}
	repo, ok := s.repo.(RegistrySourceConfigurationRepository)
	if !ok {
		return ErrSourceConfigurationUnsupported
	}
	return repo.WithRegistrySourceConfiguration(ctx, start, enabled, commit)
}

func registrySourceOrigin(current SourceOrigin, exists bool, start SourceObservationStart, enabled bool) (SourceOrigin, error) {
	if !start.RegistryManaged || start.ConfigVersion <= 0 || validateObservationStart(start) != nil {
		return SourceOrigin{}, ErrInvalidSourceObservation
	}
	if !exists {
		return SourceOrigin{OwnerUserID: start.OwnerUserID, WorkspaceID: start.WorkspaceID,
			OriginID: start.OriginID, ConfigDigest: start.ConfigDigest, ConfigEpoch: 1,
			RegistryManaged: true, Enabled: enabled, ConfigVersion: start.ConfigVersion, ChangedAt: time.Now().UTC()}, nil
	}
	if current.RegistryManaged && start.ConfigVersion < current.ConfigVersion {
		return SourceOrigin{}, ErrSourceHeadSuperseded
	}
	if current.RegistryManaged && start.ConfigVersion == current.ConfigVersion && (current.ConfigDigest != start.ConfigDigest || current.Enabled != enabled) {
		return SourceOrigin{}, ErrSourceHeadSuperseded
	}
	if current.ConfigDigest != start.ConfigDigest || !current.RegistryManaged || current.Enabled != enabled || current.ConfigVersion != start.ConfigVersion {
		if current.ConfigEpoch == math.MaxInt64 {
			return SourceOrigin{}, ErrInvalidSourceObservation
		}
		current.ConfigEpoch++
		current.ChangedAt = time.Now().UTC()
	}
	current.ConfigDigest, current.RegistryManaged, current.Enabled = start.ConfigDigest, true, enabled
	current.ConfigVersion = start.ConfigVersion
	return current, nil
}

func (r *MemoryRepository) WithRegistrySourceConfiguration(ctx context.Context, start SourceObservationStart, enabled bool, commit func() error) error {
	if err := safeEffectAdmissionError(ctx); err != nil {
		return err
	}
	if r == nil || commit == nil {
		return ErrSourceConfigurationUnsupported
	}
	ctx, finish, err := BindExecutionContext(ctx)
	if err != nil {
		return err
	}
	defer finish()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := r.lockObservationContext(ctx); err != nil {
		return err
	}
	defer r.mu.Unlock()
	key := sourceOriginKey{start.OwnerUserID, start.WorkspaceID, start.OriginID}
	current, exists := r.sourceOrigins[key]
	next, err := registrySourceOrigin(current, exists, start, enabled)
	if err != nil {
		return err
	}
	if err := intakeContextError(ctx); err != nil {
		return err
	}
	if err := commit(); err != nil {
		return err
	}
	if r.sourceOrigins == nil {
		r.sourceOrigins = make(map[sourceOriginKey]SourceOrigin)
	}
	r.sourceOrigins[key] = next
	// Publish authority even if cancellation followed the committed registry
	// write. Returning an error must not leave a new config with old authority.
	return intakeContextError(ctx)
}

// PublishRegistrySourceConfiguration belongs in the same transaction as the
// locked account-feed row and its audit write. Never call it from a cached read.
func PublishRegistrySourceConfiguration(tx *gorm.DB, start SourceObservationStart, enabled bool) error {
	if tx == nil || tx.Statement == nil {
		return ErrSourceConfigurationUnsupported
	}
	if err := safeEffectAdmissionError(tx.Statement.Context); err != nil {
		return err
	}
	if !start.RegistryManaged || start.ConfigVersion <= 0 || validateObservationStart(start) != nil {
		return ErrInvalidSourceObservation
	}
	if err := tx.Exec("SET LOCAL lock_timeout = '5s'").Error; err != nil {
		return err
	}
	var epoch int64
	err := tx.Raw(`INSERT INTO public.operation_source_origins
(owner_user_id, workspace_id, origin_id, config_digest, config_epoch, registry_managed, enabled, registry_config_version)
VALUES (?, ?, ?, ?, 1, true, ?, ?) ON CONFLICT (owner_user_id, workspace_id, origin_id) DO UPDATE
SET config_digest = EXCLUDED.config_digest, registry_managed = true, enabled = EXCLUDED.enabled, registry_config_version = EXCLUDED.registry_config_version,
config_epoch = operation_source_origins.config_epoch + CASE WHEN
operation_source_origins.config_digest <> EXCLUDED.config_digest OR NOT operation_source_origins.registry_managed
OR operation_source_origins.enabled <> EXCLUDED.enabled OR operation_source_origins.registry_config_version <> EXCLUDED.registry_config_version THEN 1 ELSE 0 END,
changed_at = CASE WHEN operation_source_origins.config_digest <> EXCLUDED.config_digest
OR NOT operation_source_origins.registry_managed OR operation_source_origins.enabled <> EXCLUDED.enabled OR operation_source_origins.registry_config_version <> EXCLUDED.registry_config_version
THEN clock_timestamp() ELSE operation_source_origins.changed_at END
WHERE (NOT operation_source_origins.registry_managed
OR operation_source_origins.registry_config_version < EXCLUDED.registry_config_version
OR (operation_source_origins.registry_config_version = EXCLUDED.registry_config_version
AND operation_source_origins.config_digest = EXCLUDED.config_digest AND operation_source_origins.enabled = EXCLUDED.enabled))
AND ((operation_source_origins.config_digest = EXCLUDED.config_digest AND operation_source_origins.registry_managed
AND operation_source_origins.enabled = EXCLUDED.enabled AND operation_source_origins.registry_config_version = EXCLUDED.registry_config_version)
OR operation_source_origins.config_epoch < ?)
RETURNING config_epoch`, start.OwnerUserID, start.WorkspaceID, start.OriginID, start.ConfigDigest, enabled, start.ConfigVersion, int64(math.MaxInt64)).Row().Scan(&epoch)
	if err != nil {
		return err
	}
	return intakeContextError(tx.Statement.Context)
}

func validateManagedSourceObservation(origin SourceOrigin, start SourceObservationStart) error {
	if !origin.RegistryManaged || !origin.Enabled || origin.ConfigDigest != start.ConfigDigest || start.ConfigVersion <= 0 || origin.ConfigVersion != start.ConfigVersion {
		return ErrSourceHeadSuperseded
	}
	return nil
}
