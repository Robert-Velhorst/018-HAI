package task

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

// The expectations below are the exact output of `'<literal>'::jsonb::text` on
// the Postgres version this deployment runs, so the digest is taken over the
// text the payload column will hand back rather than over Go's own formatting.
func TestJSONNumbersAreRenderedTheWayThePayloadColumnReturnsThem(t *testing.T) {
	for _, testCase := range []struct {
		literal string
		want    string
	}{
		{"-0", "0"},
		{"-0.0", "0.0"},
		{"-0.000", "0.000"},
		{"0.35", "0.35"},
		{"1.0", "1.0"},
		{"10.00", "10.00"},
		{"1e5", "100000"},
		{"1E+2", "100"},
		{"1.5e3", "1500"},
		{"0.15e3", "150"},
		{"-1e-7", "-0.0000001"},
		{"1e21", "1000000000000000000000"},
		{
			"-1.8491487257471213e-17",
			"-0.000000000000000018491487257471213",
		},
		{"123456789012345678901234567890", "123456789012345678901234567890"},
	} {
		if got := storageStableJSONNumber(json.Number(testCase.literal)); string(got) != testCase.want {
			t.Errorf("%s rendered as %s, want %s", testCase.literal, got, testCase.want)
		}
	}
}

func TestADigestSurvivesTheNumberFormattingThePayloadColumnApplies(t *testing.T) {
	// What Go writes for a negative zero and a float below 1e-6.
	written := `{"calibration":{"wilsonLowerBound":-0,"margin":-1.8491487257471213e-17},"id":"plan"}`
	// What the same row returns once Postgres has stored it as jsonb.
	readBack := `{"calibration": {"wilsonLowerBound": 0, "margin": -0.000000000000000018491487257471213}, "id": "plan"}`

	decoded, err := decodeWithNumbers(written)
	if err != nil {
		t.Fatalf("decode written payload: %v", err)
	}
	_, digest, err := marshalSanitizedJSONObject(decoded)
	if err != nil {
		t.Fatalf("marshal written payload: %v", err)
	}
	if err := validateStoredDigest(readBack, digest); err != nil {
		t.Fatalf("digest taken before the write no longer matches the text read back: %v", err)
	}
}

func TestCanonicalFormIsUnchangedForNumbersPostgresLeavesAlone(t *testing.T) {
	payload := `{"a":0.35,"b":1.0,"c":10.00,"d":42,"e":[0.62,{"f":-3}]}`
	canonical, err := canonicalTaskStateJSONObject(payload)
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	sum := sha256.Sum256(canonical)

	decoded, err := decodeWithNumbers(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	_, digest, err := marshalSanitizedJSONObject(decoded)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if digest != hex.EncodeToString(sum[:]) {
		t.Fatalf("write and read canonicalization disagree: %s vs %s", digest, hex.EncodeToString(sum[:]))
	}
	if got := string(canonical); got != `{"a":0.35,"b":1.0,"c":10.00,"d":42,"e":[0.62,{"f":-3}]}` {
		t.Fatalf("untouched numbers were rewritten: %s", got)
	}
}

// decodeWithNumbers reads a payload the way the storage layer does, keeping each
// number as the text it was written with rather than as a float64.
func decodeWithNumbers(payload string) (interface{}, error) {
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.UseNumber()
	var decoded interface{}
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}
