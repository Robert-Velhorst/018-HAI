// Package a2abridge implements a deliberately small subset of the Agent2Agent
// protocol. It gives one named peer a non-executable HAI planning draft without
// exposing sources, memory, credentials, workflow mutation, or runtime controls.
package a2abridge

import (
	"crypto/subtle"
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"automation-hub-backend/internal/task"
)

const (
	enabledEnv = "HAI_A2A_BRIDGE_ENABLED"
	tokenEnv   = "HAI_A2A_BRIDGE_TOKEN"
	ownerEnv   = "HAI_A2A_BRIDGE_OWNER_ID"
	urlEnv     = "HAI_A2A_BRIDGE_URL"
	publicEnv  = "HAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED"
	ngrokEnv   = "HAI_NGROK_URL"
	runModeEnv = "RUN_MODE"
	bypassEnv  = "LOCAL_LOGIN_BYPASS_ENABLED"
)

var (
	ErrUnavailable  = errors.New("A2A planning bridge is not configured")
	ErrInvalidInput = errors.New("A2A task input is invalid")
)

type Config struct {
	Enabled          bool
	Token            string
	OwnerID          string
	URL              string
	PublicNgrok      bool
	NgrokURL         string
	RunMode          string
	LocalLoginBypass bool
}

type Status struct {
	Enabled      bool     `json:"enabled"`
	Configured   bool     `json:"configured"`
	Provider     string   `json:"provider"`
	Endpoint     string   `json:"endpoint,omitempty"`
	ConfigError  string   `json:"configError,omitempty"`
	Capabilities []string `json:"capabilities"`
	Restrictions []string `json:"restrictions"`
	Scope        string   `json:"scope"`
	Transport    string   `json:"transport"`
}

type AgentCard struct {
	Name                 string                `json:"name"`
	Description          string                `json:"description"`
	SupportedInterfaces  []AgentInterface      `json:"supportedInterfaces"`
	Version              string                `json:"version"`
	Capabilities         AgentCapabilities     `json:"capabilities"`
	DefaultInputModes    []string              `json:"defaultInputModes"`
	DefaultOutputModes   []string              `json:"defaultOutputModes"`
	Skills               []AgentSkill          `json:"skills"`
	SecuritySchemes      map[string]any        `json:"securitySchemes"`
	SecurityRequirements []map[string][]string `json:"securityRequirements"`
}

type AgentInterface struct {
	URL             string `json:"url"`
	ProtocolBinding string `json:"protocolBinding"`
	ProtocolVersion string `json:"protocolVersion"`
}

type AgentCapabilities struct {
	Streaming         bool `json:"streaming"`
	PushNotifications bool `json:"pushNotifications"`
	ExtendedAgentCard bool `json:"extendedAgentCard"`
}

type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Examples    []string `json:"examples"`
}

type Proposal struct {
	TaskType         string         `json:"taskType"`
	RiskLevel        string         `json:"riskLevel"`
	NeedsApproval    bool           `json:"needsApproval"`
	SuccessCriteria  []string       `json:"successCriteria"`
	Steps            []ProposalStep `json:"steps"`
	NextAction       string         `json:"nextAction"`
	CompletionStatus string         `json:"completionStatus"`
	Scope            string         `json:"scope"`
}

type ProposalStep struct {
	Name             string `json:"name"`
	Purpose          string `json:"purpose"`
	RequiresApproval bool   `json:"requiresApproval"`
	Status           string `json:"status"`
}

type Service struct {
	config  Config
	planner task.PreviewService
	err     string
}

func DefaultConfig() Config {
	return Config{
		Enabled:          envTrue(enabledEnv),
		Token:            strings.TrimSpace(os.Getenv(tokenEnv)),
		OwnerID:          strings.TrimSpace(os.Getenv(ownerEnv)),
		URL:              strings.TrimSpace(os.Getenv(urlEnv)),
		PublicNgrok:      envTrue(publicEnv),
		NgrokURL:         strings.TrimSpace(os.Getenv(ngrokEnv)),
		RunMode:          strings.TrimSpace(os.Getenv(runModeEnv)),
		LocalLoginBypass: envTrue(bypassEnv),
	}
}

