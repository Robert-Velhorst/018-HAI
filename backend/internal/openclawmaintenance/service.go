package openclawmaintenance

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	checkInterval             = 24 * time.Hour
	checkRetryInterval        = time.Hour
	maxAutomaticCheckFailures = 3
	stalePendingJobAfter      = 20 * time.Minute
)

var ErrUnattendedInstallationBlocked = errors.New("unattended OpenClaw installation is blocked because verified target-specific process containment is unavailable")
var ErrGatewayCoreInstallationBlocked = errors.New("Gateway/core WSL installation remains blocked until Linux-side process-tree containment is verified")
var ErrCompanionCapabilityRequired = errors.New("Companion installation requires a fresh authenticated Windows containment capability")
var ErrBackgroundProcessingPaused = errors.New("background processing is paused by HAI safety policy")

var ErrTaskAdmissionRequiresFreshCurrentChecks = errors.New("OpenClaw task admission is paused: both components must have fresh successful checks reporting current, with no due check, pending maintenance, or review; refresh checks and resolve outstanding status in HAI before retrying")

type Service struct {
	db                      *gorm.DB
	now                     func() time.Time
	verifyHealth            func(context.Context) bool
	scheduleWake            chan struct{}
	canSafelyInstall        func() bool
	installCapabilityReason func(string) string
	backgroundAllowed       func() bool
}

func NewService(db *gorm.DB) *Service {
	return &Service{
		db:                db,
		now:               func() time.Time { return time.Now().UTC() },
		scheduleWake:      make(chan struct{}, 1),
		canSafelyInstall:  func() bool { return false },
		backgroundAllowed: func() bool { return false },
	}
}
func (s *Service) SetHealthVerifier(verify func(context.Context) bool) { s.verifyHealth = verify }

func (s *Service) SetBackgroundProcessingGate(allowed func() bool) {
	if allowed == nil {
		s.backgroundAllowed = func() bool { return false }
		return
	}
	s.backgroundAllowed = allowed
}

func (s *Service) backgroundProcessingAllowed() bool {
	return s != nil && s.backgroundAllowed != nil && s.backgroundAllowed()
}

func (s *Service) notifyScheduler() {
	if s.scheduleWake == nil {
		return
	}
	select {
	case s.scheduleWake <- struct{}{}:
	default:
	}
}

func defaultPolicy(id string) string {
	return "observe"
}

func (s *Service) installationAvailable(target string) bool {
	if target != "companion" || s.canSafelyInstall == nil {
		return false
	}
	return s.canSafelyInstall()
}

func (s *Service) installationReason(target string) string {
	if s.installCapabilityReason != nil {
		if reason := s.installCapabilityReason(target); reason != "" {
			return reason
		}
	}
	return installationBlockReason(target)
}

func checkRetryDelay(consecutiveFailures int) time.Duration {
	if consecutiveFailures >= maxAutomaticCheckFailures {
		return checkInterval
	}
	return checkRetryInterval
}

func freshCheckTimestamp(checkedAt *time.Time, now time.Time) bool {
	return checkedAt != nil && !checkedAt.After(now) && now.Sub(*checkedAt) < checkInterval
}

