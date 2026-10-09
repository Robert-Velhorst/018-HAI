package openclawmaintenance

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"automation-hub-backend/internal/durablejob"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/lifecycle"
	"github.com/gin-gonic/gin"
)

const (
	// The durable scan is only a fallback for missed in-process timer signals.
	// The normal path follows each Target.NextCheck deadline directly.
	maintenanceRecoveryScanInterval  = 15 * time.Minute
	maintenanceScheduleRefreshPeriod = 15 * time.Minute
	maintenanceScheduleErrorRetry    = time.Minute
	maintenanceRunnerPollInterval    = 5 * time.Minute
	maintenanceWorkerContactTTL      = 3 * time.Minute
	maintenanceWorkerCapabilityTTL   = 90 * time.Second
)

func maintenanceScheduleDelay(now, due time.Time, hasDue bool, retryAt time.Time) time.Duration {
	target := now.Add(maintenanceScheduleRefreshPeriod)
	if hasDue {
		target = due
		if retryAt.After(target) {
			target = retryAt
		}
	} else if retryAt.After(now) && retryAt.Before(target) {
		target = retryAt
	}
	if target.After(now.Add(maintenanceScheduleRefreshPeriod)) {
		target = now.Add(maintenanceScheduleRefreshPeriod)
	}
	delay := target.Sub(now)
	if delay < 0 {
		return 0
	}
	return delay
}

func maintenanceScheduleReady(now, due time.Time, hasDue bool, retryAt time.Time, deadlineErr error) bool {
	if !retryAt.IsZero() && !now.Before(retryAt) {
		return true
	}
	return deadlineErr == nil && hasDue && !now.Before(due)
}

func (s *Service) runScheduledChecks(ctx context.Context) {
	var retryAt time.Time
	if err := s.Schedule(ctx); err != nil {
		log.Printf("openclaw maintenance: initial due-check scheduling failed: %v", err)
		retryAt = s.now().Add(maintenanceScheduleErrorRetry)
	}

	for ctx.Err() == nil {
		if !s.backgroundProcessingAllowed() {
			timer := time.NewTimer(maintenanceScheduleRefreshPeriod)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return
			case <-s.scheduleWake:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			case <-timer.C:
			}
			continue
		}
		due, hasDue, err := s.NextScheduledCheck(ctx)
		if err != nil {
			log.Printf("openclaw maintenance: load next check deadline failed: %v", err)
			retryAt = s.now().Add(maintenanceScheduleErrorRetry)
			hasDue = false
		}
		now := s.now()
		delay := maintenanceScheduleDelay(now, due, hasDue, retryAt)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-s.scheduleWake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			continue
		case <-timer.C:
		}

		now = s.now()
		if !maintenanceScheduleReady(now, due, hasDue, retryAt, err) {
			continue
		}
		if err = s.Schedule(ctx); err != nil {
			log.Printf("openclaw maintenance: due-check scheduling failed: %v", err)
			retryAt = s.now().Add(maintenanceScheduleErrorRetry)
			continue
		}
		retryAt = time.Time{}
	}
}

type Handler struct {
	service            *Service
	token, worker      string
	enabled            bool
	workerMu           sync.RWMutex
	workerLastSeen     time.Time
	workerCapability   WorkerCapabilityHandshake
	workerCapabilityAt time.Time
}

type WorkerContactStatus struct {
	State      string     `json:"state"`
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`
	FreshFor   string     `json:"freshFor"`
}

type InstallationCapability struct {
	Available   bool                         `json:"available"`
	Reason      string                       `json:"reason,omitempty"`
	FreshFor    string                       `json:"freshFor"`
	Companion   TargetInstallationCapability `json:"companion"`
	GatewayCore TargetInstallationCapability `json:"gatewayCore"`
}

type TargetInstallationCapability struct {
	Available  bool       `json:"available"`
	State      string     `json:"state"`
	Reason     string     `json:"reason,omitempty"`
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`
}

func maintenanceConfigEnabled(flag, token string, otherSecrets ...string) bool {
	flag = strings.TrimSpace(flag)
	if flag != "" && !strings.EqualFold(flag, "true") {
		return false
	}
	if len(token) < 32 {
		return false
	}
	for _, r := range token {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("_+/=.-", r)) {
			return false
		}
	}
	for _, other := range otherSecrets {
		if other != "" && subtle.ConstantTimeCompare([]byte(other), []byte(token)) == 1 {
			return false
		}
	}
	return true
}