func NewService(config Config, planner task.PreviewService) *Service {
	s := &Service{config: config, planner: planner}
	if !config.Enabled {
		return s
	}
	if len(config.Token) < 32 || strings.IndexFunc(config.Token, unicode.IsSpace) >= 0 {
		s.err = tokenEnv + " must contain at least 32 non-whitespace characters"
		return s
	}
	if !validOwner(config.OwnerID) {
		s.err = ownerEnv + " must contain one configured owner identity"
		return s
	}
	if err := validateBridgeURL(config); err != nil {
		s.err = err.Error()
		return s
	}
	if planner == nil {
		s.err = "side-effect-free HAI planning is unavailable"
	}
	return s
}

func NewServiceFromEnv(planner task.PreviewService) *Service {
	return NewService(DefaultConfig(), planner)
}

func (s *Service) Status() Status {
	transport := "local"
	provider := "A2A local planning bridge"
	peerScope := "local peer"
	if s.config.PublicNgrok {
		transport = "fixed_ngrok_https"
		provider = "A2A governed ngrok planning bridge"
		peerScope = "peer through the fixed governed ngrok endpoint"
	}
	return Status{
		Enabled:     s.config.Enabled,
		Configured:  s.configured(),
		Provider:    provider,
		Endpoint:    s.config.URL,
		ConfigError: s.err,
		Capabilities: []string{
			"authenticated non-executable planning drafts",
			"A2A 1.0-shaped Agent Card and SendMessage envelope",
			"one configured bearer-token peer and owner scope",
		},
		Restrictions: []string{
			"no workflow, task, source, memory, approval, policy, or runtime mutation",
			"no source evidence, memory records, credentials, raw audit records, files, or tool inventory exposure",
			"no streaming, task polling, push notifications, peer discovery, arbitrary remote URL, or outbound external-agent invocation",
		},
		Scope:     "One configured " + peerScope + " can request a bounded SendMessage planning draft for one configured owner. This limited endpoint is not a full A2A task-lifecycle server; HAI retains the authoritative workflow, approval, execution, source, memory, verification, and audit paths.",
		Transport: transport,
	}
}

func (s *Service) Authorize(token string) bool {
	if !s.configured() || len(token) != len(s.config.Token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.config.Token)) == 1
}

func (s *Service) AgentCard() (*AgentCard, error) {
	if !s.configured() {
		return nil, ErrUnavailable
	}
	return &AgentCard{
		Name:        "HAI controlled planning",
		Description: "Token-authenticated SendMessage planning drafts only. This limited bridge cannot execute, approve, mutate, or disclose HAI internals.",
		SupportedInterfaces: []AgentInterface{{
			URL: s.config.URL, ProtocolBinding: "JSONRPC", ProtocolVersion: "1.0",
		}},
		Version:            "1.0.2",
		Capabilities:       AgentCapabilities{Streaming: false, PushNotifications: false, ExtendedAgentCard: false},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"application/json"},
		Skills: []AgentSkill{{
			ID: "hai_controlled_planning", Name: "Controlled planning draft",
			Description: "Classifies a bounded text request and returns an advisory HAI planning proposal without taking operational action.",
			Tags:        []string{"planning", "local-first", "approval-gated", "read-only"},
			Examples:    []string{"Plan how to prepare a source-backed response without sending it."},
		}},
		SecuritySchemes: map[string]any{
			"haiBearer": map[string]any{"httpAuthSecurityScheme": map[string]string{"scheme": "Bearer", "description": "Dedicated HAI A2A bridge token."}},
		},
		SecurityRequirements: []map[string][]string{{"haiBearer": {}}},
	}, nil
}

