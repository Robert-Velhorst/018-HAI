// Package pydanticai bridges HAI to one optional, local PydanticAI structured
// proposal runner. It is deliberately not an agent runtime: it has no tools,
// no memory, no provider selection, no retries, and no execution authority.
package pydanticai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"unicode/utf8"

	"automation-hub-backend/internal/agentguidance"
)

const (
	enabledEnv              = "HAI_PYDANTIC_AI_ENABLED"
	baseURLEnv              = "HAI_PYDANTIC_AI_BASE_URL"
	timeoutEnv              = "HAI_PYDANTIC_AI_TIMEOUT_SECONDS"
	maxRequestChars         = 4000
	maxCriteria             = 8
	maxCriterionChars       = 240
	maxResponseBytes  int64 = 64 << 10
)

var (
	ErrNotConfigured  = errors.New("local PydanticAI proposal runner is not configured")
	ErrInvalidRequest = errors.New("invalid local PydanticAI proposal request")
)

type Status struct {
	Enabled      bool     `json:"enabled"`
	Configured   bool     `json:"configured"`
	Provider     string   `json:"provider"`
	Endpoint     string   `json:"endpoint,omitempty"`
	ConfigError  string   `json:"configError,omitempty"`
	Capabilities []string `json:"capabilities"`
	Restrictions []string `json:"restrictions"`
	Scope        string   `json:"scope"`
}

// Request contains only task-planning text supplied by the owner. HAI source
// records, credentials, model policy, and execution state are not accepted.
type Request struct {
	Request         string   `json:"request"`
	SuccessCriteria []string `json:"successCriteria,omitempty"`
}

// Proposal is a model-produced draft. HAI still validates it through its task
// engine and keeps all execution approval-gated.
type Proposal struct {
	Goal             string   `json:"goal"`
	SuccessCriteria  []string `json:"successCriteria"`
	NextSteps        []string `json:"nextSteps"`
	Risk             string   `json:"risk"`
	RequiresApproval bool     `json:"requiresApproval"`
	Reasons          []string `json:"reasons"`
	Uncertainties    []string `json:"uncertainties"`
}

type Response struct {
	Engine             string              `json:"engine"`
	ModelID            string              `json:"modelId"`
	RequestDigest      string              `json:"requestDigest"`
	Proposal           Proposal            `json:"proposal"`
	Scope              string              `json:"scope"`
	GuidanceStatus     string              `json:"guidanceStatus,omitempty"`
	AppliedBrainSkills []agentguidance.Pin `json:"appliedBrainSkills,omitempty"`
}

type runnerRequest struct {
	Request         string               `json:"request"`
	SuccessCriteria []string             `json:"successCriteria,omitempty"`
	Guidance        []agentguidance.Item `json:"guidance,omitempty"`
}

type ProbeResult struct {
	Reachable bool      `json:"reachable"`
	Engine    string    `json:"engine,omitempty"`
	ModelID   string    `json:"modelId,omitempty"`
	CheckedAt time.Time `json:"checkedAt"`
	Scope     string    `json:"scope"`
}

type Service interface {
	Status() Status
	Probe(context.Context) (*ProbeResult, error)
	Propose(context.Context, Request) (*Response, error)
}

// ModelMaintenanceGate admits one exact local provider/model pair through
// HAI's canonical daily freshness and execution policy.
type ModelMaintenanceGate interface {
	EnsureConfiguredLocalModel(endpointURL, modelID string) error
}

type service struct {
	enabled         bool
	baseURL         *url.URL
	configErr       string
	client          *http.Client
	now             func() time.Time
	maintenanceGate ModelMaintenanceGate
}

func DefaultService() Service {
	timeout := 20 * time.Second
	if raw := strings.TrimSpace(os.Getenv(timeoutEnv)); raw != "" {
		if parsed, err := time.ParseDuration(raw + "s"); err == nil && parsed > 0 && parsed <= 60*time.Second {
			timeout = parsed
		}
	}
	return NewService(strings.EqualFold(strings.TrimSpace(os.Getenv(enabledEnv)), "true"), os.Getenv(baseURLEnv), timeout, nil)
}