func hasFreshCurrentCheck(tx *gorm.DB, target *Target, now time.Time) (bool, error) {
	if target == nil || target.State != "current" || target.CheckFailures != 0 || target.ReceiptID == "" ||
		target.CheckedAt == nil || !freshCheckTimestamp(target.CheckedAt, now) || target.NextCheck.IsZero() || !now.Before(target.NextCheck) ||
		!ValidVersion(target.Installed) || !ValidVersion(target.Available) || target.Installed != target.Available {
		return false, nil
	}

	var receipt Job
	if err := tx.First(&receipt, "id = ? AND target = ?", target.ReceiptID, target.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if (receipt.Kind != "check" && receipt.Kind != "apply") || receipt.Status != "completed" || receipt.StartedAt == nil || receipt.FinishedAt == nil ||
		receipt.Worker == "" || receipt.LeaseDigest == "" || receipt.CreatedAt.After(*receipt.StartedAt) ||
		receipt.StartedAt.After(*receipt.FinishedAt) || !receipt.FinishedAt.Equal(*target.CheckedAt) || !freshCheckTimestamp(receipt.FinishedAt, now) {
		return false, nil
	}

	var report Report
	if json.Unmarshal([]byte(receipt.Result), &report) != nil || ValidateReport(receipt, report) != nil ||
		report.Outcome != "ok" || !providerMetadataFreshAtReceipt(report.ProviderMetadataAt, &receipt) ||
		report.Installed != target.Installed || report.Available != target.Available {
		return false, nil
	}
	return true, nil
}

func verifiedApplyEvidence(target *Target, receipt *Job, report *Report, version string, now time.Time) bool {
	if target == nil || receipt == nil || report == nil || !freshCheckTimestamp(target.CheckedAt, now) {
		return false
	}
	if receipt.ID != target.ReceiptID || receipt.Target != target.ID || receipt.Kind != "check" || receipt.Status != "completed" ||
		receipt.FinishedAt == nil || !receipt.FinishedAt.Equal(*target.CheckedAt) || !freshCheckTimestamp(receipt.FinishedAt, now) {
		return false
	}
	if ValidateReport(*receipt, *report) != nil || !providerMetadataFreshAtReceipt(report.ProviderMetadataAt, receipt) {
		return false
	}
	return report.Outcome == "ok" && report.PublisherVerified && report.Installed == target.Installed &&
		report.Available == version && target.Available == version
}

func (s *Service) Initialize(ctx context.Context) error {
	for _, id := range []string{"companion", "gateway_core"} {
		if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&Target{ID: id, Policy: defaultPolicy(id), State: "unknown", NextCheck: s.now()}).Error; err != nil {
			return err
		}
		// Disable legacy implicit automatic policies. An explicit owner policy
		// always takes precedence on later startups.
		if err := s.transact(ctx, id, func(tx *gorm.DB, target *Target) error {
			if target.Policy != "auto_install_verified" || target.UpdatedBy != "" {
				return nil
			}
			target.Policy = "observe"
			now := s.now()
			audit, _ := json.Marshal(map[string]string{"owner": "system-safety-default", "reason": "automatic installation now requires explicit owner approval"})
			return tx.Create(&Job{ID: uuid.NewString(), Target: id, Kind: "policy", Status: "completed", Version: target.Policy, Result: string(audit), CreatedAt: now, FinishedAt: &now}).Error
		}); err != nil {
			return err
		}
	}
	s.notifyScheduler()
	return nil
}

// NextScheduledCheck returns the earliest persisted check deadline not already
// covered by pending or leased work for that target.
func (s *Service) NextScheduledCheck(ctx context.Context) (time.Time, bool, error) {
	for _, id := range []string{"companion", "gateway_core"} {
		if err := s.transact(ctx, id, func(tx *gorm.DB, target *Target) error {
			_, err := s.expire(tx, target, s.now())
			return err
		}); err != nil {
			return time.Time{}, false, err
		}
	}
	var targets []Target
	if err := s.db.WithContext(ctx).Select("id", "next_check").Order("next_check").Find(&targets).Error; err != nil {
		return time.Time{}, false, err
	}
	if len(targets) == 0 {
		return time.Time{}, false, nil
	}
	var activeTargets []string
	if err := s.db.WithContext(ctx).Model(&Job{}).Distinct("target").Where("status IN ?", []string{"pending", "leased"}).Pluck("target", &activeTargets).Error; err != nil {
		return time.Time{}, false, err
	}
	active := make(map[string]struct{}, len(activeTargets))
	for _, id := range activeTargets {
		active[id] = struct{}{}
	}
	for _, target := range targets {
		if _, hasActiveWork := active[target.ID]; !hasActiveWork {
			if target.NextCheck.IsZero() {
				return s.now(), true, nil
			}
			return target.NextCheck, true, nil
		}
	}
	return time.Time{}, false, nil
}

