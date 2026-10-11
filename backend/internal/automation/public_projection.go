package automation

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"
)

var ErrMaskedAutomationConfiguration = errors.New("masked automation configuration cannot replace credentials")

// Public copies never become execution configuration or approval inputs.
func publicAutomation(raw *models.Automation) *models.Automation {
	if raw == nil {
		return nil
	}
	public := *raw
	public.LaunchTarget = redactLaunchTarget(raw.LaunchTarget)
	public.PublicURL = safety.RedactURL(raw.PublicURL)
	public.LocalURL = safety.RedactURL(raw.LocalURL)
	public.HealthCheckURL = safety.RedactURL(raw.HealthCheckURL)
	public.DependencyNotes = safety.RedactSecrets(raw.DependencyNotes)
	public.LastFailureReason = safety.RedactSecrets(raw.LastFailureReason)
	public.ImageFile = nil
	return &public
}

func publicAutomations(raw []*models.Automation) []*models.Automation {
	if raw == nil {
		return nil
	}
	public := make([]*models.Automation, len(raw))
	for i, automation := range raw {
		public[i] = publicAutomation(automation)
	}
	return public
}

func publicDiagnostics(raw *DiagnosticResult) *DiagnosticResult {
	if raw == nil {
		return nil
	}
	public := *raw
	public.LaunchTarget = redactLaunchTarget(raw.LaunchTarget)
	public.HealthCheckTarget = safety.RedactURL(raw.HealthCheckTarget)
	public.LastFailureReason = safety.RedactSecrets(raw.LastFailureReason)
	if raw.RecentEvents != nil {
		public.RecentEvents = append([]models.AutomationHealthEvent{}, raw.RecentEvents...)
		for i := range public.RecentEvents {
			event := &public.RecentEvents[i]
			event.Target = safety.RedactURL(event.Target)
			event.FailureReason = safety.RedactSecrets(event.FailureReason)
		}
	}
	if raw.RecentLaunches != nil {
		public.RecentLaunches = append([]models.AutomationLaunchEvent{}, raw.RecentLaunches...)
		for i := range public.RecentLaunches {
			event := &public.RecentLaunches[i]
			event.Target = redactLaunchTarget(event.Target)
			event.Message = safety.RedactSecrets(event.Message)
			event.Output = safety.RedactSecrets(event.Output)
			event.AuditEvents = redactAuditEvents(event.AuditEvents)
			event.RuntimeRouteTrace = redactRuntimeRouteTrace(event.RuntimeRouteTrace)
			// These backing logs are not public and must not retain raw copies here.
			event.AuditLog = ""
			event.RuntimeRouteTraceLog = ""
		}
	}
	return &public
}

// The existing editor posts all visible fields. An exact current projection
// preserves the entire raw field (including userinfo, which has no mask).
// A different, unmasked value replaces it; a modified mask is never executable.
func preserveAutomationCredentials(incoming, current *models.Automation) error {
	fields := []struct {
		name   string
		value  *string
		raw    string
		redact func(string) string
	}{
		{"launchTarget", &incoming.LaunchTarget, current.LaunchTarget, redactLaunchTarget},
		{"publicUrl", &incoming.PublicURL, current.PublicURL, safety.RedactURL},
		{"localUrl", &incoming.LocalURL, current.LocalURL, safety.RedactURL},
		{"healthCheckUrl", &incoming.HealthCheckURL, current.HealthCheckURL, safety.RedactURL},
		{"dependencyNotes", &incoming.DependencyNotes, current.DependencyNotes, safety.RedactSecrets},
	}
	for _, field := range fields {
		if *field.value == field.redact(field.raw) {
			*field.value = field.raw
		} else if containsRedactionMarker(*field.value) {
			return fmt.Errorf("%w: %s must be the unchanged current display value or a complete unmasked replacement", ErrMaskedAutomationConfiguration, field.name)
		}
	}
	return nil
}

func rejectMaskedAutomationConfiguration(automation *models.Automation) error {
	for _, value := range []string{automation.LaunchTarget, automation.PublicURL, automation.LocalURL, automation.HealthCheckURL, automation.DependencyNotes} {
		if containsRedactionMarker(value) {
			return ErrMaskedAutomationConfiguration
		}
	}
	return nil
}

func containsRedactionMarker(value string) bool {
	decoded, err := url.QueryUnescape(value)
	if err == nil {
		value = decoded
	}
	return strings.Contains(strings.ToUpper(value), "[REDACTED")
}
