package events

import (
	"errors"
	"testing"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

func TestUnconfiguredPublisherFailsClosed(t *testing.T) {
	p := &Publisher{topic: "automation-events"}
	err := p.Publish(&AutomationEvent{
		Type:       CreateEvent,
		Automation: &models.Automation{ID: uuid.New()},
	})
	if err == nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unconfigured publisher error = %v, want ErrUnavailable", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("unconfigured publisher close should not error: %v", err)
	}
}

func TestPublisherRejectsMalformedEventEvenWhenKafkaIsDisabled(t *testing.T) {
	p := &Publisher{topic: "automation-events"}
	if err := p.Publish(&AutomationEvent{}); err == nil {
		t.Fatal("malformed event was accepted")
	}
}

func TestNonEmptyBrokersFiltersBlanks(t *testing.T) {
	got := nonEmptyBrokers([]string{"", "   ", "kafka1:9092", ""})
	if len(got) != 1 || got[0] != "kafka1:9092" {
		t.Fatalf("nonEmptyBrokers = %v, want [kafka1:9092]", got)
	}
}