func (s *Service) Overview(ctx context.Context) ([]Target, error) {
	for _, id := range []string{"companion", "gateway_core"} {
		changed := false
		if err := s.transact(ctx, id, func(tx *gorm.DB, target *Target) error {
			var err error
			changed, err = s.expire(tx, target, s.now())
			return err
		}); err != nil {
			return nil, err
		}
		if changed {
			s.notifyScheduler()
		}
	}
	var targets []Target
	err := s.db.WithContext(ctx).Order("id").Find(&targets).Error
	if err != nil {
		return nil, err
	}
	var jobs []Job
	if err = s.db.WithContext(ctx).Select("id", "target", "kind", "status", "started_at").Where("status IN ?", []string{"pending", "leased", "needs_review"}).Order("created_at").Find(&jobs).Error; err != nil {
		return nil, err
	}
	for i := range targets {
		targets[i].InstallStatus = "not_required"
		if targets[i].State == "update_available" {
			targets[i].InstallStatus = "blocked"
			targets[i].InstallBlockedReason = s.installationReason(targets[i].ID)
			if s.installationAvailable(targets[i].ID) {
				targets[i].InstallStatus = "capability_ready"
				targets[i].InstallBlockedReason = "Containment is available; owner policy, publisher evidence and runtime safety gates still apply."
			}
		}
		for _, j := range jobs {
			if j.Target == targets[i].ID {
				if j.Status == "needs_review" && j.Kind == "apply" {
					targets[i].ReviewRequired = true
				} else {
					targets[i].PendingKind = j.Kind
					targets[i].PendingStatus = j.Status
					targets[i].PendingStartedAt = j.StartedAt
				}
			}
		}
		if targets[i].ID == "companion" && targets[i].State == "update_available" && targets[i].ReceiptID != "" {
			var receipt Job
			if err = s.db.WithContext(ctx).Select("result").First(&receipt, "id = ?", targets[i].ReceiptID).Error; err != nil {
				return nil, err
			}
			var report Report
			if json.Unmarshal([]byte(receipt.Result), &report) != nil || !report.PublisherVerified || report.PublisherPinStatus != "verified" {
				pinReason := companionPublisherBlockReason(report.PublisherPinStatus)
				targets[i].InstallBlockedReason = pinReason + " " + targets[i].InstallBlockedReason
			}
		}
	}
	return targets, err
}

func installationBlockReason(target string) string {
	switch target {
	case "companion":
		return "Companion installation requires a fresh authenticated worker capability proving Windows Job Object containment."
	case "gateway_core":
		return "Gateway/core WSL installation remains blocked: Windows Job Objects do not contain Linux WSL descendants, and Linux-side containment is unverified."
	default:
		return "Unattended installation is unavailable for this target."
	}
}

func companionPublisherBlockReason(status string) string {
	switch status {
	case "missing":
		return "Companion update is blocked: HAI_OPENCLAW_PUBLISHER_THUMBPRINT is not configured on the Windows worker."
	case "invalid":
		return "Companion update is blocked: the configured Windows publisher thumbprint is invalid."
	case "mismatch":
		return "Companion update needs review: the installer publisher does not match the configured Windows identity pin."
	default:
		return "Companion update needs review: installer publisher identity has not been verified."
	}
}

func (s *Service) transact(ctx context.Context, id string, fn func(*gorm.DB, *Target) error) error {
	if !ValidTarget(id) {
		return fmt.Errorf("unknown maintenance target")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(1842032026)).Error; err != nil {
			return err
		}
		var target Target
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&target, "id = ?", id).Error; err != nil {
			return err
		}
		if err := fn(tx, &target); err != nil {
			return err
		}
		return tx.Save(&target).Error
	})
}

func (s *Service) Policy(ctx context.Context, id, policy, owner string) error {
	if owner == "" || (policy != "observe" && policy != "auto_install_verified") {
		return fmt.Errorf("invalid policy")
	}
	if id == "gateway_core" && policy == "auto_install_verified" {
		return ErrGatewayCoreInstallationBlocked
	}
	err := s.transact(ctx, id, func(tx *gorm.DB, t *Target) error {
		if safety.EmergencyStopActive() && policy != "observe" {
			return fmt.Errorf("emergency stop is active")
		}
		t.Policy, t.UpdatedBy = policy, owner
		if policy == "observe" {
			if err := tx.Model(&Job{}).Where("target = ? AND kind = ? AND status = ?", id, "apply", "pending").Updates(map[string]any{"status": "cancelled", "finished_at": s.now()}).Error; err != nil {
				return err
			}
		}
		// Keep each policy change in the same immutable job ledger as maintenance.
		now := s.now()
		audit, _ := json.Marshal(map[string]string{"owner": owner})
		return tx.Create(&Job{ID: uuid.NewString(), Target: id, Kind: "policy", Status: "completed", Version: policy, Result: string(audit), CreatedAt: now, FinishedAt: &now}).Error
	})
	if err == nil {
		s.notifyScheduler()
	}
	return err
}

