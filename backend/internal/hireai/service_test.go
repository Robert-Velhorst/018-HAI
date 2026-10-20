package hireai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testToken = "hire-ai-connector-test-token-with-over-32-characters"

func testService(t *testing.T, handler http.Handler) (*Service, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	service := NewService(Config{
		Enabled: true,
		URL:     server.URL + searchPath,
		Token:   testToken,
		RunMode: "demo",
		Timeout: 2 * time.Second,
	}, server.Client())
	if !service.configured() {
		server.Close()
		t.Fatalf("test service not configured: %s", service.configError)
	}
	t.Cleanup(server.Close)
	return service, server
}

func TestSearchSendsBoundedAuthenticatedRequestAndAcceptsRemoteOnlyResult(t *testing.T) {
	service, _ := testService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != searchPath {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
			t.Errorf("authorization header mismatch")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content type = %q", got)
		}
		var request SearchRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.Limit != 10 {
			t.Errorf("default limit = %d, want 10", request.Limit)
		}
		if request.Filters.SalaryRange != nil {
			t.Errorf("empty salary range was transmitted: %#v", request.Filters.SalaryRange)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"generatedAt":"2026-10-18T10:00:00Z","policy":{"remoteOnly":true,"listingSafety":"clear","canonicalListingsOnly":true},"jobs":[],"nextCursor":null}`))
	}))
	result, err := service.Search(context.Background(), SearchRequest{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if result.Policy.RemoteOnly != true || len(result.Jobs) != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestSearchPreservesHireAIFilterAndCursorContract(t *testing.T) {
	postedDate := time.Date(2026, 10, 17, 9, 30, 0, 0, time.UTC)
	createdAt := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	visaSponsorship := true
	openHiring := false
	diversityFriendly := true
	salaryDisclosed := false
	want := SearchRequest{
		Filters: Filters{
			Query:                 "senior engineer",
			Location:              "Europe",
			JobType:               "full-time",
			PlatformID:            42,
			SalaryRange:           &[2]float64{80000, 120000},
			SalaryCurrency:        "EUR",
			ExperienceLevel:       "senior",
			ApplicationProcess:    "greenhouse",
			VisaSponsorshipOnly:   &visaSponsorship,
			OpenHiringSupportOnly: &openHiring,
			DiversityFriendlyOnly: &diversityFriendly,
			SalaryDisclosedOnly:   &salaryDisclosed,
			PostedWithin:          "7",
		},
		Limit:  10,
		Cursor: &Cursor{PostedDate: &postedDate, CreatedAt: createdAt, ID: 123},
	}
	service, _ := testService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got SearchRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("forwarded request = %#v, want %#v", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"generatedAt":"2026-10-18T10:00:00Z","policy":{"remoteOnly":true,"listingSafety":"clear","canonicalListingsOnly":true},"jobs":[],"nextCursor":null}`))
	}))
	if _, err := service.Search(context.Background(), want); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
}

func TestSearchAcceptsSmartRecruitersApplicationProcessFilter(t *testing.T) {
	request := SearchRequest{Filters: Filters{ApplicationProcess: "smartrecruiters"}, Limit: 10}
	if err := validateRequest(request); err != nil {
		t.Fatalf("validateRequest() error = %v, want SmartRecruiters filter accepted", err)
	}
}

