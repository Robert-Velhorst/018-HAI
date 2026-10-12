package models

import (
	"automation-hub-backend/internal/safety"
	"fmt"
	"github.com/google/uuid"
	jsoniter "github.com/json-iterator/go"
	"mime/multipart"
	"strings"
	"time"
)

var JSON = jsoniter.ConfigCompatibleWithStandardLibrary

type Automation struct {
	ID          uuid.UUID             `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	Name        string                `gorm:"type:varchar(50);unique" json:"name,omitempty"`
	URLPath     string                `gorm:"type:varchar(255);unique" json:"urlPath,omitempty"`
	Image       string                `gorm:"type:varchar(255)" json:"image,omitempty"`
	Host        string                `gorm:"type:varchar(50)" json:"host,omitempty"`
	Port        int                   `gorm:"check:port >= 0 AND port <= 65535" json:"port,omitempty"`
	Position    int                   `gorm:"type:int;unique;check:position >= 0" json:"position,omitempty,omitinput"`
	ImageFile   *multipart.FileHeader `json:"imageFile,omitempty" gorm:"-"`
	RemoveImage bool                  `json:"removeImage,omitempty" gorm:"-"`
	OldUrlPath  string                `json:"oldUrlPath,omitempty" gorm:"-"`

	LaunchType                 string     `gorm:"type:varchar(50);default:'browser_url'" json:"launchType,omitempty"`
	LaunchTarget               string     `gorm:"type:varchar(1024)" json:"launchTarget,omitempty"`
	RuntimeType                string     `gorm:"type:varchar(50)" json:"runtimeType,omitempty"`
	RuntimeModel               string     `gorm:"type:varchar(255);default:''" json:"runtimeModel,omitempty"`
	ServiceName                string     `gorm:"type:varchar(255)" json:"serviceName,omitempty"`
	RoutePath                  string     `gorm:"type:varchar(255)" json:"routePath,omitempty"`
	PublicURL                  string     `gorm:"type:varchar(1024)" json:"publicUrl,omitempty"`
	LocalURL                   string     `gorm:"type:varchar(1024)" json:"localUrl,omitempty"`
	DependencyNotes            string     `gorm:"type:text" json:"dependencyNotes,omitempty"`
	HealthCheckType            string     `gorm:"type:varchar(50);default:'http'" json:"healthCheckType,omitempty"`
	HealthCheckURL             string     `gorm:"type:varchar(1024)" json:"healthCheckUrl,omitempty"`
	HealthCheckIntervalSeconds int        `gorm:"default:60" json:"healthCheckIntervalSeconds,omitempty"`
	ExpectedHTTPStatus         int        `gorm:"default:200" json:"expectedHttpStatus,omitempty"`
	Status                     string     `gorm:"type:varchar(30);default:'unknown'" json:"status,omitempty"`
	LastCheckedAt              *time.Time `json:"lastCheckedAt,omitempty"`
	LastSuccessAt              *time.Time `json:"lastSuccessAt,omitempty"`
	LastFailureAt              *time.Time `json:"lastFailureAt,omitempty"`
	LastFailureReason          string     `gorm:"type:text" json:"lastFailureReason,omitempty"`
	ConsecutiveFailures        int        `gorm:"default:0" json:"consecutiveFailures,omitempty"`
	AverageLatencyMs           int64      `gorm:"default:0" json:"averageLatencyMs,omitempty"`
	LastLaunchAt               *time.Time `json:"lastLaunchAt,omitempty"`
}

func (a *Automation) Validate() error {
	if err := safety.ValidateRuntimeModel(a.RuntimeType, a.RuntimeModel); err != nil {
		return err
	}
	if a.RuntimeModel != "" && strings.ToLower(strings.TrimSpace(a.LaunchType)) != "agent_runtime" {
		return fmt.Errorf("runtime model requires agent_runtime launch type")
	}
	if a.Name == "" {
		return fmt.Errorf("name is required")
	}
	if len(a.Name) > 50 {
		return fmt.Errorf("name is too long, maximum length is 50 characters")
	}
	if a.URLPath == "" {
		return fmt.Errorf("urlPath is required")
	}
	if len(a.URLPath) > 255 {
		return fmt.Errorf("urlPath is too long, maximum length is 255 characters")
	}
	if !safeAutomationSlug(a.URLPath) {
		return fmt.Errorf("urlPath must contain only letters, numbers, and hyphens")
	}
	if len(a.Image) > 255 {
		return fmt.Errorf("image name is too long, maximum length is 255 characters")
	}
	if a.Host == "" {
		return fmt.Errorf("hostname is required")
	}
	if len(a.Host) > 50 {
		return fmt.Errorf("hostname is too long, maximum length is 50 characters")
	}
	if !safeAutomationHost(a.Host) {
		return fmt.Errorf("hostname must be a DNS name or IPv4-style host without schemes, slashes, spaces, or template characters")
	}
	if a.Port <= 0 || a.Port > 65535 {
		return fmt.Errorf("error: Port %d is not valid", a.Port)
	}
	if a.Position < 0 {
		return fmt.Errorf("position cannot be negative")
	}
	return nil
}

func safeAutomationSlug(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "-") || strings.HasSuffix(value, "-") {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func safeAutomationHost(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") || strings.Contains(value, "..") {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}