func (s *Service) Schedule(ctx context.Context) error {
	if !s.backgroundProcessingAllowed() {
		return nil
	}
	for _, id := range []string{"companion", "gateway_core"} {
		if err := s.queue(ctx, id, "check", "", false); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) Check(ctx context.Context, id string) error {
	return s.queue(ctx, id, "check", "", true)
}
func (s *Service) Apply(ctx context.Context, id, version string) error {
	return s.queue(ctx, id, "apply", version, true)
}

func (s *Service) queue(ctx context.Context, id, kind, version string, manual bool) error {
	err := s.transact(ctx, id, func(tx *gorm.DB, t *Target) error {
		now := s.now()
		if _, err := s.expire(tx, t, now); err != nil {
			return err
		}
		var pending int64
		if err := tx.Model(&Job{}).Where("target = ? AND status IN ?", id, []string{"pending", "leased"}).Count(&pending).Error; err != nil {
			return err
		}
		if pending > 0 {
			return nil
		}
		if kind == "check" && !manual && now.Before(t.NextCheck) {
			return nil
		}
		if kind == "apply" {
			if err := s.canApply(tx, t, version); err != nil {
				return err
			}
		}
		job := Job{ID: uuid.NewString(), Target: id, Kind: kind, Version: version, Status: "pending", CreatedAt: now}
		if kind == "apply" {
			var receipt Job
			if err := tx.First(&receipt, "id = ?", t.ReceiptID).Error; err != nil {
				return err
			}
			var report Report
			if err := json.Unmarshal([]byte(receipt.Result), &report); err != nil {
				return err
			}
			job.Evidence = report.Evidence
		}
		return tx.Create(&job).Error
	})
	if err == nil {
		s.notifyScheduler()
	}
	return err
}

// queueRecoveryCheck records a read-only observation after an ambiguous apply.
// It deliberately never creates or replays an installation job.
func (s *Service) queueRecoveryCheck(tx *gorm.DB, target *Target, now, createdAt time.Time) error {
	var active int64
	if err := tx.Model(&Job{}).Where("target = ? AND status IN ?", target.ID, []string{"pending", "leased"}).Count(&active).Error; err != nil {
		return err
	}
	if active > 0 {
		return nil
	}
	target.NextCheck = now
	return tx.Create(&Job{ID: uuid.NewString(), Target: target.ID, Kind: "check", Status: "pending", CreatedAt: createdAt}).Error
}

func (s *Service) canApply(tx *gorm.DB, t *Target, version string) error {
	if t == nil || t.ID == "gateway_core" {
		return ErrGatewayCoreInstallationBlocked
	}
	if t.ID != "companion" {
		return ErrUnattendedInstallationBlocked
	}
	if !s.backgroundProcessingAllowed() {
		return ErrBackgroundProcessingPaused
	}
	if !s.installationAvailable(t.ID) {
		return ErrCompanionCapabilityRequired
	}
	if safety.EmergencyStopActive() {
		return fmt.Errorf("emergency stop is active")
	}
	now := s.now()
	if t.Policy != "auto_install_verified" || t.State != "update_available" || !freshCheckTimestamp(t.CheckedAt, now) || version != t.Available || !Newer(version, t.Installed) {
		return fmt.Errorf("update needs a current verified release and an enabled target policy")
	}
	var receipt Job
	if err := tx.First(&receipt, "id = ?", t.ReceiptID).Error; err != nil {
		return err
	}
	var report Report
	if json.Unmarshal([]byte(receipt.Result), &report) != nil || !verifiedApplyEvidence(t, &receipt, &report, version, now) {
		return fmt.Errorf("release publisher verification is required")
	}
	var active int64
	if err := tx.Model(&Job{}).Where("kind = ? AND status = ?", "apply", "needs_review").Count(&active).Error; err != nil {
		return err
	}
	if active > 0 {
		return fmt.Errorf("an earlier update requires owner review")
	}
	if err := tx.Model(&models.OpenClawGatewaySessionReceipt{}).
		Where("status IN ? AND reconciled_at IS NULL", []string{"admitting", "admitted", "needs_review"}).
		Count(&active).Error; err != nil {
		return err
	}
	if active > 0 {
		return fmt.Errorf("OpenClaw has active delegated sessions")
	}
	return nil
}

func (s *Service) expire(tx *gorm.DB, t *Target, now time.Time) (bool, error) {
	var stalePending []Job
	if err := tx.Where("target = ? AND status = ? AND created_at < ?", t.ID, "pending", now.Add(-stalePendingJobAfter)).Find(&stalePending).Error; err != nil {
		return false, err
	}
	changed := false
	for _, j := range stalePending {
		changed = true
		if j.Kind == "apply" {
			j.Status = "needs_review"
			t.State = "needs_review"
			t.Reason = "An installer job remained pending without a confirmed worker start. Do not retry it until a fresh read-only check and owner review confirm the installation state."
			t.Policy = "observe"
			t.UpdatedBy = "system:openclaw-needs-review"
		} else if j.Kind == "check" {
			j.Status = "failed"
			t.CheckFailures++
			if t.State != "needs_review" {
				t.State = "check_failed"
				t.Reason = "A read-only check remained queued without a worker lease. Confirmed versions were preserved and a retry is scheduled."
			}
			t.NextCheck = now.Add(checkRetryDelay(t.CheckFailures))
		} else {
			j.Status = "failed"
			t.Reason = "An unsupported maintenance job remained pending past its recovery deadline and was closed without execution."
		}
		j.FinishedAt = &now
		if j.Result == "" {
			j.Result = `{"reason":"stale_pending_job"}`
		}
		if err := tx.Save(&j).Error; err != nil {
			return false, err
		}
		if j.Kind == "apply" {
			if err := s.queueRecoveryCheck(tx, t, now, now.Add(time.Second)); err != nil {
				return false, err
			}
		}
	}
	var expired []Job
	if err := tx.Where("target = ? AND status = ? AND (lease_until IS NULL OR lease_until < ?)", t.ID, "leased", now).Find(&expired).Error; err != nil {
		return changed, err
	}
	for _, j := range expired {
		changed = true
		state := "failed"
		if j.Kind == "apply" {
			state = "needs_review"
			t.State = state
			t.Reason = "An update worker disconnected. Inspect the installation before authorizing another update."
			t.Policy = "observe"
			t.UpdatedBy = "system:openclaw-needs-review"
		} else if j.Kind == "check" {
			t.CheckFailures++
			if t.State != "needs_review" {
				t.State = "check_failed"
				t.Reason = "The read-only check worker did not complete; the last confirmed versions were preserved. A retry is scheduled."
				if t.CheckFailures >= maxAutomaticCheckFailures {
					t.Reason = fmt.Sprintf("The read-only check worker failed %d consecutive times. Automatic read-only checks continue daily; updates remain blocked until a fresh verified check succeeds.", t.CheckFailures)
				}
			}
			t.NextCheck = now.Add(checkRetryDelay(t.CheckFailures))
		}
		if err := tx.Model(&j).Updates(map[string]any{"status": state, "finished_at": now}).Error; err != nil {
			return false, err
		}
		if j.Kind == "apply" {
			if err := s.queueRecoveryCheck(tx, t, now, now.Add(time.Second)); err != nil {
				return false, err
			}
		}
	}
	return changed, nil
}

func digest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Service) Lease(ctx context.Context, worker string) (*Lease, error) {
	if !s.backgroundProcessingAllowed() {
		return nil, nil
	}
	var schedulerChanged bool
	defer func() {
		if schedulerChanged {
			s.notifyScheduler()
		}
	}()
	var result *Lease
	for _, id := range []string{"companion", "gateway_core"} {
		if err := s.transact(ctx, id, func(tx *gorm.DB, t *Target) error {
			changed, err := s.expire(tx, t, s.now())
			schedulerChanged = schedulerChanged || changed
			return err
		}); err != nil {
			return nil, err
		}
	}
	for _, id := range []string{"companion", "gateway_core"} {
		err := s.transact(ctx, id, func(tx *gorm.DB, t *Target) error {
			changed, err := s.expire(tx, t, s.now())
			schedulerChanged = schedulerChanged || changed
			if err != nil {
				return err
			}
			var j Job
			if err := tx.Order("created_at").First(&j, "target = ? AND status = ?", id, "pending").Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				return err
			}
			if j.Kind == "apply" {
				var installing int64
				if err := tx.Model(&Job{}).Where("kind = ? AND status = ?", "apply", "leased").Count(&installing).Error; err != nil {
					return err
				}
				if installing > 0 {
					return nil
				}
				if !s.installationAvailable(j.Target) {
					j.Status = "cancelled"
					blockCode := "companion_worker_capability_unavailable"
					if j.Target == "gateway_core" {
						blockCode = "gateway_core_wsl_containment_unverified"
					}
					result, _ := json.Marshal(map[string]string{"reason": blockCode})
					j.Result = string(result)
					now := s.now()
					j.FinishedAt = &now
					return tx.Save(&j).Error
				}
				if err := s.canApply(tx, t, j.Version); err != nil {
					j.Status = "cancelled"
					now := s.now()
					j.FinishedAt = &now
					return tx.Save(&j).Error
				}
			}
			var secret [32]byte
			if _, err := rand.Read(secret[:]); err != nil {
				return err
			}
			token := hex.EncodeToString(secret[:])
			until := s.now().Add(20 * time.Minute)
			j.Status, j.Worker, j.LeaseDigest, j.LeaseUntil = "leased", worker, digest(token), &until
			if err := tx.Save(&j).Error; err != nil {
				return err
			}
			result = &Lease{Job: j, Token: token}
			return nil
		})
		if err != nil {
			return nil, err
		}
		if result != nil {
			return result, nil
		}
	}
	return nil, nil
}

