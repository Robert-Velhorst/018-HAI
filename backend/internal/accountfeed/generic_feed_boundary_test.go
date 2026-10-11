package accountfeed

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestParseGenericFeedRejectsOversizeAndInvalidUTF8(t *testing.T) {
	if got, err := ParseGenericFeed(bytes.Repeat([]byte{' '}, maxFeedBytes+1), 0, 0); err == nil || len(got.Items) != 0 {
		t.Fatalf("oversized direct parse = %+v, %v; want rejection", got, err)
	}
	if got, err := ParseGenericFeed([]byte{'[', '"', 0xff, '"', ']'}, 0, 0); err == nil || len(got.Items) != 0 {
		t.Fatalf("invalid UTF-8 parse = %+v, %v; want rejection", got, err)
	}
}

func TestParseGenericFeedRejectsDuplicateKeysAtEveryDepth(t *testing.T) {
	for _, input := range []string{
		`{"items":[],"items":[]}`,
		`[{"externalId":"first","externalId":"second","title":"x","provider":"generic_json_feed","itemType":"email"}]`,
		`{"items":[{"externalId":"x","title":"x","provider":"generic_json_feed","itemType":"email","metadata":{"revision":1,"revision":2}}]}`,
	} {
		t.Run(input, func(t *testing.T) {
			if got, err := ParseGenericFeed([]byte(input), 0, 0); err == nil || len(got.Items) != 0 {
				t.Fatalf("duplicate-key input accepted: %+v, %v", got, err)
			}
		})
	}
}

func TestParseGenericFeedRejectsTypedFieldAliases(t *testing.T) {
	for _, input := range []string{
		`{"items":[],"ITEMS":[{"externalId":"x","title":"x","provider":"generic_json_feed","itemType":"email"}]}`,
		`[{"externalId":"first","ExternalID":"second","title":"x","provider":"generic_json_feed","itemType":"email"}]`,
		`[{"externalId":"x","title":"x","provider":"generic_json_feed","itemType":"email","ReceivedAt":"2026-01-01T00:00:00Z","receivedAt":"2026-01-02T00:00:00Z"}]`,
	} {
		if got, err := ParseGenericFeed([]byte(input), 0, 0); err == nil || len(got.Items) != 0 {
			t.Fatalf("typed field alias accepted: %+v, %v", got, err)
		}
	}
	// Metadata remains a case-sensitive map and is not subject to struct-field alias rules.
	input := `[{"externalId":"x","title":"x","provider":"generic_json_feed","itemType":"email","metadata":{"Revision":1,"revision":2}}]`
	if _, err := ParseGenericFeed([]byte(input), 0, 0); err != nil {
		t.Fatalf("case-distinct metadata keys rejected: %v", err)
	}
}

func TestParseGenericFeedRejectsNonStringCursor(t *testing.T) {
	for _, value := range []string{"null", "false", "1", "[]", "{}"} {
		input := `{"cursor":` + value + `,"items":[]}`
		if got, err := ParseGenericFeed([]byte(input), 0, 0); err == nil || len(got.Items) != 0 {
			t.Errorf("cursor %s accepted: %+v, %v", value, got, err)
		}
	}
	if got, err := ParseGenericFeed([]byte(`{"items":[]}`), 0, 0); err != nil || got.Cursor != "" {
		t.Fatalf("omitted cursor should remain valid: %+v, %v", got, err)
	}
}

func TestParseGenericFeedBoundsItemCountAndCursor(t *testing.T) {
	item := `{"externalId":"x","title":"x","provider":"generic_json_feed","itemType":"email"}`
	var input strings.Builder
	input.WriteString(`{"cursor":"c","items":[`)
	for i := 0; i <= maxGenericFeedItems; i++ {
		if i != 0 {
			input.WriteByte(',')
		}
		input.WriteString(item)
	}
	input.WriteString(`]}`)
	if got, err := ParseGenericFeed([]byte(input.String()), 0, 0); err == nil || len(got.Items) != 0 {
		t.Fatalf("excess item count accepted: items=%d err=%v", len(got.Items), err)
	}
	tooLongCursor := fmt.Sprintf(`{"cursor":%q,"items":[]}`, strings.Repeat("c", maxGenericFeedCursor+1))
	if got, err := ParseGenericFeed([]byte(tooLongCursor), 0, 0); err == nil || len(got.Items) != 0 {
		t.Fatalf("excess cursor accepted: %+v, %v", got, err)
	}
}

func TestGenericItemRejectsUnmarshalableMetadata(t *testing.T) {
	item := GenericItem{
		ExternalID: "x", Title: "x", Provider: string(ProviderGenericJSONFeed),
		ItemType: string(ItemEmail), Metadata: map[string]any{"value": math.NaN()},
	}
	if err := item.Validate(0, 0); err == nil || !strings.Contains(err.Error(), "metadata") {
		t.Fatalf("invalid metadata validation error = %v, want explicit rejection", err)
	}
}
