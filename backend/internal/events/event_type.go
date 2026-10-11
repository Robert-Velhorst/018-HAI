package events

import (
	"automation-hub-backend/internal/models"
	"time"

	"github.com/google/uuid"
)

type AutomationEventType string

const (
	CreateEvent AutomationEventType = "create"
	UpdateEvent AutomationEventType = "update"
	DeleteEvent AutomationEventType = "delete"
)

type AutomationEvent struct {
	ID         uuid.UUID           `json:"eventId"`
	Type       AutomationEventType `json:"type"`
	Automation *models.Automation  `json:"automation"`
	OccurredAt time.Time           `json:"occurredAt"`
}

func normalizeAutomationEvent(event *AutomationEvent) error {
	if event == nil || event.Automation == nil || event.Automation.ID == uuid.Nil {
		return ErrInvalidEvent
	}
	switch event.Type {
	case CreateEvent, UpdateEvent, DeleteEvent:
	default:
		return ErrInvalidEvent
	}
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	} else {
		event.OccurredAt = event.OccurredAt.UTC()
	}
	return nil
}