func validLease(j Job, worker, token string, now time.Time) bool {
	return token != "" && j.Status == "leased" && j.Worker == worker && j.LeaseDigest == digest(token) && j.LeaseUntil != nil && now.Before(*j.LeaseUntil)
}

func (s *Service) Confirm(ctx context.Context, id, worker, token string) error {
	var found Job
	if err := s.db.WithContext(ctx).First(&found, "id = ?", id).Error; err != nil {
		return err
	}
	err := s.transact(ctx, found.Target, func(tx *gorm.DB, t *Target) error {
		if !s.backgroundProcessingAllowed() {
			return ErrBackgroundProcessingPaused
		}
		var j Job
		if err := tx.First(&j, "id = ?", id).Error; err != nil {
			return err
		}
		if !validLease(j, worker, token, s.now()) || j.StartedAt != nil {
			return fmt.Errorf("lease cannot start or has already started")
		}
		if j.Kind == "apply" {
			if err := s.canApply(tx, t, j.Version); err != nil {
				return err
			}
			t.State = "installing"
		}
		now := s.now()
		return tx.Model(&j).Update("started_at", now).Error
	})
	return err
}

func (s *Service) Complete(ctx context.Context, id, worker, token string, r Report) error {
	var found Job
	if err := s.db.WithContext(ctx).First(&found, "id = ?", id).Error; err != nil {
		return err
	}
	// Receipt identity is based on the worker's exact submission, not on
	// time-sensitive provider policy or HAI's independent health decision.
	submitted, _ := json.Marshal(r)
	if receiptRetry(found, worker, token, string(submitted)) {
		return nil
	}
	providerMetadataRejected := r.Outcome == "ok" && !providerMetadataFresh(r.ProviderMetadataAt, s.now())
	if providerMetadataRejected {
		if found.Kind == "check" {
			r.Outcome = "unavailable"
		} else if found.Kind == "apply" {
			r.Outcome = "needs_review"
			r.HealthOK = false
		}
	}
	if err := ValidateReport(found, r); err != nil {
		return err
	}
	encoded, _ := json.Marshal(r)
	if receiptRetry(found, worker, token, string(encoded)) {
		return nil
	}
	if !validLease(found, worker, token, s.now()) || found.StartedAt == nil {
		return fmt.Errorf("stale or unstarted receipt")
	}
	backendHealthFailed := false
	if found.Kind == "apply" && r.Outcome == "ok" && (s.verifyHealth == nil || !s.verifyHealth(ctx)) {
		backendHealthFailed = true
	}
	err := s.transact(ctx, found.Target, func(tx *gorm.DB, t *Target) error {
		var j Job
		if err := tx.First(&j, "id = ?", id).Error; err != nil {
			return err
		}
		if receiptRetry(j, worker, token, string(submitted)) {
			return nil
		}
		if r.Outcome == "ok" && !providerMetadataFresh(r.ProviderMetadataAt, s.now()) {
			providerMetadataRejected = true
			if j.Kind == "check" {
				r.Outcome = "unavailable"
			} else if j.Kind == "apply" {
				r.Outcome = "needs_review"
				r.HealthOK = false
			}
			if err := ValidateReport(j, r); err != nil {
				return err
			}
			encoded, _ = json.Marshal(r)
		}
		if receiptRetry(j, worker, token, string(encoded)) {
			return nil
		}
		if !validLease(j, worker, token, s.now()) || j.StartedAt == nil {
			return fmt.Errorf("stale or unstarted receipt")
		}
		// PostgreSQL stores timestamptz at microsecond precision. Normalize the
		// check receipt and target metadata together so their persisted timestamps
		// remain identical when that evidence is reloaded inside this transaction.
		now := s.now().UTC().Truncate(time.Microsecond)
		j.Result, j.FinishedAt = string(encoded), &now
		if j.Kind == "check" {
			if r.Outcome != "ok" {
				j.Status = "failed"
				t.CheckFailures++
				t.NextCheck = now.Add(checkRetryDelay(t.CheckFailures))
				var unresolved int64
				if err := tx.Model(&Job{}).Where("target = ? AND kind = ? AND status = ?", t.ID, "apply", "needs_review").Count(&unresolved).Error; err != nil {
					return err
				}
				if unresolved == 0 {
					t.State = "check_failed"
					if providerMetadataRejected {
						t.Reason = "The provider response was stale or its timestamp was outside the accepted clock-skew window. Last confirmed versions were preserved and a retry is scheduled."
					} else {
						t.Reason = "The read-only check reported " + r.Outcome + "; last confirmed versions were preserved and a retry is scheduled."
					}
					if t.CheckFailures >= maxAutomaticCheckFailures {
						t.Reason = fmt.Sprintf("The read-only check reported %s for %d consecutive checks. Automatic read-only checks continue daily; updates remain blocked until a fresh verified check succeeds.", r.Outcome, t.CheckFailures)
					}
				}
				return tx.Save(&j).Error
			}
			j.Status = "completed"
			t.Installed, t.Available, t.CheckedAt, t.ReceiptID = r.Installed, r.Available, &now, j.ID
			t.NextCheck = now.Add(checkInterval)
			t.CheckFailures = 0
			var unresolved int64
			if err := tx.Model(&Job{}).Where("target = ? AND kind = ? AND status = ?", t.ID, "apply", "needs_review").Count(&unresolved).Error; err != nil {
				return err
			}
			if unresolved > 0 {
				t.State = "needs_review"
			} else {
				t.Reason = ""
				switch {
				case r.Available == "":
					t.State = "unknown"
				case Newer(r.Available, r.Installed):
					t.State = "update_available"
				default:
					t.State = "current"
				}
			}
			if err := tx.Save(&j).Error; err != nil {
				return err
			}
			applyErr := s.canApply(tx, t, t.Available)
			if unresolved == 0 && t.ID == "companion" && t.State == "update_available" && t.Policy == "auto_install_verified" && r.PublisherVerified && s.installationAvailable(t.ID) && applyErr == nil {
				return tx.Create(&Job{ID: uuid.NewString(), Target: t.ID, Kind: "apply", Version: t.Available, Evidence: r.Evidence, Status: "pending", CreatedAt: now}).Error
			}
			return nil
		}
		if j.Kind != "apply" {
			return fmt.Errorf("unsupported maintenance receipt kind")
		}
		if r.Outcome != "ok" || backendHealthFailed {
			j.Status = "needs_review"
			t.State, t.Policy = "needs_review", "observe"
			if providerMetadataRejected {
				t.Reason = "The provider response was stale or its timestamp was outside the accepted clock-skew window. The update result requires owner review; a read-only check was queued."
			} else if backendHealthFailed {
				t.Reason = "HAI's independent runtime health check did not confirm the update. A read-only check was queued; owner review is still required."
			} else {
				t.Reason = "The update worker reported " + r.Outcome + ". A read-only check was queued; owner review is still required."
			}
			if err := tx.Save(&j).Error; err != nil {
				return err
			}
			return s.queueRecoveryCheck(tx, t, now, now.Add(time.Second))
		}
		j.Status = "completed"
		t.Installed, t.Available = r.Installed, r.Available
		t.CheckFailures = 0
		t.Reason, t.VerifiedAt = "", &now
		if r.Available == r.Installed {
			t.CheckedAt, t.ReceiptID = &now, j.ID
			t.NextCheck = now.Add(checkInterval)
			t.State = "current"
			return tx.Save(&j).Error
		}

		// A new release observed during installation invalidates the pre-install
		// check. Require a fresh read-only provider check before task admission or
		// any later update can be planned.
		t.CheckedAt, t.ReceiptID = nil, ""
		t.NextCheck = now
		if r.Available != "" && Newer(r.Available, r.Installed) {
			t.State = "update_available"
		} else {
			t.State = "unknown"
		}
		if err := tx.Save(&j).Error; err != nil {
			return err
		}
		return s.queueRecoveryCheck(tx, t, now, now.Add(time.Second))
	})
	if err == nil {
		s.notifyScheduler()
	}
	return err
}

