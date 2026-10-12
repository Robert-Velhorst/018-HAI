package automation

import (
	"reflect"
	"testing"

	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

type stopConfigurationProbe struct {
	Repository
	configuration *models.Automation
	taskLookups   int
}

func (r *stopConfigurationProbe) FindByID(_ uuid.UUID) (*models.Automation, error) {
	return r.configuration, nil
}
func (r *stopConfigurationProbe) FindOwnerActiveRuntimeLaunch(_ uuid.UUID, _, _ string) (*models.AutomationLaunchEvent, error) {
	r.taskLookups++
	return nil, nil
}
func TestRuntimeStopRequiresExactConfigurationIdentity(t *testing.T) {
	for _, mode := range []string{"nil_record", "wrong_record", "nil_requested_id", "nil_repository", "valid_borrowed_record"} {
		t.Run(mode, func(t *testing.T) {
			defer func() {
				if value := recover(); value != nil {
					t.Errorf("runtime stop panicked on configuration: %T", value)
				}
			}()
			id := uuid.New()
			config := &models.Automation{ID: id, LaunchType: "agent_runtime", RuntimeType: "openclaw"}
			if mode == "wrong_record" {
				config.ID = uuid.New()
			}
			if mode == "nil_requested_id" {
				id = uuid.Nil
			}
			stored := *config
			base := newFakeAutomationRepo(&stored)
			probe := &stopConfigurationProbe{Repository: base, configuration: config}
			if mode == "nil_record" {
				probe.configuration = nil
			}
			var repository Repository = probe
			if mode == "nil_repository" {
				repository = nil
			}
			before := *config
			result, err := newTestService(repository, events.Publisher{}).StopRuntimeTaskForOwner(id, "alice")
			if mode == "valid_borrowed_record" {
				if err != nil || result == nil || result.Status != "blocked" || probe.taskLookups != 1 {
					t.Fatal("valid owner-scoped lookup failed")
				}
				if !reflect.DeepEqual(*config, before) {
					t.Fatal("runtime stop mutated borrowed configuration while applying defaults")
				}
			} else if err == nil || result != nil || probe.taskLookups != 0 || len(base.launchIntents) != 0 || len(base.launchEvents) != 0 {
				t.Fatal("invalid configuration reached task lookup or audit writes")
			}
		})
	}
}