func maintenanceConfigEnabledFromEnvironment() bool {
	return maintenanceConfigEnabled(
		os.Getenv("HAI_OPENCLAW_MAINTENANCE_ENABLED"),
		os.Getenv("HAI_OPENCLAW_MAINTENANCE_TOKEN"),
		os.Getenv("BACKEND_API_SHARED_KEY"),
		os.Getenv("HAI_HOST_RUNTIME_BRIDGE_TOKEN"),
		os.Getenv("OPENCLAW_GATEWAY_TOKEN"),
		os.Getenv("OPENCLAW_GATEWAY_DELEGATION_TOKEN"),
	)
}

func NewHandler(s *Service) *Handler {
	token := os.Getenv("HAI_OPENCLAW_MAINTENANCE_TOKEN")
	h := &Handler{
		service: s, token: token, worker: "windows-openclaw-maintenance",
		enabled: maintenanceConfigEnabledFromEnvironment(),
	}
	if s != nil {
		s.canSafelyInstall = h.companionInstallationAvailable
		s.installCapabilityReason = h.installationBlocker
	}
	return h
}
func (h *Handler) Overview(c *gin.Context) {
	targets, err := h.service.Overview(c.Request.Context())
	if err != nil {
		c.JSON(503, gin.H{"error": "Maintenance records unavailable"})
		return
	}
	now := time.Now().UTC()
	companion := h.targetInstallationCapability("companion", now)
	gateway := h.targetInstallationCapability("gateway_core", now)
	capability := InstallationCapability{
		Available: companion.Available, Reason: companion.Reason,
		FreshFor: maintenanceWorkerCapabilityTTL.String(), Companion: companion, GatewayCore: gateway,
	}
	c.JSON(200, gin.H{"enabled": h.enabled, "worker": h.workerContactStatus(time.Now().UTC()), "installation": capability, "targets": targets})
}

func (h *Handler) targetInstallationCapability(target string, now time.Time) TargetInstallationCapability {
	if target == "gateway_core" {
		return TargetInstallationCapability{State: "blocked", Reason: installationBlockReason(target)}
	}
	if target != "companion" {
		return TargetInstallationCapability{State: "blocked", Reason: installationBlockReason(target)}
	}
	status := TargetInstallationCapability{State: "unknown"}
	if !h.enabled {
		status.State = "disabled"
		status.Reason = "OpenClaw maintenance is disabled."
		return status
	}
	h.workerMu.RLock()
	capability, observedAt := h.workerCapability, h.workerCapabilityAt
	h.workerMu.RUnlock()
	if observedAt.IsZero() {
		status.Reason = "No authenticated Windows containment capability has been received."
		return status
	}
	observedAt = observedAt.UTC()
	status.LastSeenAt = &observedAt
	if now.Before(observedAt) || now.Sub(observedAt) > maintenanceWorkerCapabilityTTL {
		status.State = "stale"
		status.Reason = "The authenticated Windows containment capability is stale; a fresh worker handshake is required."
		return status
	}
	if !capability.supportsCompanionInstall() {
		status.State = "unsupported"
		status.Reason = "The authenticated worker did not prove the required Windows Job Object containment capabilities."
		return status
	}
	status.Available = true
	status.State = "available"
	return status
}

func (h *Handler) companionInstallationAvailable() bool {
	return h.targetInstallationCapability("companion", time.Now().UTC()).Available
}

func (h *Handler) installationBlocker(target string) string {
	if target != "companion" {
		return installationBlockReason(target)
	}
	return h.targetInstallationCapability(target, time.Now().UTC()).Reason
}

func (h *Handler) workerContactStatus(now time.Time) WorkerContactStatus {
	status := WorkerContactStatus{State: "disabled", FreshFor: maintenanceWorkerContactTTL.String()}
	if !h.enabled {
		return status
	}
	h.workerMu.RLock()
	seen := h.workerLastSeen
	h.workerMu.RUnlock()
	if seen.IsZero() {
		status.State = "unknown"
		return status
	}
	seen = seen.UTC()
	status.LastSeenAt = &seen
	if !now.Before(seen) && now.Sub(seen) <= maintenanceWorkerContactTTL {
		status.State = "recent_contact"
	} else {
		status.State = "stale_contact"
	}
	return status
}