func TestSearchRejectsNonAdvancingNextCursor(t *testing.T) {
	createdAt := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	postedDate := time.Date(2026, 10, 17, 9, 30, 0, 0, time.UTC)
	cursor := &Cursor{PostedDate: &postedDate, CreatedAt: createdAt, ID: 123}
	service, _ := testService(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"generatedAt":"2026-10-18T10:00:00Z","policy":{"remoteOnly":true,"listingSafety":"clear","canonicalListingsOnly":true},"jobs":[],"nextCursor":{"postedDate":"2026-10-17T09:30:00Z","createdAt":"2026-10-18T10:00:00Z","id":123}}`))
	}))
	if _, err := service.Search(context.Background(), SearchRequest{Limit: 10, Cursor: cursor}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Search() error = %v, want ErrUnavailable for a repeated cursor", err)
	}
}

func TestCursorAdvancesFollowsHireAIOrdering(t *testing.T) {
	postedAt := time.Date(2026, 10, 17, 9, 30, 0, 0, time.UTC)
	olderPostedAt := postedAt.Add(-time.Hour)
	newerPostedAt := postedAt.Add(time.Hour)
	createdAt := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	olderCreatedAt := createdAt.Add(-time.Hour)
	current := &Cursor{PostedDate: &postedAt, CreatedAt: createdAt, ID: 123}
	olderCursor := func(posted *time.Time, created time.Time, id int64) *Cursor {
		return &Cursor{PostedDate: posted, CreatedAt: created, ID: id}
	}
	tests := []struct {
		name    string
		next    *Cursor
		current *Cursor
		want    bool
	}{
		{name: "older posting date", next: olderCursor(&olderPostedAt, createdAt, 999), want: true},
		{name: "null posting date follows dated listings", next: olderCursor(nil, createdAt, 999), want: true},
		{name: "older creation time on same posting date", next: olderCursor(&postedAt, olderCreatedAt, 999), want: true},
		{name: "lower ID on same timestamps", next: olderCursor(&postedAt, createdAt, 122), want: true},
		{name: "same cursor", next: olderCursor(&postedAt, createdAt, 123), want: false},
		{name: "newer posting date", next: olderCursor(&newerPostedAt, createdAt, 1), want: false},
		{name: "newer creation time", next: olderCursor(&postedAt, createdAt.Add(time.Hour), 1), want: false},
		{name: "higher ID on same timestamps", next: olderCursor(&postedAt, createdAt, 124), want: false},
		{name: "dated listing cannot follow null dates", next: olderCursor(&postedAt, olderCreatedAt, 1), current: olderCursor(nil, createdAt, 999), want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cursor := current
			if test.current != nil {
				cursor = test.current
			}
			if got := cursorAdvances(test.next, cursor); got != test.want {
				t.Fatalf("cursorAdvances() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestValidContinuationMustMatchLastResultAndAdvance(t *testing.T) {
	postedAt := time.Date(2026, 10, 17, 9, 30, 0, 0, time.UTC)
	newerPostedAt := postedAt.Add(time.Hour)
	forwardPostedAt := newerPostedAt.Add(time.Hour)
	createdAt := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	request := SearchRequest{
		Limit:  1,
		Cursor: &Cursor{PostedDate: &newerPostedAt, CreatedAt: createdAt.Add(time.Hour), ID: 124},
	}
	result := SearchResult{
		Jobs:       []Job{{ID: 123, PostedAt: &postedAt}},
		NextCursor: &Cursor{PostedDate: &postedAt, CreatedAt: createdAt, ID: 123},
	}
	if !validContinuation(&result, request) {
		t.Fatal("matching last-result cursor was rejected")
	}
	if !validContinuation(&SearchResult{}, request) {
		t.Fatal("terminal page without a continuation cursor was rejected")
	}

	invalid := []struct {
		name   string
		result SearchResult
	}{
		{
			name: "cursor ID does not match final job",
			result: SearchResult{
				Jobs:       []Job{{ID: 123, PostedAt: &postedAt}},
				NextCursor: &Cursor{PostedDate: &postedAt, CreatedAt: createdAt, ID: 122},
			},
		},
		{
			name: "cursor date does not match final job",
			result: SearchResult{
				Jobs:       []Job{{ID: 123, PostedAt: &postedAt}},
				NextCursor: &Cursor{PostedDate: &newerPostedAt, CreatedAt: createdAt, ID: 123},
			},
		},
		{
			name: "short page has a continuation cursor",
			result: SearchResult{
				NextCursor: &Cursor{PostedDate: &postedAt, CreatedAt: createdAt, ID: 123},
			},
		},
		{
			name: "cursor moves forward instead of older",
			result: SearchResult{
				Jobs:       []Job{{ID: 123, PostedAt: &forwardPostedAt}},
				NextCursor: &Cursor{PostedDate: &forwardPostedAt, CreatedAt: createdAt, ID: 123},
			},
		},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if validContinuation(&test.result, request) {
				t.Fatal("invalid continuation was accepted")
			}
		})
	}
}

func TestValidResultRejectsDuplicateJobIDs(t *testing.T) {
	result := &SearchResult{
		GeneratedAt: "2026-10-18T10:00:00Z",
		Policy:      Policy{RemoteOnly: true, ListingSafety: "clear", CanonicalListingsOnly: true},
		Jobs:        []Job{{ID: 10, PlatformID: 1}, {ID: 10, PlatformID: 2}},
	}
	if validResult(result, 10) {
		t.Fatal("duplicate canonical job IDs were accepted")
	}
}

func TestSearchRejectsUnsafePolicyAndDoesNotFollowRedirects(t *testing.T) {
	t.Run("unsafe policy", func(t *testing.T) {
		service, _ := testService(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"generatedAt":"2026-10-18T10:00:00Z","policy":{"remoteOnly":false,"listingSafety":"clear","canonicalListingsOnly":true},"jobs":[],"nextCursor":null}`))
		}))
		if _, err := service.Search(context.Background(), SearchRequest{}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("Search() error = %v, want ErrUnavailable", err)
		}
	})
	t.Run("redirect", func(t *testing.T) {
		var destinationHit bool
		destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { destinationHit = true; w.WriteHeader(http.StatusOK) }))
		defer destination.Close()
		service, origin := testService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, destination.URL, http.StatusFound)
		}))
		defer origin.Close()
		if _, err := service.Search(context.Background(), SearchRequest{}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("Search() error = %v, want ErrUnavailable", err)
		}
		if destinationHit {
			t.Fatal("client followed an untrusted redirect")
		}
	})
}