func (s *Service) Draft(request string) (*Proposal, error) {
	if !s.configured() {
		return nil, ErrUnavailable
	}
	request = normalize(request)
	if request == "" || utf8.RuneCountInString(request) > 4096 {
		return nil, ErrInvalidInput
	}
	plan, err := s.planner.Preview(task.IntakeRequest{OwnerIdentity: s.config.OwnerID, Request: request})
	if err != nil {
		return nil, err
	}
	result := &Proposal{
		TaskType:         bounded(plan.Intake.TaskType, 80),
		RiskLevel:        bounded(plan.RiskAssessment.Level, 40),
		NeedsApproval:    plan.RiskAssessment.ApprovalRequired,
		SuccessCriteria:  boundedList(plan.Intake.SuccessCriteria, 6, 180),
		NextAction:       bounded(plan.ValidationResult.NextAction, 220),
		CompletionStatus: bounded(plan.CompletionStatus, 80),
		Scope:            "Planning draft only. It did not create a task, change a workflow, refresh a source, request approval, call a model, or execute a tool.",
	}
	for _, step := range plan.Steps {
		if len(result.Steps) == 8 {
			break
		}
		result.Steps = append(result.Steps, ProposalStep{
			Name: bounded(step.Name, 100), Purpose: bounded(step.Purpose, 180),
			RequiresApproval: step.RequiresApproval, Status: bounded(step.Status, 40),
		})
	}
	return result, nil
}

func (s *Service) configured() bool { return s.config.Enabled && s.err == "" && s.planner != nil }

func validOwner(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 255 && !strings.ContainsAny(value, "\r\n")
}

func validateBridgeURL(config Config) error {
	raw := strings.TrimSpace(config.URL)
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New(urlEnv + " must be a plain HTTP(S) URL without credentials, query data, or fragments")
	}
	if parsed.Path != "/api/v1/a2a" || parsed.RawPath != "" {
		return errors.New(urlEnv + " must use the exact /api/v1/a2a path")
	}
	host := strings.ToLower(parsed.Hostname())
	if isLocalHost(host) {
		if config.PublicNgrok {
			return errors.New(urlEnv + " must use the configured ngrok origin when " + publicEnv + " is enabled")
		}
		return nil
	}
	if !config.PublicNgrok {
		return errors.New(urlEnv + " must be local unless " + publicEnv + " is explicitly enabled")
	}
	if !strings.EqualFold(strings.TrimSpace(config.RunMode), "production") {
		return errors.New(runModeEnv + " must be production for the public A2A bridge")
	}
	if config.LocalLoginBypass {
		return errors.New(bypassEnv + " must be false for the public A2A bridge")
	}
	if parsed.Scheme != "https" || parsed.Port() != "" || !validNgrokHost(host) {
		return errors.New(urlEnv + " must use a fixed HTTPS ngrok hostname without a port")
	}
	origin, err := validateNgrokOrigin(config.NgrokURL)
	if err != nil {
		return err
	}
	if !strings.EqualFold(parsed.Scheme, origin.Scheme) || !strings.EqualFold(parsed.Host, origin.Host) {
		return errors.New(urlEnv + " must use the same origin as " + ngrokEnv)
	}
	return nil
}

func isLocalHost(host string) bool {
	if host == "localhost" || host == "host.docker.internal" || host == "gateway" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validateNgrokOrigin(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Port() != "" || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.Fragment != "" || !validNgrokHost(strings.ToLower(parsed.Hostname())) {
		return nil, errors.New(ngrokEnv + " must be a fixed HTTPS ngrok origin without credentials, port, path, query, or fragment")
	}
	return parsed, nil
}

func validNgrokHost(host string) bool {
	for _, suffix := range []string{".ngrok.app", ".ngrok.dev", ".ngrok-free.app", ".ngrok-free.dev"} {
		if len(host) > len(suffix) && strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

func envTrue(name string) bool { return strings.EqualFold(strings.TrimSpace(os.Getenv(name)), "true") }

func normalize(value string) string { return strings.Join(strings.Fields(value), " ") }

func bounded(value string, max int) string {
	value = normalize(value)
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	if max < 4 {
		return string(runes[:max])
	}
	return strings.TrimSpace(string(runes[:max-3])) + "..."
}

func boundedList(values []string, limit, width int) []string {
	result := make([]string, 0, limit)
	for _, value := range values {
		value = bounded(value, width)
		if value == "" {
			continue
		}
		result = append(result, value)
		if len(result) == limit {
			break
		}
	}
	return result
}
