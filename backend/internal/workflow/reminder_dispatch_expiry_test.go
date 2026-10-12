package workflow

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

type expiringReminderDispatchRepository struct {
	*reminderDeliveryFakeRepo
	fixture historicalReminderFixture
	expires time.Time
}

func (r *expiringReminderDispatchRepository) FindDueReminderDeliveryAuthorizations(owner string, now time.Time, limit, maxAttempts int) ([]reminderDeliveryCandidate, error) {
	// Set expiry after the sweep's selection time, then hold revalidation until
	// it elapses. The regression cannot pass merely because selection was slow.
	r.expires = time.Now().UTC().Add(20 * time.Millisecond)
	authorization := r.fixture.authorization
	authorization.ExpiresAt = r.expires
	var err error
	authorization.RecordDigest, err = digestReminderActivationPayload(&authorization)
	if err != nil {
		return nil, err
	}
	r.authorizations[authorization.ID] = authorization
	return r.reminderDeliveryFakeRepo.FindDueReminderDeliveryAuthorizations(owner, now, limit, maxAttempts)
}

func (r *expiringReminderDispatchRepository) LoadReminderActivationSourceForOwner(owner string, checklistID uuid.UUID) (*WorkflowReminderCandidate, error) {
	time.Sleep(time.Until(r.expires.Add(time.Millisecond)))
	return r.reminderDeliveryFakeRepo.LoadReminderActivationSourceForOwner(owner, checklistID)
}

func TestReminderDeliveryRechecksExpiryAfterSlowRevalidation(t *testing.T) {
	f := newHistoricalReminderFixture(t, time.Now().UTC().Add(-3*time.Minute))
	repo := &expiringReminderDispatchRepository{reminderDeliveryFakeRepo: newReminderDeliveryFakeRepo(), fixture: f}
	f.seed(repo.reminderDeliveryFakeRepo)
	sink := &reminderDeliverySinkSpy{}
	configured, err := WithReminderDeliverySink(NewService(repo), sink)
	if err != nil {
		t.Fatal(err)
	}
	run, err := configured.(ReminderDeliveryService).RunDueReminderDeliveriesForOwner("alice", RunDueRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if run.Checked != 1 || run.Expired != 1 || run.Delivered != 0 || len(sink.deliveries) != 0 {
		t.Fatalf("expired grant reached the sink after slow revalidation: summary=%#v sink=%d", run, len(sink.deliveries))
	}
	attempts := repo.attempts[f.authorization.ID]
	if len(attempts) != 1 || attempts[0].Status != ReminderDeliveryStatusExpired || attempts[0].AttemptedAt.Before(repo.expires) {
		t.Fatalf("expiry receipt does not describe the actual dispatch time: attempts=%#v expiry=%s", attempts, repo.expires)
	}
}