func TestConfigurationRejectsUntrustedDestinationsAndSecrets(t *testing.T) {
	cases := []struct {
		name   string
		config Config
	}{
		{name: "public http", config: Config{Enabled: true, URL: "http://hire-ai.example.com" + searchPath, Token: testToken, RunMode: "production"}},
		{name: "production loopback", config: Config{Enabled: true, URL: "http://127.0.0.1:3000" + searchPath, Token: testToken, RunMode: "production"}},
		{name: "wrong path", config: Config{Enabled: true, URL: "https://hire-ai.example.com/api/hai/status", Token: testToken, RunMode: "production"}},
		{name: "short token", config: Config{Enabled: true, URL: "https://hire-ai.example.com" + searchPath, Token: "short-token", RunMode: "production"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NewService(tc.config, nil).Status(); got.Configured || got.ConfigError == "" {
				t.Fatalf("unsafe configuration accepted: %#v", got)
			}
		})
	}
	if _, err := validateEndpoint("http://host.docker.internal:3000"+searchPath, "demo"); err != nil {
		t.Fatalf("development Docker host-gateway endpoint rejected: %v", err)
	}
	if _, err := validateEndpoint("http://hire-ai.example.com:3000"+searchPath, "demo"); err == nil {
		t.Fatal("non-local HTTP endpoint accepted")
	}
}

func TestUnknownRunModesFailClosedLikeProduction(t *testing.T) {
	for _, mode := range []string{"", "production", "prodution", "development", "staging"} {
		t.Run(mode, func(t *testing.T) {
			for _, endpoint := range []string{
				"http://127.0.0.1:3000" + searchPath,
				"http://host.docker.internal:3000" + searchPath,
			} {
				if _, err := validateEndpoint(endpoint, mode); err == nil {
					t.Errorf("mode %q accepted private HTTP endpoint %q", mode, endpoint)
				}
			}
			if allowsLocalNetworkTarget(mode, "host.docker.internal") {
				t.Errorf("mode %q allowed private network dialing", mode)
			}
		})
	}
}

func TestDemoModeAllowsExplicitLocalDevelopmentEndpoint(t *testing.T) {
	if _, err := validateEndpoint("http://host.docker.internal:3000"+searchPath, "demo"); err != nil {
		t.Fatalf("demo mode rejected explicit local endpoint: %v", err)
	}
	if !allowsLocalNetworkTarget("demo", "host.docker.internal") {
		t.Fatal("demo mode rejected private network dialing")
	}
}

func TestEnvironmentConfigDefaultsMissingRunModeToProduction(t *testing.T) {
	t.Setenv("RUN_MODE", "")
	t.Setenv("HIRE_AI_CONNECTOR_ENABLED", "true")
	t.Setenv(urlEnv, "http://127.0.0.1:3000"+searchPath)
	t.Setenv(tokenEnv, testToken)

	status := NewServiceFromEnv().Status()
	if status.Configured || !strings.Contains(status.ConfigError, "public HTTPS hostname") {
		t.Fatalf("missing run mode did not fail closed: %#v", status)
	}
}