func NewService(enabled bool, rawBaseURL string, timeout time.Duration, client *http.Client) Service {
	if timeout <= 0 || timeout > 60*time.Second {
		timeout = 20 * time.Second
	}
	if client == nil {
		client = &http.Client{
			Timeout:       timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
			Transport:     &http.Transport{Proxy: nil},
		}
	}
	s := &service{enabled: enabled, client: client, now: time.Now}
	if enabled {
		s.baseURL, s.configErr = parseLocalBaseURL(rawBaseURL)
	}
	return s
}

// WithModelMaintenance prevents this optional runner from generating with a
// model outside HAI's canonical local model policy.
func WithModelMaintenance(delegate Service, gate ModelMaintenanceGate) Service {
	if configured, ok := delegate.(*service); ok {
		configured.maintenanceGate = gate
	}
	return delegate
}

func (s *service) Status() Status {
	status := Status{
		Enabled:     s.enabled,
		Configured:  s.configured(),
		Provider:    "PydanticAI local structured proposal runner",
		ConfigError: s.configErr,
		Capabilities: []string{
			"one typed local-model task proposal",
			"Pydantic schema validation before HAI receives the draft",
			"local runner health and model reachability probe",
		},
		Restrictions: []string{
			"no tools, MCP, web search, file access, memory, persistence, retry loop, or model-provider selection",
			"no HAI source records, credentials, raw connected-account data, policy changes, approvals, or execution",
			"proposal remains unverified until HAI task validation and human approval gates complete",
		},
		Scope: "Operator-configured local PydanticAI proposal runner. It creates one bounded schema-validated planning draft; HAI retains routing, verification, audit, approval, and all side-effect authority.",
	}
	if s.baseURL != nil {
		status.Endpoint = s.baseURL.String()
	}
	return status
}

func (s *service) Probe(ctx context.Context) (*ProbeResult, error) {
	if !s.configured() {
		return nil, ErrNotConfigured
	}
	endpoint := s.endpoint("/v1/probe")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("could not create local PydanticAI probe request")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "HAI-PydanticAI-Proposal/1.0")
	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("local PydanticAI proposal runner is unavailable")
	}
	defer response.Body.Close()
	var body struct {
		Status string `json:"status"`
		Engine string `json:"engine"`
		Model  string `json:"modelId"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 4097)).Decode(&body) != nil || body.Status != "ok" || !validEngine(body.Engine) || !validBoundedText(body.Model, 160) {
		return nil, fmt.Errorf("local PydanticAI proposal runner did not pass its model probe")
	}
	return &ProbeResult{Reachable: true, Engine: body.Engine, ModelID: body.Model, CheckedAt: s.now().UTC(), Scope: "Local runner and configured model endpoint reachability only. It does not produce a proposal or authorize any HAI action."}, nil
}

func (s *service) Propose(ctx context.Context, input Request) (*Response, error) {
	return s.propose(ctx, input, nil)
}

// ProposeWithGuidance adds only current, request-matched HAI catalog guidance
// resolved from the authenticated owner's stored consent by the router.
func (s *service) ProposeWithGuidance(ctx context.Context, input Request, guidance []agentguidance.Item) (*Response, error) {
	if err := agentguidance.Validate("planning", input.Request, guidance); err != nil {
		return nil, err
	}
	return s.propose(ctx, input, guidance)
}

func (s *service) propose(ctx context.Context, input Request, guidance []agentguidance.Item) (*Response, error) {
	if !s.configured() {
		return nil, ErrNotConfigured
	}
	if err := validateRequest(input); err != nil {
		return nil, err
	}
	if err := s.ensureMaintainedModel(ctx); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(runnerRequest{Request: input.Request, SuccessCriteria: input.SuccessCriteria, Guidance: guidance})
	if err != nil {
		return nil, fmt.Errorf("could not encode local PydanticAI proposal request")
	}
	endpoint := s.endpoint("/v1/propose")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("could not create local PydanticAI proposal request")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "HAI-PydanticAI-Proposal/1.0")
	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("local PydanticAI proposal runner is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("local PydanticAI proposal runner returned an unsuccessful response")
	}
	var result Response
	if json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes)).Decode(&result) != nil || !validResponse(result, input) {
		return nil, fmt.Errorf("local PydanticAI proposal runner returned an invalid proposal")
	}
	result.Scope = s.Status().Scope
	if len(guidance) > 0 {
		result.GuidanceStatus = "applied"
		result.AppliedBrainSkills = agentguidance.Pins(guidance)
	}
	return &result, nil
}

func (s *service) ensureMaintainedModel(ctx context.Context) error {
	if s.maintenanceGate == nil {
		return fmt.Errorf("central daily model maintenance gate is unavailable")
	}
	endpoint := s.endpoint("/healthz")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return fmt.Errorf("could not create local PydanticAI runner status request")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "HAI-PydanticAI-Proposal/1.0")
	response, err := s.client.Do(request)
	if err != nil {
		return fmt.Errorf("local PydanticAI proposal runner is unavailable")
	}
	defer response.Body.Close()
	var body struct {
		Status        string `json:"status"`
		Configured    bool   `json:"configured"`
		Model         string `json:"modelId"`
		ModelEndpoint string `json:"modelEndpoint"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 4097)).Decode(&body) != nil || body.Status != "ok" || !body.Configured || !validBoundedText(body.Model, 160) || !validBoundedText(body.ModelEndpoint, 512) {
		return fmt.Errorf("local PydanticAI runner did not disclose one fixed local model configuration")
	}
	if err := s.maintenanceGate.EnsureConfiguredLocalModel(body.ModelEndpoint, body.Model); err != nil {
		return fmt.Errorf("local PydanticAI planning model is not admitted: %w", err)
	}
	return nil
}

