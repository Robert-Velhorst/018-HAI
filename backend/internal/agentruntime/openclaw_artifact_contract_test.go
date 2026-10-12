package agentruntime

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenClawArtifactNativeOptionalSizeAndUnicodeTitle(t *testing.T) {
	// Upstream ArtifactSummary permits Unicode titles and omits unknown sizes.
	payload := json.RawMessage(`{"artifacts":[{"id":"artifact_abc","title":"Rapport \u00e9tude","type":"file","runId":"run-private","download":{"mode":"url"}}]}`)
	got, err := openClawGatewayArtifactDescriptorsFromResponse(payload, "run-private")
	if err != nil || len(got) != 1 {
		t.Fatalf("valid native artifact rejected: %v", err)
	}
	encoded, err := json.Marshal(got[0])
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["sizeBytes"] != nil {
		t.Fatalf("unknown size became a measured size: %s", encoded)
	}
	if strings.Contains(string(encoded), "Rapport") || strings.Contains(string(encoded), "artifact_abc") {
		t.Fatal("private title or identifier escaped the metadata boundary")
	}
}

func TestOpenClawArtifactMissingListIsNotAnEmptyResult(t *testing.T) {
	for _, payload := range []string{`{}`, `null`, `{"artifacts":null}`, `{"artifacts":{}}`} {
		if _, err := openClawGatewayArtifactDescriptorsFromResponse(json.RawMessage(payload), "run-private"); err == nil {
			t.Errorf("malformed response counted as successful empty list: %s", payload)
		}
	}
	got, err := openClawGatewayArtifactDescriptorsFromResponse(json.RawMessage(`{"artifacts":[]}`), "run-private")
	if err != nil || len(got) != 0 {
		t.Fatalf("valid empty list rejected: %v", err)
	}
}

func TestOpenClawArtifactDuplicateIsNotCountedTwice(t *testing.T) {
	artifact := `{"id":"artifact_abc","title":"Report","type":"file","runId":"run-private","download":{"mode":"unsupported"}}`
	if _, err := openClawGatewayArtifactDescriptorsFromResponse(json.RawMessage(`{"artifacts":[`+artifact+`,`+artifact+`]}`), "run-private"); err == nil {
		t.Fatal("duplicate artifact accepted as two results")
	}
}

func TestOpenClawArtifactMeasuredZeroAndInvalidMetadata(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra string
		run   string
		valid bool
	}{
		{"measured zero", `,"sizeBytes":0`, "run-private", true},
		{"negative", `,"sizeBytes":-1`, "run-private", false},
		{"fraction", `,"sizeBytes":1.5`, "run-private", false},
		{"overflow", `,"sizeBytes":9223372036854775808`, "run-private", false},
		{"wrong run", `,"sizeBytes":1`, "another-run", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := `{"artifacts":[{"id":"artifact_abc","title":"Report","type":"file","runId":"` + tc.run + `","download":{"mode":"bytes"}` + tc.extra + `}]}`
			got, err := openClawGatewayArtifactDescriptorsFromResponse(json.RawMessage(payload), "run-private")
			if !tc.valid {
				if err == nil {
					t.Fatal("invalid metadata accepted")
				}
				return
			}
			if err != nil || len(got) != 1 || got[0].SizeBytes == nil || *got[0].SizeBytes != 0 {
				t.Fatalf("measured zero lost: %#v %v", got, err)
			}
		})
	}
}

func TestOpenClawArtifactJSONRejectsAmbiguousOrExcessiveStructure(t *testing.T) {
	for _, payload := range []string{
		`{"artifact":{},"artifact":{}}`,
		`{"artifact":{"id":"one","\u0069d":"two"}}`,
		`{"artifact":{}} {"artifact":{}}`,
		strings.Repeat("[", 66) + `0` + strings.Repeat("]", 66),
	} {
		if err := validateOpenClawArtifactJSON([]byte(payload)); err == nil {
			t.Errorf("accepted ambiguous or excessive JSON: %q", payload[:min(len(payload), 96)])
		}
	}
	for _, payload := range []string{`{"artifact":{"id":"one","title":"Report"}}`, `[]`, `null`} {
		if err := validateOpenClawArtifactJSON([]byte(payload)); err != nil {
			t.Errorf("rejected valid JSON: %v", err)
		}
	}
}
