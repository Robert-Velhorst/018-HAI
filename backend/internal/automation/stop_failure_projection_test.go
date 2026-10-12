package automation

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

type stopFailureProjectionProbe struct {
	*fakeAutomationRepo
	fullWrites, summaryWrites int
	failSummary               bool
}

func (r *stopFailureProjectionProbe) Update(item *models.Automation) (*models.Automation, error) {
	r.fullWrites++
	return r.fakeAutomationRepo.Update(item)
}
func (r *stopFailureProjectionProbe) UpdateRuntimeStopFailure(id uuid.UUID, at time.Time, reason string) error {
	r.summaryWrites++
	if r.failSummary {
		return errors.New("private-database-details")
	}
	if r.automation.ID != id || at.IsZero() {
		return errors.New("invalid projection binding")
	}
	if r.automation.LastLaunchAt == nil || !at.Before(*r.automation.LastLaunchAt) {
		r.automation.LastFailureReason = reason
	}
	return nil
}

func TestStopFailureAuditDoesNotRewriteBorrowedConfiguration(t *testing.T) {
	for _, mode := range []string{"blocked", "failed", "cancellation_requested", "summary_write_failed", "newer_launch"} {
		t.Run(mode, func(t *testing.T) {
			id := uuid.New()
			borrowed := &models.Automation{ID: id, Host: "old-host", LaunchTarget: "old-target", RuntimeType: "openclaw"}
			before := *borrowed
			current := &models.Automation{ID: id, Host: "new-host", LaunchTarget: "new-target", RuntimeType: "openclaw"}
			if mode == "newer_launch" {
				at := time.Now().UTC().Add(time.Hour)
				current.LastLaunchAt, current.LastFailureReason = &at, "new launch failure"
			}
			probe := &stopFailureProjectionProbe{fakeAutomationRepo: newFakeAutomationRepo(current), failSummary: mode == "summary_write_failed"}
			status := mode
			if probe.failSummary {
				status = "failed"
			}
			if mode == "newer_launch" {
				status = "failed"
			}
			result := &agentruntime.StopResult{Status: status, Message: "stop was not confirmed"}
			_, err := newTestService(probe, events.Publisher{}).(*service).persistRuntimeStopEvent(borrowed, result, time.Now().UTC(), "alice")
			if err != nil || len(probe.launchEvents) != 1 || result.EvidenceURI == "" {
				t.Fatal("stop audit was not retained")
			}
			if probe.fullWrites != 0 || probe.automation.Host != "new-host" || probe.automation.LaunchTarget != "new-target" || !reflect.DeepEqual(*borrowed, before) {
				t.Fatal("stop failure rewrote configuration or mutated borrowed data")
			}
			want := 1
			if mode == "cancellation_requested" {
				want = 0
			}
			if probe.summaryWrites != want {
				t.Fatal("failure summary was not updated through the narrow projection")
			}
			if mode == "newer_launch" && probe.automation.LastFailureReason != "new launch failure" {
				t.Fatal("older stop overwrote the newer launch summary")
			}
			if probe.failSummary {
				audit := strings.Join(probe.launchEvents[0].AuditEvents, " ")
				if !strings.Contains(audit, "summary update was not confirmed") || strings.Contains(audit, "private-database-details") {
					t.Fatal("summary failure was hidden or leaked private diagnostics")
				}
			}
		})
	}
}