func (s *service) configured() bool { return s.enabled && s.configErr == "" && s.baseURL != nil }

func (s *service) endpoint(path string) url.URL {
	endpoint := *s.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path
	return endpoint
}

func parseLocalBaseURL(raw string) (*url.URL, string) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, baseURLEnv + " must be a plain local HTTP(S) URL without credentials, query data, or fragments"
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "localhost" && host != "host.docker.internal" && host != "pydantic-ai-runner" && net.ParseIP(host) == nil {
		return nil, baseURLEnv + " must resolve to localhost, host.docker.internal, pydantic-ai-runner, or a literal local IP"
	}
	if ip := net.ParseIP(host); ip != nil && !ip.IsLoopback() && !ip.IsPrivate() {
		return nil, baseURLEnv + " must use a loopback or private-network IP"
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed, ""
}

func validateRequest(input Request) error {
	input.Request = strings.TrimSpace(input.Request)
	if !validBoundedText(input.Request, maxRequestChars) || strings.ContainsAny(input.Request, "\r\n") {
		return ErrInvalidRequest
	}
	if len(input.SuccessCriteria) > maxCriteria {
		return ErrInvalidRequest
	}
	for _, criterion := range input.SuccessCriteria {
		if !validBoundedText(criterion, maxCriterionChars) || strings.ContainsAny(criterion, "\r\n") {
			return ErrInvalidRequest
		}
	}
	return nil
}

func validResponse(result Response, input Request) bool {
	if !validEngine(result.Engine) || !validBoundedText(result.ModelID, 160) || result.RequestDigest != requestDigest(input) {
		return false
	}
	proposal := result.Proposal
	if !validBoundedText(proposal.Goal, 400) || !validRisk(proposal.Risk) || !validStringList(proposal.SuccessCriteria, 1, 8, 240) || !validStringList(proposal.NextSteps, 1, 8, 320) || !validStringList(proposal.Reasons, 1, 5, 320) || !validStringList(proposal.Uncertainties, 0, 5, 320) {
		return false
	}
	return true
}

func validRisk(value string) bool { return value == "low" || value == "medium" || value == "high" }

func validStringList(values []string, min, max, maxChars int) bool {
	if len(values) < min || len(values) > max {
		return false
	}
	for _, value := range values {
		if !validBoundedText(value, maxChars) || strings.ContainsAny(value, "\r\n") {
			return false
		}
	}
	return true
}

func validBoundedText(value string, limit int) bool {
	value = strings.TrimSpace(value)
	return value != "" && utf8.RuneCountInString(value) <= limit
}

func validEngine(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "pydantic-ai ") && len(value) <= 160
}

func requestDigest(input Request) string {
	encoded, _ := json.Marshal(input)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
