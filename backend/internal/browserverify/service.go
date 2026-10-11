// Package browserverify provides a deliberately narrow Playwright bridge for
// named, read-only local browser checks. It is not a browser-control runtime.
package browserverify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"automation-hub-backend/internal/models"
)

const (
	enabledEnv  = "HAI_PLAYWRIGHT_ENABLED"
	runnerEnv   = "HAI_PLAYWRIGHT_RUNNER_URL"
	tokenEnv    = "HAI_PLAYWRIGHT_RUNNER_TOKEN"
	profilesEnv = "HAI_PLAYWRIGHT_PROFILES"
)

var (
	ErrNotConfigured = errors.New("browser verification is not configured")
	ErrUnavailable   = errors.New("local browser verification worker is unavailable")
)

type Profile struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	URL          string `json:"url"`
	ExpectedPath string `json:"expectedPath,omitempty"`
}

type Status struct {
	Enabled     bool      `json:"enabled"`
	Configured  bool      `json:"configured"`
	RunnerURL   string    `json:"runnerUrl,omitempty"`
	Profiles    []Profile `json:"profiles"`
	ConfigError string    `json:"configError,omitempty"`
	Scope       string    `json:"scope"`
}

type Run struct {
	ID                 string     `json:"id"`
	ProfileID          string     `json:"profileId"`
	Status             string     `json:"status"`
	FinalPath          string     `json:"finalPath,omitempty"`
	PageTitle          string     `json:"pageTitle,omitempty"`
	Summary            string     `json:"summary"`
	StartedAt          time.Time  `json:"startedAt"`
	CompletedAt        *time.Time `json:"completedAt,omitempty"`
	WorkflowID         string     `json:"workflowId,omitempty"`
	WorkflowLinkStatus string     `json:"workflowLinkStatus,omitempty"`
	WorkflowLinkError  string     `json:"workflowLinkError,omitempty"`
}

// WorkflowLinker attaches a completed local browser check as a review signal.
// It cannot authorize work or transition a workflow to completion.
type WorkflowLinker interface {
	AttachBrowserVerification(ownerIdentity, workflowID, runID, profileID, status, finalPath, pageTitle, summary string) error
}

type config struct {
	enabled   bool
	runnerURL string
	token     string
	profiles  []Profile
}
type service struct {
	config    config
	configErr string
	repo      Repository
	client    *http.Client
	now       func() time.Time
	workflows WorkflowLinker
}

func DefaultService(workflows ...WorkflowLinker) *service {
	profiles, _ := parseProfiles(os.Getenv(profilesEnv))
	return NewService(DefaultRepository(), strings.EqualFold(strings.TrimSpace(os.Getenv(enabledEnv)), "true"), os.Getenv(runnerEnv), os.Getenv(tokenEnv), profiles, workflows...)
}

func NewService(repo Repository, enabled bool, runnerURL, token string, profiles []Profile, workflows ...WorkflowLinker) *service {
	s := &service{config: config{enabled: enabled, runnerURL: strings.TrimRight(strings.TrimSpace(runnerURL), "/"), token: strings.TrimSpace(token), profiles: profiles}, repo: repo, now: time.Now,
		client: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{Proxy: nil}}}
	if len(workflows) > 0 {
		s.workflows = workflows[0]
	}
	if enabled {
		s.configErr = validateConfig(s.config)
	}
	return s
}

func (s *service) Status() Status {
	return Status{Enabled: s.config.enabled, Configured: s.config.enabled && s.configErr == "", RunnerURL: s.config.runnerURL, Profiles: append([]Profile(nil), s.config.profiles...), ConfigError: s.configErr, Scope: "Named, read-only checks against configured local origins only. No clicks, forms, downloads, retained browser state, public origins, or external actions."}
}
func (s *service) Profiles() []Profile { return append([]Profile(nil), s.config.profiles...) }

