package openclawmaintenance

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const RuntimeID = "openclaw-maintenance"

const (
	providerMetadataMaxAge     = 15 * time.Minute
	providerClockSkewAllowance = 5 * time.Minute
)

var versionPattern = regexp.MustCompile(`^v?(\d{4})\.(\d{1,2})\.(\d{1,2})(?:-(\d+))?$`)

// OpenClaw numeric release suffixes are revisions, not SemVer prereleases.
func Newer(candidate, installed string) bool {
	a, okA := parseVersion(candidate)
	b, okB := parseVersion(installed)
	if !okA || !okB {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func ValidTarget(target string) bool { return target == "companion" || target == "gateway_core" }

func ValidVersion(version string) bool {
	_, ok := parseVersion(version)
	return ok
}

func parseVersion(version string) ([4]uint64, bool) {
	var parsed [4]uint64
	if len(version) > 40 {
		return parsed, false
	}
	parts := versionPattern.FindStringSubmatch(version)
	if parts == nil {
		return parsed, false
	}
	for i := 1; i < len(parts); i++ {
		if parts[i] == "" {
			continue
		}
		value, err := strconv.ParseUint(parts[i], 10, 64)
		if err != nil {
			return parsed, false
		}
		parsed[i-1] = value
	}
	if parsed[0] == 0 || parsed[1] < 1 || parsed[1] > 12 || parsed[2] < 1 {
		return parsed, false
	}
	dayLimit := time.Date(int(parsed[0]), time.Month(parsed[1])+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if parsed[2] > uint64(dayLimit) {
		return parsed, false
	}
	return parsed, true
}

type Target struct {
	ID                   string     `gorm:"primaryKey" json:"id"`
	Policy               string     `json:"policy"`
	Installed            string     `json:"installed"`
	Available            string     `json:"available"`
	State                string     `json:"state"`
	Reason               string     `json:"reason"`
	CheckedAt            *time.Time `json:"checkedAt"`
	NextCheck            time.Time  `json:"nextCheck"`
	CheckFailures        int        `json:"checkFailures"`
	VerifiedAt           *time.Time `json:"verifiedAt"`
	ReceiptID            string     `json:"receiptId"`
	UpdatedBy            string     `json:"-"`
	ReviewRequired       bool       `gorm:"-" json:"reviewRequired"`
	PendingKind          string     `gorm:"-" json:"pendingKind"`
	PendingStatus        string     `gorm:"-" json:"pendingStatus"`
	PendingStartedAt     *time.Time `gorm:"-" json:"pendingStartedAt"`
	InstallBlockedReason string     `gorm:"-" json:"installBlockedReason"`
	InstallStatus        string     `gorm:"-" json:"installStatus"`
}

func (Target) TableName() string { return "openclaw_maintenance_targets" }

type Job struct {
	ID          string     `gorm:"primaryKey" json:"id"`
	Target      string     `json:"target"`
	Kind        string     `json:"kind"`
	Version     string     `json:"version"`
	Status      string     `json:"status"`
	Evidence    string     `json:"evidence"`
	Worker      string     `json:"-"`
	LeaseDigest string     `json:"-"`
	LeaseUntil  *time.Time `json:"leaseUntil"`
	StartedAt   *time.Time `json:"startedAt"`
	CreatedAt   time.Time  `json:"createdAt"`
	FinishedAt  *time.Time `json:"finishedAt"`
	Result      string     `json:"-"`
}

func (Job) TableName() string { return "openclaw_maintenance_jobs" }

type Lease struct {
	Job   Job    `json:"job"`
	Token string `json:"leaseToken"`
}

type ReceiptAcknowledgement struct {
	Status string `json:"status"`
}

// Report contains normalized evidence only; upstream logs and paths stay local.
type Report struct {
	Installed          string     `json:"installed"`
	Available          string     `json:"available"`
	Evidence           string     `json:"evidence"`
	ProviderMetadataAt *time.Time `json:"providerMetadataAt,omitempty"`
	PublisherVerified  bool       `json:"publisherVerified"`
	PublisherPinStatus string     `json:"publisherPinStatus,omitempty"`
	ProcessTreeStatus  string     `json:"processTreeStatus,omitempty"`
	HealthOK           bool       `json:"healthOk"`
	Outcome            string     `json:"outcome"`
}

func providerMetadataFresh(metadataAt *time.Time, now time.Time) bool {
	if metadataAt == nil || metadataAt.IsZero() {
		return false
	}
	metadataTime := metadataAt.UTC()
	now = now.UTC()
	return !metadataTime.After(now.Add(providerClockSkewAllowance)) && now.Sub(metadataTime) <= providerMetadataMaxAge
}

func providerMetadataFreshAtReceipt(metadataAt *time.Time, receipt *Job) bool {
	return receipt != nil && receipt.FinishedAt != nil && providerMetadataFresh(metadataAt, *receipt.FinishedAt)
}

func ValidateReport(job Job, report Report) error {
	if !ValidTarget(job.Target) || (!ValidVersion(report.Installed) && !(report.Installed == "" && report.Outcome != "ok")) {
		return fmt.Errorf("installed version is missing or unsupported")
	}
	if report.Available != "" && !ValidVersion(report.Available) {
		return fmt.Errorf("available version is unsupported")
	}
	switch report.PublisherPinStatus {
	case "", "missing", "invalid", "configured", "mismatch", "verified":
	default:
		return fmt.Errorf("unknown publisher pin status")
	}
	if report.PublisherPinStatus == "verified" && !report.PublisherVerified {
		return fmt.Errorf("publisher pin status conflicts with verification result")
	}
	if report.ProcessTreeStatus != "" && report.ProcessTreeStatus != "verified" && report.ProcessTreeStatus != "unknown" {
		return fmt.Errorf("unknown process-tree termination status")
	}
	if report.ProcessTreeStatus != "" && (job.Target != "companion" || job.Kind != "apply") {
		return fmt.Errorf("process-tree status is only valid for Companion installation reports")
	}
	if report.Outcome == "ok" && (report.ProviderMetadataAt == nil || report.ProviderMetadataAt.IsZero()) {
		return fmt.Errorf("successful report is missing provider metadata evidence")
	}
	if job.Target == "companion" && report.PublisherVerified && report.PublisherPinStatus != "verified" {
		return fmt.Errorf("Companion publisher verification requires an explicit verified identity pin")
	}
	if len(report.Evidence) != 64 || strings.Trim(report.Evidence, "0123456789abcdef") != "" {
		return fmt.Errorf("invalid evidence digest")
	}
	switch report.Outcome {
	case "ok", "unavailable", "verification_failed", "update_failed", "needs_review":
	default:
		return fmt.Errorf("unknown outcome")
	}
	if job.Kind == "apply" && report.Outcome == "ok" && (!report.PublisherVerified || report.Installed != job.Version || !report.HealthOK || report.Evidence != job.Evidence) {
		return fmt.Errorf("update version, publisher or health verification failed")
	}
	if job.Target == "companion" && job.Kind == "apply" && report.Outcome == "ok" && report.ProcessTreeStatus != "verified" {
		return fmt.Errorf("successful Companion installation requires verified process-tree termination")
	}
	return nil
}
