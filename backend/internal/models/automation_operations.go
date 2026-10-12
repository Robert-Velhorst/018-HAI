package models

import (
	"encoding/json"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"time"
)

type AutomationHealthEvent struct {
	ID                  uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	AutomationID        uuid.UUID `gorm:"type:uuid;index" json:"automationId"`
	Status              string    `gorm:"type:varchar(30);index" json:"status"`
	CheckType           string    `gorm:"type:varchar(50)" json:"checkType"`
	Target              string    `gorm:"type:varchar(1024)" json:"target,omitempty"`
	LatencyMs           int64     `gorm:"default:0" json:"latencyMs"`
	FailureReason       string    `gorm:"type:text" json:"failureReason,omitempty"`
	ConsecutiveFailures int       `gorm:"default:0" json:"consecutiveFailures"`
	CheckedAt           time.Time `gorm:"index" json:"checkedAt"`
}

type AutomationLaunchEvent struct {
	ID                   uuid.UUID                    `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	AutomationID         uuid.UUID                    `gorm:"type:uuid;index" json:"automationId"`
	OwnerIdentity        string                       `gorm:"type:varchar(255);index" json:"-"`
	RuntimeType          string                       `gorm:"type:varchar(50);index" json:"runtimeType,omitempty"`
	LaunchType           string                       `gorm:"type:varchar(50);index" json:"launchType"`
	RuntimeTaskID        string                       `gorm:"type:varchar(120);index" json:"runtimeTaskId,omitempty"`
	ExecutionReference   string                       `gorm:"type:varchar(120);index" json:"executionReference,omitempty"`
	EventKey             string                       `gorm:"type:varchar(120);index" json:"-"`
	Target               string                       `gorm:"type:varchar(1024)" json:"target,omitempty"`
	Status               string                       `gorm:"type:varchar(30);index" json:"status"`
	Message              string                       `gorm:"type:text" json:"message,omitempty"`
	Output               string                       `gorm:"type:text" json:"output,omitempty"`
	AuditLog             string                       `gorm:"column:audit_events;type:text" json:"-"`
	AuditEvents          []string                     `gorm:"-" json:"auditEvents,omitempty"`
	RuntimeRouteTraceLog string                       `gorm:"column:runtime_route_trace;type:text" json:"-"`
	RuntimeRouteTrace    *AutomationRuntimeRouteTrace `gorm:"-" json:"runtimeRouteTrace,omitempty"`
	ExitCode             int                          `gorm:"default:0" json:"exitCode"`
	DurationMs           int64                        `gorm:"default:0" json:"durationMs"`
	RequiresApproval     bool                         `gorm:"not null;default:false" json:"requiresApproval"`
	StartedAt            time.Time                    `gorm:"index" json:"startedAt"`
	CompletedAt          time.Time                    `gorm:"index" json:"completedAt"`
}

type AutomationRuntimeRouteTrace struct {
	RuntimeID           string   `json:"runtimeId"`
	Intent              string   `json:"intent,omitempty"`
	ExecutionMode       string   `json:"executionMode,omitempty"`
	RiskLevel           string   `json:"riskLevel,omitempty"`
	RecommendedSkills   []string `json:"recommendedSkills,omitempty"`
	VisibleProviders    []string `json:"visibleProviders,omitempty"`
	VisibleTools        []string `json:"visibleTools,omitempty"`
	RelevantMaps        []string `json:"relevantMaps,omitempty"`
	BlockedSurfaces     []string `json:"blockedSurfaces,omitempty"`
	RequiredControls    []string `json:"requiredControls,omitempty"`
	ValidationChecklist []string `json:"validationChecklist,omitempty"`
}

func (e *AutomationLaunchEvent) BeforeSave(_ *gorm.DB) error {
	if e == nil {
		return nil
	}
	if len(e.AuditEvents) == 0 {
		e.AuditLog = ""
	} else {
		payload, err := json.Marshal(e.AuditEvents)
		if err != nil {
			return err
		}
		e.AuditLog = string(payload)
	}

	if e.RuntimeRouteTrace == nil {
		e.RuntimeRouteTraceLog = ""
		return nil
	}
	payload, err := json.Marshal(e.RuntimeRouteTrace)
	if err != nil {
		return err
	}
	e.RuntimeRouteTraceLog = string(payload)
	return nil
}

func (e *AutomationLaunchEvent) AfterFind(_ *gorm.DB) error {
	if e == nil || e.AuditLog == "" {
	} else {
		var events []string
		if err := json.Unmarshal([]byte(e.AuditLog), &events); err == nil {
			e.AuditEvents = events
		}
	}
	if e.RuntimeRouteTraceLog != "" {
		var trace AutomationRuntimeRouteTrace
		if err := json.Unmarshal([]byte(e.RuntimeRouteTraceLog), &trace); err == nil {
			e.RuntimeRouteTrace = &trace
		}
	}
	return nil
}

type AutomationDependency struct {
	ID            uuid.UUID  `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	AutomationID  uuid.UUID  `gorm:"type:uuid;index" json:"automationId"`
	Name          string     `gorm:"type:varchar(255);not null" json:"name"`
	Kind          string     `gorm:"type:varchar(50);not null" json:"kind"`
	Target        string     `gorm:"type:varchar(1024)" json:"target,omitempty"`
	Required      bool       `gorm:"default:true" json:"required"`
	Status        string     `gorm:"type:varchar(30);default:'unknown'" json:"status"`
	LastCheckedAt *time.Time `json:"lastCheckedAt,omitempty"`
	Notes         string     `gorm:"type:text" json:"notes,omitempty"`
}