func TestSearchRejectsUnsupportedFiltersAndOversizedPayload(t *testing.T) {
	service, _ := testService(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"generatedAt":"2026-10-18T10:00:00Z","policy":{"remoteOnly":true,"listingSafety":"clear","canonicalListingsOnly":true},"jobs":[],"nextCursor":null}`))
	}))
	if _, err := service.Search(context.Background(), SearchRequest{Filters: Filters{JobType: "hybrid"}}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid filter error = %v", err)
	}
	if _, err := service.Search(context.Background(), SearchRequest{Filters: Filters{Query: strings.Repeat("x", 200)}}); err != nil {
		t.Fatalf("query at limit should pass: %v", err)
	}
	if _, err := service.Search(context.Background(), SearchRequest{Filters: Filters{Query: strings.Repeat("x", 201)}}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("oversized query error = %v", err)
	}
}

func TestSearchTextLimitsMatchHireAITrimmedUTF16Semantics(t *testing.T) {
	cases := []struct {
		name    string
		filters Filters
		wantErr bool
	}{
		{name: "200 BMP characters", filters: Filters{Query: strings.Repeat("漢", 200)}},
		{name: "201 BMP characters", filters: Filters{Query: strings.Repeat("漢", 201)}, wantErr: true},
		{name: "100 supplementary characters", filters: Filters{Location: strings.Repeat("🙂", 100)}},
		{name: "101 supplementary characters", filters: Filters{Location: strings.Repeat("🙂", 101)}, wantErr: true},
		{name: "trimmed query at limit", filters: Filters{Query: strings.Repeat(" ", 20) + strings.Repeat("x", 200) + strings.Repeat(" ", 20)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRequest(SearchRequest{Filters: tc.filters})
			if tc.wantErr && !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("validateRequest() error = %v, want ErrInvalidRequest", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateRequest() error = %v, want nil", err)
			}
		})
	}
}

func TestSearchMapsHireAIFilterRejectionWithoutLeakingUpstreamBody(t *testing.T) {
	service, _ := testService(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "private upstream validation detail", http.StatusBadRequest)
	}))

	_, err := service.Search(context.Background(), SearchRequest{Filters: Filters{SalaryCurrency: "ZZZ"}})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Search() error = %v, want ErrInvalidRequest", err)
	}
	if strings.Contains(err.Error(), "private upstream validation detail") {
		t.Fatal("upstream response body was exposed in the returned error")
	}
}

func TestSafeDialContextRefusesUnexpectedAndPrivateTargets(t *testing.T) {
	dial := safeDialContext("production", "hire-ai.example.com")
	if _, err := dial(context.Background(), "tcp", "127.0.0.1:443"); err == nil {
		t.Fatal("unexpected IP target was allowed")
	}
	for _, raw := range []string{
		"0.1.2.3",
		"127.0.0.1",
		"10.0.0.1",
		"100.64.0.1",
		"169.254.1.1",
		"192.0.2.1",
		"198.18.0.1",
		"240.0.0.1",
		"::1",
		"fc00::1",
		"ff02::1",
		"2001:2::1",
		"2001:db8::1",
		"2002::1",
		"3fff::1",
		"5f00::1",
		"64:ff9b::192.0.2.1",
	} {
		ip := netip.MustParseAddr(raw)
		if publicUnicast(ip) {
			t.Errorf("private/non-unicast address %s was treated as public", raw)
		}
	}
	if !publicUnicast(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("public unicast address was rejected")
	}
	if !publicUnicast(netip.MustParseAddr("2606:4700:4700::1111")) {
		t.Fatal("public IPv6 unicast address was rejected")
	}
	if !publicUnicast(netip.MustParseAddr("2001:3::1")) {
		t.Fatal("globally reachable IANA IPv6 special-purpose allocation was rejected")
	}
}

func TestPublicListingURLsRejectLocalHostsAndKeepOrdinaryQueryURLs(t *testing.T) {
	for _, raw := range []string{
		"https://127.0.0.1/apply",
		"https://[::ffff:127.0.0.1]/apply",
		"https://10.1.2.3/apply",
		"https://100.64.0.1/apply",
		"https://192.0.2.1/apply",
		"https://localhost/apply",
		"https://jobs.local/apply",
		"https://user:password@jobs.example/apply",
	} {
		if validPublicHTTPS(&raw) {
			t.Errorf("unsafe listing URL accepted: %q", raw)
		}
	}
	publicURL := "https://jobs.example/apply?source=hire-ai"
	if !validPublicHTTPS(&publicURL) {
		t.Fatal("ordinary HTTPS application URL with query parameters was rejected")
	}
}

func TestStatusNeverExposesConfiguredToken(t *testing.T) {
	service, _ := testService(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	data, err := json.Marshal(service.Status())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), testToken) {
		t.Fatal("status exposed connector token")
	}
}
