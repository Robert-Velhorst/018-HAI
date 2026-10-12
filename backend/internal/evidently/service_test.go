package evidently

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEvidentlyBridgeSendsOnlyBoundedLocalFixtureAndValidatesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok","engine":"evidently 0.7.21"}`))
		case "/v1/evaluate":
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "" || r.Header.Get("User-Agent") != "HAI-Evidently-Evaluation/1.0" {
				t.Fatalf("unexpected local evaluation request: %s auth=%q ua=%q", r.Method, r.Header.Get("Authorization"), r.Header.Get("User-Agent"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"passed","engine":"evidently 0.7.21","fixtureKind":"synthetic","caseCount":1,"emptyOutputs":0,"duplicateOutputs":0,"averageOutputChars":12,"reportDigest":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	service := NewService(true, server.URL, 0, nil)
	if probe, err := service.Probe(context.Background()); err != nil || !probe.Reachable || probe.Engine == "" {
		t.Fatalf("unexpected probe: %#v %v", probe, err)
	}
	result, err := service.Evaluate(context.Background(), Request{FixtureKind: "synthetic", Cases: []Case{{ID: "case_one", Input: "What is a test?", Output: "A bounded test."}}})
	if err != nil || result.Status != "passed" || result.CaseCount != 1 || result.ReportDigest == "" {
		t.Fatalf("unexpected response: %#v %v", result, err)
	}
}

func TestEvidentlyBridgeRejectsUnsafeOrExternalFixtures(t *testing.T) {
	unsafe := NewService(true, "https://example.com", 0, nil)
	if unsafe.Status().Configured || unsafe.Status().ConfigError == "" {
		t.Fatalf("external runner must be rejected: %#v", unsafe.Status())
	}
	service := NewService(false, "http://127.0.0.1:8080", 0, nil)
	unsafeFixture := "api" + "_key=" + strings.Repeat("Q", 24)
	_, err := service.Evaluate(context.Background(), Request{FixtureKind: "synthetic", Cases: []Case{{ID: "case", Input: unsafeFixture, Output: "result"}}})
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("disabled runner must not be contacted: %v", err)
	}
	configured := NewService(true, "http://127.0.0.1:8080", 0, nil)
	_, err = configured.Evaluate(context.Background(), Request{FixtureKind: "redacted", Cases: []Case{{ID: "case", Input: "email test@example.com", Output: "result"}}})
	if !errors.Is(err, ErrUnsafeFixture) {
		t.Fatalf("detected personal data must be rejected before runner call: %v", err)
	}
}