func (s *service) Run(ctx context.Context, owner, profileID string) (*Run, error) {
	if strings.TrimSpace(owner) == "" {
		return nil, fmt.Errorf("owner identity is required")
	}
	if !s.config.enabled || s.configErr != "" {
		return nil, ErrNotConfigured
	}
	profile, ok := s.profile(profileID)
	if !ok {
		return nil, fmt.Errorf("browser verification profile is not configured")
	}
	now := s.now().UTC()
	record := &models.BrowserVerificationRun{OwnerIdentity: owner, ProfileID: profile.ID, Status: "running", Summary: "read-only local browser verification is running", StartedAt: now, CreatedAt: now, UpdatedAt: now}
	stored, err := s.repo.Create(record)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(profile)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.runnerURL+"/verify", bytes.NewReader(payload))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-HAI-Browser-Token", s.config.token)
	}
	if err != nil {
		return s.fail(stored, "could not create local browser verification request")
	}
	response, err := s.client.Do(req)
	if err != nil {
		return s.fail(stored, "local browser verification worker is unavailable")
	}
	defer response.Body.Close()
	var result struct {
		Status    string `json:"status"`
		FinalPath string `json:"finalPath"`
		PageTitle string `json:"pageTitle"`
		Summary   string `json:"summary"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(&result) != nil || (result.Status != "passed" && result.Status != "failed") {
		return s.fail(stored, "local browser verification worker returned an invalid result")
	}
	completed := s.now().UTC()
	stored.Status = result.Status
	stored.FinalPath = safePath(result.FinalPath)
	stored.PageTitle = bounded(result.PageTitle, 160)
	stored.Summary = bounded(result.Summary, 240)
	stored.CompletedAt = &completed
	stored.UpdatedAt = completed
	if stored.Summary == "" {
		stored.Summary = "read-only local browser verification completed"
	}
	if _, err = s.repo.Update(stored); err != nil {
		return nil, err
	}
	out := runFromModel(*stored)
	return &out, nil
}

// RunWithWorkflow performs the same read-only check and optionally attaches
// its bounded result to an owner-authorized workflow. A linkage failure does
// not hide or change the browser check result.
func (s *service) RunWithWorkflow(ctx context.Context, owner, profileID, workflowID string) (*Run, error) {
	run, runErr := s.Run(ctx, owner, profileID)
	workflowID = strings.TrimSpace(workflowID)
	if run == nil || workflowID == "" {
		return run, runErr
	}
	run.WorkflowID = workflowID
	if s.workflows == nil {
		run.WorkflowLinkStatus = "not_linked"
		run.WorkflowLinkError = "workflow linkage is unavailable"
		return run, runErr
	}
	if err := s.workflows.AttachBrowserVerification(owner, workflowID, run.ID, run.ProfileID, run.Status, run.FinalPath, run.PageTitle, run.Summary); err != nil {
		run.WorkflowLinkStatus = "link_failed"
		run.WorkflowLinkError = "browser verification completed but could not be linked to the requested workflow"
		return run, runErr
	}
	run.WorkflowLinkStatus = "linked_quality_signal"
	return run, runErr
}

func (s *service) fail(record *models.BrowserVerificationRun, summary string) (*Run, error) {
	completed := s.now().UTC()
	record.Status = "failed"
	record.Summary = summary
	record.CompletedAt = &completed
	record.UpdatedAt = completed
	_, _ = s.repo.Update(record)
	out := runFromModel(*record)
	return &out, ErrUnavailable
}
func (s *service) Runs(owner string, limit int) ([]Run, error) {
	records, err := s.repo.List(strings.TrimSpace(owner), limit)
	if err != nil {
		return nil, err
	}
	out := make([]Run, 0, len(records))
	for _, r := range records {
		out = append(out, runFromModel(r))
	}
	return out, nil
}
func (s *service) profile(id string) (Profile, bool) {
	for _, p := range s.config.profiles {
		if p.ID == strings.TrimSpace(id) {
			return p, true
		}
	}
	return Profile{}, false
}
func runFromModel(m models.BrowserVerificationRun) Run {
	return Run{ID: m.ID.String(), ProfileID: m.ProfileID, Status: m.Status, FinalPath: m.FinalPath, PageTitle: m.PageTitle, Summary: m.Summary, StartedAt: m.StartedAt, CompletedAt: m.CompletedAt}
}

func parseProfiles(raw string) ([]Profile, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var profiles []Profile
	if err := json.Unmarshal([]byte(raw), &profiles); err != nil {
		return nil, err
	}
	return profiles, nil
}
func validateConfig(c config) string {
	if c.runnerURL == "" || len(c.token) < 16 || len(c.profiles) == 0 {
		return "HAI_PLAYWRIGHT_RUNNER_URL, a 16+ character HAI_PLAYWRIGHT_RUNNER_TOKEN, and HAI_PLAYWRIGHT_PROFILES are required when HAI_PLAYWRIGHT_ENABLED=true"
	}
	if len(c.profiles) > 20 {
		return "HAI_PLAYWRIGHT_PROFILES may contain at most 20 named local checks"
	}
	if err := validateLocalURL(c.runnerURL); err != nil {
		return err.Error()
	}
	seen := map[string]bool{}
	for _, p := range c.profiles {
		if strings.TrimSpace(p.ID) == "" || seen[p.ID] || len(p.ID) > 80 {
			return "each browser verification profile needs a unique bounded id"
		}
		seen[p.ID] = true
		if err := validateProfile(p); err != nil {
			return err.Error()
		}
	}
	return ""
}
func validateProfile(p Profile) error {
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 120 {
		return errors.New("browser verification profile name is required and bounded")
	}
	if err := validateLocalURL(p.URL); err != nil {
		return fmt.Errorf("browser verification profile URL: %w", err)
	}
	u, _ := url.Parse(p.URL)
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return errors.New("browser verification profile URL cannot include credentials, query data, or fragments")
	}
	if p.ExpectedPath != "" && !strings.HasPrefix(p.ExpectedPath, "/") {
		return errors.New("browser verification expectedPath must start with /")
	}
	return nil
}
func validateLocalURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return errors.New("must be a local http(s) URL")
	}
	host := strings.ToLower(u.Hostname())
	if host == "browser-verifier" || host == "frontend" || host == "localhost" || host == "host.docker.internal" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return errors.New("may only target frontend, localhost, host.docker.internal, or a loopback IP")
}
func safePath(value string) string {
	u, err := url.Parse(value)
	if err != nil {
		return ""
	}
	return bounded(u.EscapedPath(), 240)
}
func bounded(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) > max {
		return value[:max]
	}
	return value
}
