package apierror

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestEnvelopeRedactsCredentialDetailsWithoutChangingOriginalError(t *testing.T) {
	err := New(CodeValidation, "provider password=synthetic-password").
		WithDetail("Cookie", "session=synthetic-session").
		WithDetail("diagnostic", "Bearer synthetic-bearer-value").
		WithDetail("field", "name is required")
	// Direct struct population is part of the public API and must use the same boundary.
	err.Details["Bearer synthetic-field-secret"] = "synthetic-unsafe-value"
	public := err.Envelope()
	raw, marshalErr := json.Marshal(public)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(string(raw), "synthetic-") || public.Error.Code != CodeValidation || public.Error.Details["field"] != "name is required" {
		t.Fatalf("envelope leaked credentials or changed the benign contract: %s", raw)
	}
	if err.Message != "provider password=synthetic-password" || err.Details["Cookie"] != "session=synthetic-session" {
		t.Fatal("building the public envelope mutated the original error")
	}
}

func TestEnvelopeCapturesIndependentSnapshotAndPreservesNil(t *testing.T) {
	err := New(CodeValidation, "name is required").WithDetail("field", "name")
	public := err.Envelope()
	err.Message = "later message"
	err.Details["field"] = "later field"
	if public.Error.Message != "name is required" || public.Error.Details["field"] != "name" {
		t.Fatal("later changes to the error changed the public snapshot")
	}
	public.Error.Details["field"] = "independent change"
	if err.Details["field"] != "later field" {
		t.Fatal("public envelope still aliases the original detail map")
	}
	var absent *Error
	if absent.Envelope().Error != nil {
		t.Fatal("nil error envelope changed its null contract")
	}
}

func TestDirectErrorJSONUsesPublicRedactionBoundary(t *testing.T) {
	err := New(CodeInternal, "provider password=synthetic-password").WithDetail("diagnostic", "Bearer synthetic-bearer-value")
	raw, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(string(raw), "synthetic-") || !strings.Contains(string(raw), `"code":"internal_error"`) {
		t.Fatalf("direct Error serialization bypassed redaction: %s", raw)
	}
	var absent *Error
	raw, marshalErr = json.Marshal(absent)
	if marshalErr != nil || string(raw) != "null" {
		t.Fatalf("nil Error JSON = %q, %v; want null", raw, marshalErr)
	}
}

func TestHTTPStatusMapping(t *testing.T) {
	cases := map[Code]int{
		CodeBadRequest:  http.StatusBadRequest,
		CodeNotFound:    http.StatusNotFound,
		CodeConflict:    http.StatusConflict,
		CodeValidation:  http.StatusUnprocessableEntity,
		CodeRateLimited: http.StatusTooManyRequests,
		CodeInternal:    http.StatusInternalServerError,
		Code("unknown"): http.StatusInternalServerError, // fail-safe default
	}
	for code, want := range cases {
		if got := code.HTTPStatus(); got != want {
			t.Fatalf("%s status = %d, want %d", code, got, want)
		}
	}
}

func TestErrorStringAndDetails(t *testing.T) {
	err := New(CodeValidation, "content is required").WithDetail("content", "must not be empty")
	if err.Error() != "validation_failed: content is required" {
		t.Fatalf("Error() = %q", err.Error())
	}
	if err.Details["content"] != "must not be empty" {
		t.Fatalf("detail missing: %+v", err.Details)
	}
	if err.HTTPStatus() != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", err.HTTPStatus())
	}
}

func TestErrorStringRedactsCredentials(t *testing.T) {
	err := New(CodeUnavailable, "provider password=synthetic-password")
	if got := err.Error(); strings.Contains(got, "synthetic-password") || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("error string leaked a credential: %q", got)
	}
	var absent *Error
	if got := absent.Error(); got != "<nil>" {
		t.Fatalf("nil error string = %q, want <nil>", got)
	}
}

func TestEnvelopeSerialization(t *testing.T) {
	raw, _ := json.Marshal(New(CodeNotFound, "memory not found").Envelope())
	var round struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round.Error.Code != "not_found" || round.Error.Message != "memory not found" {
		t.Fatalf("envelope round-trip wrong: %+v", round)
	}
}

func TestPublicMessageDoesNotExposeUnexpectedErrorDetails(t *testing.T) {
	if got := PublicMessage(errors.New(`database password=real-secret at C:\\private`), "Service is unavailable"); got != "Service is unavailable" {
		t.Fatalf("unexpected error message = %q", got)
	}
	if got := PublicMessage(New(CodeValidation, "name is required"), "Service is unavailable"); got != "name is required" {
		t.Fatalf("structured error message = %q", got)
	}
}