func (h *Handler) recordWorkerContact(capability WorkerCapabilityHandshake, supported bool) {
	h.workerMu.Lock()
	now := time.Now().UTC()
	h.workerLastSeen = now
	if !supported {
		capability = WorkerCapabilityHandshake{}
	}
	h.workerCapability = capability
	h.workerCapabilityAt = now
	h.workerMu.Unlock()
}
func decode(c *gin.Context, v any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		c.JSON(400, gin.H{"error": "Invalid maintenance request"})
		return false
	}
	return true
}
func (h *Handler) Action(c *gin.Context) {
	if !h.enabled {
		c.JSON(503, gin.H{"error": "Maintenance worker is not enabled"})
		return
	}
	var body struct {
		Action  string `json:"action"`
		Version string `json:"version"`
		Policy  string `json:"policy"`
	}
	if !decode(c, &body) {
		return
	}
	var err error
	switch body.Action {
	case "check":
		err = h.service.Check(c.Request.Context(), c.Param("target"))
	case "apply":
		err = h.service.Apply(c.Request.Context(), c.Param("target"), body.Version)
	case "policy":
		err = h.service.Policy(c.Request.Context(), c.Param("target"), body.Policy, c.GetString(identity.ContextSubjectKey))
	case "review":
		err = h.service.Review(c.Request.Context(), c.Param("target"), c.GetString(identity.ContextSubjectKey))
	default:
		c.JSON(400, gin.H{"error": "Unknown maintenance action"})
		return
	}
	if err != nil {
		if errors.Is(err, ErrUnattendedInstallationBlocked) {
			c.JSON(http.StatusConflict, gin.H{"error": installationBlockReason(c.Param("target"))})
			return
		}
		if errors.Is(err, ErrGatewayCoreInstallationBlocked) || errors.Is(err, ErrCompanionCapabilityRequired) {
			c.JSON(http.StatusConflict, gin.H{"error": h.installationBlocker(c.Param("target"))})
			return
		}
		c.JSON(409, gin.H{"error": "Action unavailable: check policy, release verification, active sessions and emergency stop"})
		return
	}
	c.Status(http.StatusAccepted)
}
func (h *Handler) WorkerAuth(c *gin.Context) {
	if !h.enabled || len(h.token) < 32 {
		c.AbortWithStatus(503)
		return
	}
	provided, bearer := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
	if !bearer || subtle.ConstantTimeCompare([]byte(provided), []byte(h.token)) != 1 {
		c.AbortWithStatus(401)
		return
	}
	capability, supported := capabilityFromHeader(c.GetHeader("X-HAI-Worker-Capability"))
	h.recordWorkerContact(capability, supported)
	c.Next()
}
func (h *Handler) Lease(c *gin.Context) {
	if err := h.service.Schedule(c.Request.Context()); err != nil {
		c.Status(503)
		return
	}
	lease, err := h.service.Lease(c.Request.Context(), h.worker)
	if err != nil {
		c.Status(503)
		return
	}
	if lease == nil {
		c.Status(204)
		return
	}
	c.JSON(200, lease)
}
func (h *Handler) Confirm(c *gin.Context) {
	var body struct {
		Token string `json:"leaseToken"`
	}
	if !decode(c, &body) {
		return
	}
	if err := h.service.Confirm(c.Request.Context(), c.Param("id"), h.worker, body.Token); err != nil {
		c.Status(409)
		return
	}
	c.Status(204)
}
func (h *Handler) Complete(c *gin.Context) {
	var body struct {
		Token  string `json:"leaseToken"`
		Report Report `json:"report"`
	}
	if !decode(c, &body) {
		return
	}
	if err := h.service.Complete(c.Request.Context(), c.Param("id"), h.worker, body.Token, body.Report); err != nil {
		c.Status(409)
		return
	}
	status, err := h.service.ReceiptStatus(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Status(503)
		return
	}
	c.JSON(http.StatusOK, ReceiptAcknowledgement{Status: status})
}
func (h *Handler) Permit(c *gin.Context) {
	var body struct {
		Token string `json:"leaseToken"`
	}
	if !decode(c, &body) {
		return
	}
	if err := h.service.Permit(c.Request.Context(), c.Param("id"), h.worker, body.Token); err != nil {
		c.Status(409)
		return
	}
	c.Status(204)
}
func StartScheduler(ctx context.Context, s *Service) error {
	if !maintenanceConfigEnabledFromEnvironment() {
		return nil
	}
	repo, err := durablejob.DefaultRepository()
	if err != nil {
		return err
	}
	runner := durablejob.NewRunner(repo, durablejob.Options{Queue: "openclaw-maintenance"})
	if err := runner.RegisterRecurring("openclaw-maintenance.check", maintenanceRecoveryScanInterval, 3, s.Schedule); err != nil {
		return err
	}
	// Each deadline is handled by an in-process timer. The durable recurring
	// scan plus queue polling remains the cross-restart recovery path: at worst,
	// a missed due deadline is recovered within 15 minutes + one 5-minute queue
	// poll, excluding database outages, process suspension, and scheduler load.
	if !lifecycle.Go(ctx, "openclaw-maintenance-worker", func() { runner.Start(ctx, maintenanceRunnerPollInterval) }) ||
		!lifecycle.Go(ctx, "openclaw-maintenance-deadlines", func() { s.runScheduledChecks(ctx) }) {
		return context.Canceled
	}
	return nil
}
