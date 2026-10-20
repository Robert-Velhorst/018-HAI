// Package hireai provides HAI with a bounded, read-only Hire.AI job search.
package hireai

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"

	"automation-hub-backend/internal/demomode"
)

const (
	urlEnv       = "HIRE_AI_JOBS_SEARCH_URL"
	tokenEnv     = "HIRE_AI_CONNECTOR_TOKEN"
	modeEnv      = "RUN_MODE"
	searchPath   = "/api/hai/jobs/search"
	maxRequest   = 16 << 10
	maxResponse  = 512 << 10
	defaultLimit = 10
)

var ErrUnavailable = errors.New("Hire.AI connector is unavailable")
var ErrInvalidRequest = errors.New("Hire.AI search request is invalid")

type Config struct {
	Enabled bool
	URL     string
	Token   string
	RunMode string
	Timeout time.Duration
}

type Status struct {
	Enabled      bool     `json:"enabled"`
	Configured   bool     `json:"configured"`
	Endpoint     string   `json:"endpoint,omitempty"`
	ConfigError  string   `json:"configError,omitempty"`
	Capabilities []string `json:"capabilities"`
	Restrictions []string `json:"restrictions"`
}

type Filters struct {
	Query                 string      `json:"query,omitempty"`
	Location              string      `json:"location,omitempty"`
	JobType               string      `json:"jobType,omitempty"`
	PlatformID            int64       `json:"platformId,omitempty"`
	SalaryRange           *[2]float64 `json:"salaryRange,omitempty"`
	SalaryCurrency        string      `json:"salaryCurrency,omitempty"`
	ExperienceLevel       string      `json:"experienceLevel,omitempty"`
	ApplicationProcess    string      `json:"applicationProcess,omitempty"`
	VisaSponsorshipOnly   *bool       `json:"visaSponsorshipOnly,omitempty"`
	OpenHiringSupportOnly *bool       `json:"openHiringSupportOnly,omitempty"`
	DiversityFriendlyOnly *bool       `json:"diversityFriendlyOnly,omitempty"`
	SalaryDisclosedOnly   *bool       `json:"salaryDisclosedOnly,omitempty"`
	PostedWithin          string      `json:"postedWithin,omitempty"`
}

type Cursor struct {
	PostedDate *time.Time `json:"postedDate"`
	CreatedAt  time.Time  `json:"createdAt"`
	ID         int64      `json:"id"`
}

type SearchRequest struct {
	Filters Filters `json:"filters"`
	Limit   int     `json:"limit"`
	Cursor  *Cursor `json:"cursor,omitempty"`
}

type Job struct {
	ID             int64      `json:"id"`
	Title          *string    `json:"title"`
	Company        *string    `json:"company"`
	Location       *string    `json:"location"`
	JobType        *string    `json:"jobType"`
	SalaryMin      *float64   `json:"salaryMin"`
	SalaryMax      *float64   `json:"salaryMax"`
	SalaryCurrency *string    `json:"salaryCurrency"`
	Skills         *string    `json:"skills"`
	Description    *string    `json:"description"`
	Requirements   *string    `json:"requirements"`
	PlatformID     int64      `json:"platformId"`
	PlatformName   *string    `json:"platformName"`
	SourceURL      *string    `json:"sourceUrl"`
	ApplicationURL *string    `json:"applicationUrl"`
	PostedAt       *time.Time `json:"postedAt"`
}

type Policy struct {
	RemoteOnly            bool   `json:"remoteOnly"`
	ListingSafety         string `json:"listingSafety"`
	CanonicalListingsOnly bool   `json:"canonicalListingsOnly"`
}

type SearchResult struct {
	GeneratedAt string  `json:"generatedAt"`
	Policy      Policy  `json:"policy"`
	Jobs        []Job   `json:"jobs"`
	NextCursor  *Cursor `json:"nextCursor"`
}

type Service struct {
	config      Config
	client      *http.Client
	endpoint    *url.URL
	configError string
}

func DefaultConfig() Config {
	timeout := 8 * time.Second
	if raw := strings.TrimSpace(os.Getenv("HIRE_AI_CONNECTOR_TIMEOUT")); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 && parsed <= 30*time.Second {
			timeout = parsed
		}
	}
	return Config{
		Enabled: strings.EqualFold(strings.TrimSpace(os.Getenv("HIRE_AI_CONNECTOR_ENABLED")), "true"),
		URL:     strings.TrimSpace(os.Getenv(urlEnv)),
		Token:   strings.TrimSpace(os.Getenv(tokenEnv)),
		RunMode: strings.TrimSpace(os.Getenv(modeEnv)),
		Timeout: timeout,
	}
}