func (s *Service) ReceiptStatus(ctx context.Context, id string) (string, error) {
	var job Job
	if err := s.db.WithContext(ctx).Select("status").First(&job, "id = ?", id).Error; err != nil {
		return "", err
	}
	switch job.Status {
	case "completed", "failed", "needs_review", "reviewed":
		return job.Status, nil
	default:
		return "", fmt.Errorf("maintenance receipt is not terminal")
	}
}

func receiptRetry(j Job, worker, token, encoded string) bool {
	return token != "" && (j.Status == "completed" || j.Status == "failed" || j.Status == "needs_review" || j.Status == "reviewed") && j.Worker == worker && j.LeaseDigest == digest(token) && j.Result == encoded
}

// Review resolves an ambiguous update only after a fresh read-only observation,
// independent health probe, and explicit owner acknowledgement. It never enables
// automatic updates or grants delegated capabilities.
func (s *Service) Review(ctx context.Context, id, owner string) error {
	if owner == "" || s.verifyHealth == nil || !s.verifyHealth(ctx) {
		return fmt.Errorf("owner and healthy runtime are required")
	}
	return s.transact(ctx, id, func(tx *gorm.DB, t *Target) error {
		var receipt Job
		if err := tx.First(&receipt, "id = ?", t.ReceiptID).Error; err != nil {
			return err
		}
		var report Report
		now := s.now()
		if receipt.Kind != "check" || receipt.Status != "completed" || t.CheckedAt == nil || now.Before(*t.CheckedAt) || now.Sub(*t.CheckedAt) > time.Hour || json.Unmarshal([]byte(receipt.Result), &report) != nil || ValidateReport(receipt, report) != nil || report.Outcome != "ok" || !ValidVersion(report.Installed) {
			return fmt.Errorf("a fresh successful check is required")
		}
		if !providerMetadataFreshAtReceipt(report.ProviderMetadataAt, &receipt) {
			return fmt.Errorf("a check with fresh provider metadata is required")
		}
		var unresolved []Job
		if err := tx.Where("target = ? AND kind = ? AND status = ?", id, "apply", "needs_review").Find(&unresolved).Error; err != nil {
			return err
		}
		if len(unresolved) == 0 {
			return fmt.Errorf("no update requires review")
		}
		for _, job := range unresolved {
			if job.FinishedAt == nil || !receipt.CreatedAt.After(*job.FinishedAt) {
				return fmt.Errorf("check must follow the interrupted update")
			}
		}
		if err := tx.Model(&Job{}).Where("target = ? AND kind = ? AND status = ?", id, "apply", "needs_review").Update("status", "reviewed").Error; err != nil {
			return err
		}
		t.Policy, t.UpdatedBy = "observe", owner
		t.Installed, t.Available = report.Installed, report.Available
		t.Reason = ""
		switch {
		case report.Available == "":
			t.State = "unknown"
		case Newer(report.Available, report.Installed):
			t.State = "update_available"
		default:
			t.State = "current"
		}
		audit, _ := json.Marshal(map[string]string{"owner": owner, "checkReceipt": receipt.ID})
		return tx.Create(&Job{ID: uuid.NewString(), Target: id, Kind: "review", Status: "completed", Result: string(audit), CreatedAt: now, FinishedAt: &now}).Error
	})
}