type AutomationRouteCheck struct {
	ID             uuid.UUID  `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	AutomationID   uuid.UUID  `gorm:"type:uuid;index" json:"automationId"`
	ExpectedRoute  string     `gorm:"type:varchar(255)" json:"expectedRoute"`
	ExpectedHost   string     `gorm:"type:varchar(255)" json:"expectedHost,omitempty"`
	ExpectedPort   int        `json:"expectedPort,omitempty"`
	ExpectedStatus int        `gorm:"default:200" json:"expectedStatus,omitempty"`
	Status         string     `gorm:"type:varchar(30);default:'unknown'" json:"status"`
	FailureReason  string     `gorm:"type:text" json:"failureReason,omitempty"`
	LastCheckedAt  *time.Time `json:"lastCheckedAt,omitempty"`
}

type AutomationAlert struct {
	ID             uuid.UUID  `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	AutomationID   uuid.UUID  `gorm:"type:uuid;index" json:"automationId"`
	Severity       string     `gorm:"type:varchar(30);index" json:"severity"`
	Title          string     `gorm:"type:varchar(255);not null" json:"title"`
	Message        string     `gorm:"type:text" json:"message"`
	Status         string     `gorm:"type:varchar(30);default:'open';index" json:"status"`
	FirstSeenAt    time.Time  `gorm:"index" json:"firstSeenAt"`
	LastSeenAt     time.Time  `gorm:"index" json:"lastSeenAt"`
	AcknowledgedAt *time.Time `json:"acknowledgedAt,omitempty"`
	ResolvedAt     *time.Time `json:"resolvedAt,omitempty"`
}

type AutomationIncident struct {
	ID             uuid.UUID  `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	AutomationID   uuid.UUID  `gorm:"type:uuid;index" json:"automationId"`
	Title          string     `gorm:"type:varchar(255);not null" json:"title"`
	Severity       string     `gorm:"type:varchar(30);index" json:"severity"`
	Status         string     `gorm:"type:varchar(30);default:'open';index" json:"status"`
	StartedAt      time.Time  `gorm:"index" json:"startedAt"`
	ResolvedAt     *time.Time `json:"resolvedAt,omitempty"`
	RootCause      string     `gorm:"type:text" json:"rootCause,omitempty"`
	ResolutionNote string     `gorm:"type:text" json:"resolutionNote,omitempty"`
}

type AutomationSLO struct {
	ID                     uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	AutomationID           uuid.UUID `gorm:"type:uuid;index" json:"automationId"`
	AvailabilityTargetPct  float64   `gorm:"default:99" json:"availabilityTargetPct"`
	MaxLatencyMs           int64     `gorm:"default:5000" json:"maxLatencyMs"`
	MaxConsecutiveFailures int       `gorm:"default:3" json:"maxConsecutiveFailures"`
	MonitoringWindowHours  int       `gorm:"default:24" json:"monitoringWindowHours"`
	Notes                  string    `gorm:"type:text" json:"notes,omitempty"`
}