func NewService(config Config, client *http.Client) *Service {
	if config.Timeout <= 0 || config.Timeout > 30*time.Second {
		config.Timeout = 8 * time.Second
	}
	s := &Service{config: config}
	if !config.Enabled {
		return s
	}
	endpoint, err := validateEndpoint(config.URL, config.RunMode)
	if err != nil {
		s.configError = err.Error()
		return s
	}
	if len(config.Token) < 32 || strings.IndexFunc(config.Token, func(r rune) bool { return r <= ' ' || r == 127 }) >= 0 {
		s.configError = tokenEnv + " must contain at least 32 non-whitespace characters"
		return s
	}
	s.endpoint = endpoint
	if client == nil {
		client = &http.Client{
			Timeout:       config.Timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
			Transport: &http.Transport{
				Proxy:                 nil,
				DialContext:           safeDialContext(config.RunMode, endpoint.Hostname()),
				TLSHandshakeTimeout:   3 * time.Second,
				ResponseHeaderTimeout: 4 * time.Second,
				IdleConnTimeout:       30 * time.Second,
				MaxIdleConns:          2,
				MaxIdleConnsPerHost:   1,
			},
		}
	} else {
		clientCopy := *client
		clientCopy.Timeout = config.Timeout
		clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
		client = &clientCopy
	}
	s.client = client
	return s
}

func NewServiceFromEnv() *Service { return NewService(DefaultConfig(), nil) }

func (s *Service) Status() Status {
	endpoint := ""
	if s.configured() {
		endpoint = s.endpoint.String()
	}
	return Status{
		Enabled:      s.config.Enabled,
		Configured:   s.configured(),
		Endpoint:     endpoint,
		ConfigError:  s.configError,
		Capabilities: []string{"bounded read-only remote job search"},
		Restrictions: []string{"no candidate profile or resume access", "no application submission, messaging, approval, billing, or workflow mutation", "remote-only, safety-cleared canonical listings", "one explicitly configured endpoint; production requires public HTTPS; no redirects"},
	}
}

