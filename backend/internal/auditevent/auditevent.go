// Package auditevent builds structured, redaction-aware audit entries. Detail
// values are filtered using the shared known-credential policy.
package auditevent

import (
	"encoding/json"
	"strings"
	"time"

	"automation-hub-backend/internal/safety"
)

const redacted = "[redacted]"

// Event describes an audit entry. WithDetail returns an independent detail map.
type Event struct {
	At       time.Time         `json:"at"`
	Actor    string            `json:"actor"`
	Action   string            `json:"action"`
	Resource string            `json:"resource"`
	Result   string            `json:"result"`
	Details  map[string]string `json:"details,omitempty"`
}

// MarshalJSON applies redaction to every serialized field, including events
// assembled with a struct literal rather than New and WithDetail.
func (e Event) MarshalJSON() ([]byte, error) {
	public := e
	public.Actor = safety.RedactSecrets(public.Actor)
	public.Action = safety.RedactSecrets(public.Action)
	public.Resource = safety.RedactSecrets(public.Resource)
	public.Result = safety.RedactSecrets(public.Result)
	if e.Details != nil {
		public.Details = make(map[string]string, len(e.Details))
		for key, value := range e.Details {
			safeKey, safeValue := safeDetail(key, value)
			public.Details[safeKey] = safeValue
		}
	}
	type wireEvent Event
	return json.Marshal(wireEvent(public))
}

// New creates an audit event.
func New(at time.Time, actor, action, resource, result string) Event {
	return Event{At: at.UTC(), Actor: actor, Action: action, Resource: resource, Result: result}
}

// WithDetail returns a filtered copy without changing prior entries or sibling copies.
func (e Event) WithDetail(key, value string) Event {
	details := make(map[string]string, len(e.Details)+1)
	for existingKey, existingValue := range e.Details {
		safeKey, safeValue := safeDetail(existingKey, existingValue)
		details[safeKey] = safeValue
	}
	safeKey, safeValue := safeDetail(key, value)
	details[safeKey] = safeValue
	e.Details = details
	return e
}

func safeDetail(key, value string) (string, string) {
	safeKey := safety.RedactSecrets(key)
	if safeKey != key || safety.IsSensitiveKey(key) || strings.Contains(strings.ToLower(key), "private") {
		return safeKey, redacted
	}
	return safeKey, safety.RedactSecrets(value)
}
