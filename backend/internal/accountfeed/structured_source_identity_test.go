package accountfeed

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"
	"github.com/google/uuid"
)

func structuredIdentityFeed() Feed {
	return Feed{
		ID:   uuid.MustParse("00000000-0000-4000-8000-000000000201"),
		Name: "Local source identity fixture", Provider: "gmail", AccountLabel: " Default:\u00e9 ",
		SourceType: SourceLocalJSONFile, Path: "items.json", WorkspaceID: "identity-workspace",
		OwnerUserID: "identity-owner", OperationType: "review_message", Enabled: true,
	}
}

func structuredIdentityItem() FeedItem {
	received := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return FeedItem{
		ExternalID: " Record:42 ", Title: "Local message", Body: "Message body",
		ReceivedAt: &received, Metadata: map[string]any{"label": "inbox"},
	}
}

func structuredIdentityOperation(t *testing.T, feed Feed, item FeedItem) (operations.NewOperationInput, models.Operation) {
	t.Helper()
	in, err := feed.ToOperationInput(item)
	if err != nil {
		t.Fatal(err)
	}
	op, err := operations.NewOperation(in, time.Date(2026, 1, 2, 4, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return in, op
}

func TestFeedStructuredIdentityEffectiveDefaultsAndOverrides(t *testing.T) {
	for _, kind := range []string{"defaults", "provider_override", "account_override", "both_overrides", "blank_override", "fallback"} {
		t.Run(kind, func(t *testing.T) {
			feed, item := structuredIdentityFeed(), structuredIdentityItem()
			provider, account := feed.Provider, feed.AccountLabel
			switch kind {
			case "provider_override":
				item.Provider, provider = "github", "github"
			case "account_override":
				item.AccountLabel, account = " Item:\u00e9 ", " Item:\u00e9 "
			case "both_overrides":
				item.Provider, provider = "github", "github"
				item.AccountLabel, account = " Item:\u00e9 ", " Item:\u00e9 "
			case "blank_override":
				item.AccountLabel = "   "
			case "fallback":
				feed.AccountLabel, item.AccountLabel = "", "   "
				account = "feed:" + feed.ID.String()
			}
			in, op := structuredIdentityOperation(t, feed, item)
			want, err := operations.SourceIdentityDigest(provider, account, item.ExternalID)
			if err != nil || in.SourceProvider != provider || in.SourceAccount != account || in.SourceExternalID != item.ExternalID ||
				op.SourceProvider != provider || op.SourceAccount != account || op.SourceExternalID != item.ExternalID || op.SourceIdentityHash != want {
				t.Fatalf("effective identity bytes lost: input=%+v operation=%+v err=%v", in, op, err)
			}
			if in.SourceURI != provider+":"+account+":"+item.ExternalID || in.AccountFeedID == nil || *in.AccountFeedID != feed.ID {
				t.Fatal("display URI or feed reference changed")
			}
		})
	}
	first, second := structuredIdentityFeed(), structuredIdentityFeed()
	first.AccountLabel, second.AccountLabel = "", ""
	second.ID = uuid.MustParse("00000000-0000-4000-8000-000000000202")
	_, a := structuredIdentityOperation(t, first, structuredIdentityItem())
	_, b := structuredIdentityOperation(t, second, structuredIdentityItem())
	if a.SourceAccount == b.SourceAccount || a.SourceIdentityHash == b.SourceIdentityHash {
		t.Fatal("feed UUID fallback merged unrelated unnamed accounts")
	}
}

func TestFeedStructuredIdentityStableAcrossSemanticRevisions(t *testing.T) {
	feed, item := structuredIdentityFeed(), structuredIdentityItem()
	baseInput, base := structuredIdentityOperation(t, feed, item)
	for _, kind := range []string{"title", "body", "operation_type", "metadata", "received_at"} {
		t.Run(kind, func(t *testing.T) {
			changed := item
			switch kind {
			case "title":
				changed.Title += " revised"
			case "body":
				changed.Body += " revised"
			case "operation_type":
				changed.OperationType = "review_document"
			case "metadata":
				changed.Metadata = map[string]any{"label": "archive"}
			case "received_at":
				received := item.ReceivedAt.Add(time.Second)
				changed.ReceivedAt = &received
			}
			in, op := structuredIdentityOperation(t, feed, changed)
			if op.SourceIdentityHash != base.SourceIdentityHash || in.SourceRevisionHash == baseInput.SourceRevisionHash || in.DedupeKey == baseInput.DedupeKey {
				t.Fatalf("revision and identity conflated: input=%+v operation=%+v", in, op)
			}
		})
	}
	for _, kind := range []string{"provider", "account", "external"} {
		t.Run("identity_"+kind, func(t *testing.T) {
			changed := item
			switch kind {
			case "provider":
				changed.Provider = "github"
			case "account":
				changed.AccountLabel = "different-account"
			case "external":
				changed.ExternalID += " "
			}
			in, op := structuredIdentityOperation(t, feed, changed)
			if op.SourceIdentityHash == base.SourceIdentityHash || in.DedupeKey == baseInput.DedupeKey || in.SourceRevisionHash != baseInput.SourceRevisionHash {
				t.Fatal("identity tuple changes were normalized or treated as content revisions")
			}
		})
	}
	changed := item
	changed.RawJSON = `{"presentation":"different evidence bytes"}`
	received := item.ReceivedAt.In(time.FixedZone("offset", 2*60*60))
	changed.ReceivedAt = &received
	in, op := structuredIdentityOperation(t, feed, changed)
	if op.SourceIdentityHash != base.SourceIdentityHash || in.SourceRevisionHash != baseInput.SourceRevisionHash || in.DedupeKey != baseInput.DedupeKey {
		t.Fatal("raw evidence formatting or equivalent timestamp changed source identity/revision")
	}
}

func TestFeedStructuredIdentityDisplayURICannotMergeColonSegments(t *testing.T) {
	feed := structuredIdentityFeed()
	left, right := structuredIdentityItem(), structuredIdentityItem()
	left.AccountLabel, left.ExternalID = "a:b", "c"
	right.AccountLabel, right.ExternalID = "a", "b:c"
	a, opA := structuredIdentityOperation(t, feed, left)
	b, opB := structuredIdentityOperation(t, feed, right)
	if a.SourceURI != b.SourceURI || a.SourceRevisionHash != b.SourceRevisionHash || a.DedupeKey == b.DedupeKey || opA.SourceIdentityHash == opB.SourceIdentityHash {
		t.Fatal("ambiguous display URI influenced structured identity")
	}
}

func TestFeedStructuredIdentityRejectsMalformedEffectiveTuple(t *testing.T) {
	for _, kind := range []string{"provider_blank", "provider_utf8", "provider_control", "provider_limit", "account_utf8", "account_control", "account_limit", "external_utf8", "external_control", "external_limit", "invalid_override"} {
		t.Run(kind, func(t *testing.T) {
			feed, item := structuredIdentityFeed(), structuredIdentityItem()
			switch kind {
			case "provider_blank":
				feed.Provider = "   "
			case "provider_utf8":
				feed.Provider = string([]byte{0xff})
			case "provider_control":
				feed.Provider = "gmail\n"
			case "provider_limit":
				feed.Provider = strings.Repeat("x", 65)
			case "account_utf8":
				item.AccountLabel = string([]byte{0xff})
			case "account_control":
				item.AccountLabel = "account\x7f"
			case "account_limit":
				item.AccountLabel = strings.Repeat("\u00e9", 513)
			case "external_utf8":
				item.ExternalID = string([]byte{0xff})
			case "external_control":
				item.ExternalID = "record\x00"
			case "external_limit":
				item.ExternalID = strings.Repeat("\u00e9", 2049)
			case "invalid_override":
				item.Provider = "unsupported-provider"
			}
			in, err := feed.ToOperationInput(item)
			if err == nil || !reflect.DeepEqual(in, operations.NewOperationInput{}) {
				t.Fatalf("invalid identity escaped feed conversion: %+v / %v", in, err)
			}
			if kind != "invalid_override" && !errors.Is(err, operations.ErrInvalidSourceIdentity) {
				t.Fatalf("effective tuple rejection has wrong error: %v", err)
			}
		})
	}
}

func TestFeedStructuredIdentityBothLocalReaderPaths(t *testing.T) {
	for _, overrides := range []bool{false, true} {
		t.Run(map[bool]string{false: "feed_defaults", true: "item_overrides"}[overrides], func(t *testing.T) {
			feed, item := structuredIdentityFeed(), structuredIdentityItem()
			item.OperationType = "review_message"
			provider, account := feed.Provider, feed.AccountLabel
			if overrides {
				item.Provider, item.AccountLabel = "github", " Item:\u00e9 "
				provider, account = item.Provider, item.AccountLabel
			}
			root := t.TempDir()
			raw, err := json.Marshal([]FeedItem{item})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, feed.Path), raw, 0600); err != nil {
				t.Fatal(err)
			}
			reader, err := NewLocalFileReader(feed, root)
			if err != nil {
				t.Fatal(err)
			}
			local, err := reader.Read(context.Background())
			if err != nil || len(local) != 1 {
				t.Fatalf("local JSON reader: items=%d err=%v", len(local), err)
			}
			offset := item.ReceivedAt.In(time.FixedZone("offset", 2*60*60)).Format(time.RFC3339)
			generic := GenericItem{
				ExternalID: item.ExternalID, Title: item.Title, Content: item.Body,
				Provider: provider, ItemType: "email", ReceivedAt: &offset, Metadata: item.Metadata,
				SourceURI: "display:unrelated:to:identity",
			}
			if overrides {
				generic.AccountLabel = account
			}
			localInput, localOp := structuredIdentityOperation(t, feed, local[0])
			for _, envelope := range []bool{false, true} {
				var payload any = []GenericItem{generic}
				if envelope {
					payload = GenericFeed{Cursor: "local-only", Items: []GenericItem{generic}}
				}
				data, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				parsed, err := ParseGenericFeed(data, 0, 0)
				if err != nil || len(parsed.Items) != 1 {
					t.Fatalf("generic local parser: items=%d err=%v", len(parsed.Items), err)
				}
				in, op := structuredIdentityOperation(t, feed, parsed.Items[0].ToFeedItem())
				if in.SourceProvider != provider || in.SourceAccount != account || in.SourceExternalID != item.ExternalID ||
					op.SourceIdentityHash != localOp.SourceIdentityHash || in.SourceRevisionHash != localInput.SourceRevisionHash || in.DedupeKey != localInput.DedupeKey ||
					in.SourceURI != localInput.SourceURI || in.SourceURI == generic.SourceURI {
					t.Fatalf("local reader paths disagree or trust display URI: local=%+v generic=%+v", localInput, in)
				}
				if local[0].RawJSON == "" || parsed.Items[0].RawJSON == "" {
					t.Fatal("reader lost original evidence JSON")
				}
				parsed.Items[0].SourceURI = "another:display:uri"
				again, opAgain := structuredIdentityOperation(t, feed, parsed.Items[0].ToFeedItem())
				if opAgain.SourceIdentityHash != op.SourceIdentityHash || again.SourceRevisionHash != in.SourceRevisionHash || again.DedupeKey != in.DedupeKey {
					t.Fatal("generic source URI changed structured identity or semantic revision")
				}
			}
		})
	}
}