func (s *Service) Search(ctx context.Context, input SearchRequest) (*SearchResult, error) {
	if !s.configured() {
		return nil, ErrUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateRequest(input); err != nil {
		return nil, err
	}
	if input.Limit == 0 {
		input.Limit = defaultLimit
	}
	body, err := json.Marshal(input)
	if err != nil || len(body) > maxRequest {
		return nil, ErrInvalidRequest
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, ErrUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+s.config.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "HAI-Hire-AI-Connector/1.0")
	response, err := s.client.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusBadRequest {
		return nil, ErrInvalidRequest
	}
	if response.StatusCode != http.StatusOK {
		return nil, ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if err != nil || len(data) > maxResponse {
		return nil, ErrUnavailable
	}
	var result SearchResult
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || !validResult(&result, input.Limit) || !validContinuation(&result, input) {
		return nil, ErrUnavailable
	}
	return &result, nil
}

// The API orders listings by posted date, then creation time, then ID, all
// descending; a continuation cursor must move strictly toward older entries.
func cursorAdvances(next, current *Cursor) bool {
	if next == nil {
		return true
	}
	if current == nil {
		return true
	}
	if current.PostedDate == nil {
		if next.PostedDate != nil {
			return false
		}
	} else if next.PostedDate == nil {
		return true
	} else if !next.PostedDate.Equal(*current.PostedDate) {
		return next.PostedDate.Before(*current.PostedDate)
	}
	if !next.CreatedAt.Equal(current.CreatedAt) {
		return next.CreatedAt.Before(current.CreatedAt)
	}
	return next.ID < current.ID
}

func validContinuation(result *SearchResult, request SearchRequest) bool {
	if result.NextCursor == nil {
		return true
	}
	if len(result.Jobs) == 0 || len(result.Jobs) != request.Limit || !cursorAdvances(result.NextCursor, request.Cursor) {
		return false
	}
	last := result.Jobs[len(result.Jobs)-1]
	if last.ID != result.NextCursor.ID {
		return false
	}
	if last.PostedAt == nil || result.NextCursor.PostedDate == nil {
		return last.PostedAt == nil && result.NextCursor.PostedDate == nil
	}
	return last.PostedAt.Equal(*result.NextCursor.PostedDate)
}

func (s *Service) configured() bool {
	return s.config.Enabled && s.configError == "" && s.endpoint != nil && s.client != nil
}

func validateRequest(input SearchRequest) error {
	if input.Limit < 0 || input.Limit > 25 || utf16Length(strings.TrimSpace(input.Filters.Query)) > 200 || utf16Length(strings.TrimSpace(input.Filters.Location)) > 200 || len(input.Filters.JobType) > 20 || len(input.Filters.SalaryCurrency) > 3 || len(input.Filters.ExperienceLevel) > 20 || len(input.Filters.ApplicationProcess) > 20 || len(input.Filters.PostedWithin) > 3 {
		return ErrInvalidRequest
	}
	if !oneOf(input.Filters.JobType, "", "all", "full-time", "part-time", "contract", "temporary") ||
		!oneOf(input.Filters.ExperienceLevel, "", "all", "entry", "junior", "mid", "senior", "lead", "executive") ||
		!oneOf(input.Filters.ApplicationProcess, "", "all", "greenhouse", "lever", "ashby", "smartrecruiters", "workday", "email", "other") ||
		!oneOf(input.Filters.PostedWithin, "", "all", "1", "3", "7", "30") {
		return ErrInvalidRequest
	}
	if input.Filters.PlatformID < 0 {
		return ErrInvalidRequest
	}
	if input.Filters.SalaryCurrency != "" && input.Filters.SalaryCurrency != "all" && (len(input.Filters.SalaryCurrency) != 3 || strings.ToUpper(input.Filters.SalaryCurrency) != input.Filters.SalaryCurrency || strings.IndexFunc(input.Filters.SalaryCurrency, func(r rune) bool { return r < 'A' || r > 'Z' }) >= 0) {
		return ErrInvalidRequest
	}
	if input.Filters.SalaryCurrency == "all" && input.Filters.SalaryRange != nil {
		return ErrInvalidRequest
	}
	if input.Filters.SalaryRange != nil && (input.Filters.SalaryCurrency == "" || input.Filters.SalaryRange[0] < 0 || input.Filters.SalaryRange[1] < input.Filters.SalaryRange[0] || input.Filters.SalaryRange[1] > 10_000_000) {
		return ErrInvalidRequest
	}
	if input.Cursor != nil && (input.Cursor.ID <= 0 || input.Cursor.CreatedAt.IsZero()) {
		return ErrInvalidRequest
	}
	return nil
}

func utf16Length(value string) int {
	length := 0
	for _, character := range value {
		if character > 0xFFFF {
			length += 2
		} else {
			length++
		}
	}
	return length
}

func validResult(result *SearchResult, limit int) bool {
	if result == nil || result.GeneratedAt == "" || len(result.GeneratedAt) > 64 || result.Policy != (Policy{RemoteOnly: true, ListingSafety: "clear", CanonicalListingsOnly: true}) || len(result.Jobs) > limit {
		return false
	}
	if _, err := time.Parse(time.RFC3339Nano, result.GeneratedAt); err != nil {
		return false
	}
	if result.NextCursor != nil && (result.NextCursor.ID <= 0 || result.NextCursor.CreatedAt.IsZero()) {
		return false
	}
	jobIDs := make(map[int64]struct{}, len(result.Jobs))
	for _, job := range result.Jobs {
		if job.ID <= 0 || job.PlatformID <= 0 || !validOptionalText(job.Title, 300) || !validOptionalText(job.Company, 200) || !validOptionalText(job.Location, 200) || !validOptionalText(job.JobType, 40) || !validOptionalText(job.SalaryCurrency, 10) || !validOptionalText(job.Skills, 500) || !validOptionalText(job.Description, 2000) || !validOptionalText(job.Requirements, 2000) || !validOptionalText(job.PlatformName, 200) || !validSalary(job.SalaryMin) || !validSalary(job.SalaryMax) || job.SalaryMin != nil && job.SalaryMax != nil && *job.SalaryMin > *job.SalaryMax || !validPublicHTTPS(job.SourceURL) || !validPublicHTTPS(job.ApplicationURL) {
			return false
		}
		if _, duplicate := jobIDs[job.ID]; duplicate {
			return false
		}
		jobIDs[job.ID] = struct{}{}
	}
	return true
}

func validSalary(value *float64) bool {
	return value == nil || !math.IsNaN(*value) && !math.IsInf(*value, 0) && *value >= 0 && *value <= 10_000_000
}

func validOptionalText(value *string, max int) bool {
	return value == nil || len(*value) <= max && !strings.ContainsAny(*value, "\x00\r")
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func validPublicHTTPS(value *string) bool {
	if value == nil {
		return true
	}
	u, err := url.Parse(*value)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || len(*value) > 2048 {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if isLocalNetworkName(host) {
		return false
	}
	if ip, err := netip.ParseAddr(host); err == nil && !publicUnicast(ip) {
		return false
	}
	return true
}

func validateEndpoint(raw, runMode string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Path != searchPath || u.RawPath != "" {
		return nil, fmt.Errorf("%s must be the exact Hire.AI jobs-search URL", urlEnv)
	}
	host := strings.ToLower(u.Hostname())
	ip, ipErr := netip.ParseAddr(host)
	if ipErr == nil {
		ip = ip.Unmap()
	}
	production := demomode.Parse(runMode).IsProduction()
	privateIP := ipErr == nil && (ip.IsLoopback() || ip.IsPrivate())
	localNetworkName := isLocalNetworkName(host)
	if production {
		if u.Scheme != "https" || u.Port() != "" || ipErr == nil || localNetworkName || strings.Contains(host, "..") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
			return nil, fmt.Errorf("%s must use a public HTTPS hostname without an explicit port", urlEnv)
		}
	} else if u.Scheme != "https" && u.Scheme != "http" || ipErr == nil && !privateIP || (privateIP || localNetworkName) && production {
		return nil, fmt.Errorf("%s must use HTTPS or an explicitly local development endpoint", urlEnv)
	} else if !privateIP && !localNetworkName && (u.Scheme != "https" || u.Port() != "") {
		return nil, fmt.Errorf("%s must use a public HTTPS hostname without an explicit port", urlEnv)
	}
	return u, nil
}

func safeDialContext(runMode, endpointHost string) func(context.Context, string, string) (net.Conn, error) {
	allowLocalNetwork := allowsLocalNetworkTarget(runMode, endpointHost)
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || !strings.EqualFold(host, endpointHost) {
			return nil, errors.New("Hire.AI connector refused an unexpected destination")
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("Hire.AI connector endpoint did not resolve")
		}
		var lastErr error
		for _, candidate := range ips {
			ip, ok := netip.AddrFromSlice(candidate.IP)
			if !ok {
				continue
			}
			ip = ip.Unmap()
			if allowLocalNetwork {
				if !ip.IsLoopback() && !ip.IsPrivate() {
					continue
				}
			} else if !publicUnicast(ip) {
				continue
			}
			connection, dialErr := (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			lastErr = dialErr
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, errors.New("Hire.AI connector refused a non-public network address")
	}
}

func allowsLocalNetworkTarget(runMode, endpointHost string) bool {
	return !demomode.Parse(runMode).IsProduction() && isLocalNetworkTarget(endpointHost)
}

func isLocalNetworkName(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "host.docker.internal" || host == "gateway" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return true
	}
	return false
}

func isLocalNetworkTarget(host string) bool {
	if isLocalNetworkName(host) {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && (ip.Unmap().IsLoopback() || ip.Unmap().IsPrivate())
}

var (
	globalIPv6Prefix      = netip.MustParsePrefix("2000::/3")
	nonPublicIPv4Prefixes = [...]netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("192.88.99.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("240.0.0.0/4"),
	}
	nonPublicIPv6Prefixes = [...]netip.Prefix{
		netip.MustParsePrefix("2001::/23"),
		netip.MustParsePrefix("2001:db8::/32"),
		netip.MustParsePrefix("2002::/16"),
		netip.MustParsePrefix("3fff::/20"),
		netip.MustParsePrefix("5f00::/16"),
	}
	globallyReachableIPv6Exceptions = [...]netip.Prefix{
		netip.MustParsePrefix("2001:1::/128"),
		netip.MustParsePrefix("2001:1::2/128"),
		netip.MustParsePrefix("2001:1::3/128"),
		netip.MustParsePrefix("2001:3::/32"),
		netip.MustParsePrefix("2001:4:112::/48"),
		netip.MustParsePrefix("2001:20::/28"),
		netip.MustParsePrefix("2001:30::/28"),
	}
)

func publicUnicast(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	if ip.Is6() {
		if !globalIPv6Prefix.Contains(ip) {
			return false
		}
		for _, prefix := range nonPublicIPv6Prefixes {
			if prefix.Contains(ip) {
				for _, exception := range globallyReachableIPv6Exceptions {
					if exception.Contains(ip) {
						return true
					}
				}
				return false
			}
		}
		return true
	}
	for _, prefix := range nonPublicIPv4Prefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

func constantTimeTokenMatch(expected, actual string) bool {
	if len(expected) != len(actual) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

func (s *Service) Authorize(token string) bool {
	return s.configured() && constantTimeTokenMatch(s.config.Token, token)
}
