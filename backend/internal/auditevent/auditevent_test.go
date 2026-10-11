package auditevent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestWithDetailKeepsExistingEventAndSiblingCopiesIndependent(t *testing.T) {
	base := New(time.Now(), "owner", "review", "workflow/1", "waiting").WithDetail("reason", "original")
	first := base.WithDetail("reason", "first review")
	second := base.WithDetail("note", "second review")
	if base.Details["reason"] != "original" || len(base.Details) != 1 || first.Details["reason"] != "first review" || len(first.Details) != 1 {
		t.Fatalf("copy-on-write audit details changed a prior event: base=%v first=%v", base.Details, first.Details)
	}
	second.Details["reason"] = "independent caller change"
	if base.Details["reason"] != "original" || first.Details["reason"] != "first review" {
		t.Fatal("sibling audit entries still share a mutable detail map")
	}
}

func TestAuditDetailsFilterCredentialNamesAndFreeformValues(t *testing.T) {
	for _, key := range []string{"Cookie", "Set-Cookie", "setCookie", "api-key", "password", "private_metadata"} {
		event := New(time.Now(), "owner", "review", "workflow/1", "waiting").WithDetail(key, "synthetic-credential")
		if event.Details[key] != redacted {
			t.Errorf("credential detail %q was not redacted", key)
		}
	}
	input := "Cookie: session=synthetic-session; csrf=synthetic-csrf\nstatus=keep"
	event := New(time.Now(), "owner", "review", "workflow/1", "waiting").WithDetail("diagnostic", input)
	if strings.Contains(event.Details["diagnostic"], "synthetic-") || !strings.Contains(event.Details["diagnostic"], "status=keep") {
		t.Fatalf("freeform credential leaked or ordinary diagnostic lost: %q", event.Details["diagnostic"])
	}
}

func TestWithDetailSanitizesInheritedEntriesWithoutMutatingSource(t *testing.T) {
	base := New(time.Now(), "owner", "review", "workflow/1", "waiting")
	base.Details = map[string]string{
		"Cookie":                            "synthetic-cookie",
		"Bearer synthetic-field-credential": "synthetic-unsafe-value",
		"diagnostic":                        `{"Bearer synthetic-nested-credential":"synthetic-nested-value","status":"keep"}`,
		"reason":                            "manual approval",
	}
	copy := base.WithDetail("result", "waiting")
	for key, value := range copy.Details {
		if strings.Contains(key, "synthetic-") || strings.Contains(value, "synthetic-") {
			t.Errorf("inherited audit detail escaped filtering: %q=%q", key, value)
		}
	}
	if copy.Details["reason"] != "manual approval" || copy.Details["result"] != "waiting" || len(copy.Details) != 5 {
		t.Fatal("ordinary audit context or details were lost")
	}
	if base.Details["Cookie"] != "synthetic-cookie" || len(base.Details) != 4 {
		t.Fatal("sanitizing the audit copy changed the source")
	}
}

func TestDirectEventJSONRedactsTopLevelAndStructLiteralDetails(t *testing.T) {
	event := Event{
		At:       time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Actor:    "Bearer synthetic-actor-token",
		Action:   "request password=synthetic-action-secret",
		Resource: "https://example.invalid/item?access_token=synthetic-resource-token",
		Result:   "failed api_key=synthetic-result-key",
		Details: map[string]string{
			"Cookie":     "session=synthetic-cookie",
			"diagnostic": "Bearer synthetic-detail-token; status=failed",
		},
	}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "synthetic-") || !strings.Contains(string(raw), "status=failed") {
		t.Fatalf("direct event serialization leaked credentials or lost benign detail: %s", raw)
	}
	if event.Actor != "Bearer synthetic-actor-token" || event.Details["Cookie"] != "session=synthetic-cookie" {
		t.Fatal("JSON redaction mutated the source audit event")
	}
}

func TestNewNormalizesToUTC(t *testing.T) {
	loc := time.FixedZone("X", 3600)
	e := New(time.Date(2026, 1, 1, 12, 0, 0, 0, loc), "operator", "approve", "workflow/1", "success")
	if e.At.Location() != time.UTC {
		t.Fatalf("timestamp not normalized to UTC")
	}
	if e.Actor != "operator" || e.Action != "approve" {
		t.Fatalf("fields wrong: %+v", e)
	}
}

func TestSensitiveDetailsAreRedacted(t *testing.T) {
	e := New(time.Now(), "a", "b", "c", "ok").
		WithDetail("reason", "manual approval").
		WithDetail("api_token", "sk-secret-value").
		WithDetail("Authorization", "Bearer abc")

	if e.Details["reason"] != "manual approval" {
		t.Fatalf("non-sensitive detail should be preserved")
	}
	if e.Details["api_token"] != redacted || e.Details["Authorization"] != redacted {
		t.Fatalf("sensitive details not redacted: %+v", e.Details)
	}
}