// Permit rechecks policy after release verification and immediately before the
// installer starts. It does not create or renew installation authority.
func (s *Service) Permit(ctx context.Context, id, worker, token string) error {
	var j Job
	if err := s.db.WithContext(ctx).First(&j, "id = ?", id).Error; err != nil {
		return err
	}
	return s.transact(ctx, j.Target, func(tx *gorm.DB, t *Target) error {
		if err := tx.First(&j, "id = ?", id).Error; err != nil {
			return err
		}
		if !validLease(j, worker, token, s.now()) || j.StartedAt == nil || j.Kind != "apply" || t.State != "installing" {
			return fmt.Errorf("update lease is no longer valid")
		}
		copyTarget := *t
		copyTarget.State = "update_available"
		return s.canApply(tx, &copyTarget, j.Version)
	})
}

// AcquireTask serializes task admission with update leasing. The transaction
// stays open until the adapter records its session receipt or finishes its CLI.
func (s *Service) AcquireTask(ctx context.Context) (func(), error) {
	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	release := func() { tx.Rollback() }
	if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(1842032026)).Error; err != nil {
		release()
		return nil, err
	}
	now := s.now()
	for _, id := range []string{"companion", "gateway_core"} {
		var target Target
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&target, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				release()
				return nil, ErrTaskAdmissionRequiresFreshCurrentChecks
			}
			release()
			return nil, err
		}
		ready, err := hasFreshCurrentCheck(tx, &target, now)
		if err != nil {
			release()
			return nil, err
		}
		if !ready {
			release()
			return nil, ErrTaskAdmissionRequiresFreshCurrentChecks
		}
	}
	var active int64
	if err := tx.Model(&Job{}).Where("status IN ?", []string{"pending", "leased", "needs_review"}).Count(&active).Error; err != nil {
		release()
		return nil, err
	}
	if active > 0 {
		release()
		return nil, ErrTaskAdmissionRequiresFreshCurrentChecks
	}
	return release, nil
}
