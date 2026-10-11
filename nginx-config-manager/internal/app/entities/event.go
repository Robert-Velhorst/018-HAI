package entities

type AutomationEventType string

const (
	CreateEvent AutomationEventType = "create"
	UpdateEvent AutomationEventType = "update"
	DeleteEvent AutomationEventType = "delete"
)

type AutomationEvent struct {
	ID         string              `json:"eventId"`
	Type       AutomationEventType `json:"type"`
	Automation *Automation         `json:"automation"`
	OccurredAt string              `json:"occurredAt,omitempty"`
}
