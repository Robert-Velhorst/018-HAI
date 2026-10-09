package operations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

var sourceIdentityContractTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func sourceIdentityContractInput() NewOperationInput {
	return NewOperationInput{
		OwnerUserID: "identity-owner", WorkspaceID: "identity-workspace",
		Title: "Local source record", OperationType: "review_source_item", SourceType: "local_json_file",
		DedupeKey: "arbitrary-canonical-key", EvidenceJSON: "{}",
		SourceProvider: "gmail", SourceAccount: "account:a", SourceExternalID: "record:b",
		SourceRevisionHash: "opaque revision:v1", SourceURI: "display only",
	}
}

func sourceIdentityContractOperation(t *testing.T, in NewOperationInput) models.Operation {
	t.Helper()
	op, err := NewOperation(in, sourceIdentityContractTime)
	if err != nil {
		t.Fatal(err)
	}
	op.ID = uuid.MustParse("00000000-0000-4000-8000-000000000101")
	return op
}

func TestSourceIdentityDigestSchemaAndExactBytes(t *testing.T) {
	// Pin the independent schema bytes, not an encoding rebuilt from production types.
	wire := `{"version":1,"provider":"gmail","account":"account:a","externalId":"record:b"}`
	sum := sha256.Sum256([]byte(wire))
	want := hex.EncodeToString(sum[:])
	got, err := SourceIdentityDigest("gmail", "account:a", "record:b")
	if err != nil || got != want || len(got) != 64 || got != strings.ToLower(got) {
		t.Fatalf("schema-v1 digest = %q, %v; want %q", got, err, want)
	}
	seen := map[string]bool{got: true}
	for _, tuple := range [][3]string{
		{"Gmail", "account:a", "record:b"},
		{"gmail", " account:a ", "record:b"},
		{"gmail", "account:a", " record:b "},
		{"gmail", "caf\u00e9", "record:b"},
		{"gmail", "cafe\u0301", "record:b"},
	} {
		digest, err := SourceIdentityDigest(tuple[0], tuple[1], tuple[2])
		if err != nil || seen[digest] {
			t.Fatalf("meaningful bytes normalized or rejected: %q => %q, %v", tuple, digest, err)
		}
		seen[digest] = true
	}
	for _, pair := range [][2][3]string{
		{{"a:b", "c", "d"}, {"a", "b:c", "d"}},
		{{"gmail", "a:b", "c"}, {"gmail", "a", "b:c"}},
	} {
		a, errA := SourceIdentityDigest(pair[0][0], pair[0][1], pair[0][2])
		b, errB := SourceIdentityDigest(pair[1][0], pair[1][1], pair[1][2])
		if errA != nil || errB != nil || a == b {
			t.Fatalf("colon boundaries collided: %q, %q / %v, %v", a, b, errA, errB)
		}
	}
}

func TestSourceIdentityDigestValidationAndByteLimits(t *testing.T) {
	for field, limit := range []int{64, 1024, 4096} {
		t.Run(fmt.Sprintf("field_%d", field), func(t *testing.T) {
			invalid := []string{"", "   ", "\u2003", string([]byte{0xff}), strings.Repeat("x", limit+1), strings.Repeat("\u00e9", limit/2) + "x"}
			for control := 0; control < 32; control++ {
				invalid = append(invalid, "a"+string(rune(control))+"b")
			}
			invalid = append(invalid, "a\x7fb")
			for index, value := range invalid {
				tuple := [3]string{"gmail", "account", "external"}
				tuple[field] = value
				if digest, err := SourceIdentityDigest(tuple[0], tuple[1], tuple[2]); !errors.Is(err, ErrInvalidSourceIdentity) || digest != "" {
					t.Fatalf("invalid case %d accepted: digest=%q err=%v", index, digest, err)
				}
				in := sourceIdentityContractInput()
				in.SourceProvider, in.SourceAccount, in.SourceExternalID = tuple[0], tuple[1], tuple[2]
				if _, err := NewOperation(in, sourceIdentityContractTime); !errors.Is(err, ErrInvalidSourceIdentity) {
					t.Fatalf("invalid tuple accepted by NewOperation, case %d: %v", index, err)
				}
				op := sourceIdentityContractOperation(t, sourceIdentityContractInput())
				op.SourceProvider, op.SourceAccount, op.SourceExternalID = tuple[0], tuple[1], tuple[2]
				if err := Validate(op); !errors.Is(err, ErrInvalidSourceIdentity) {
					t.Fatalf("invalid tuple accepted by Validate, case %d: %v", index, err)
				}
			}
			for _, value := range []string{strings.Repeat("x", limit), strings.Repeat("\u00e9", limit/2), "a\u0085b"} {
				tuple := [3]string{"gmail", "account", "external"}
				tuple[field] = value
				if digest, err := SourceIdentityDigest(tuple[0], tuple[1], tuple[2]); err != nil || len(digest) != 64 {
					t.Fatalf("valid byte boundary rejected: bytes=%d digest=%q err=%v", len(value), digest, err)
				}
			}
		})
	}
}

func TestNewOperationDerivesSourceIdentityWithoutCallerDigest(t *testing.T) {
	if _, exists := reflect.TypeOf(NewOperationInput{}).FieldByName("SourceIdentityHash"); exists {
		t.Fatal("caller-controlled SourceIdentityHash must not be an intake input")
	}
	in := sourceIdentityContractInput()
	in.SourceAccount, in.SourceExternalID = " Account:\u00e9 ", " Record:42 "
	op := sourceIdentityContractOperation(t, in)
	want, err := SourceIdentityDigest(in.SourceProvider, in.SourceAccount, in.SourceExternalID)
	if err != nil || op.SourceProvider != in.SourceProvider || op.SourceAccount != in.SourceAccount || op.SourceExternalID != in.SourceExternalID || op.SourceIdentityHash != want {
		t.Fatalf("structured identity not preserved/derived: %+v / %v", op, err)
	}
	other := in
	other.OwnerUserID, other.WorkspaceID = "other-owner", "other-workspace"
	other.SourceRevisionHash, other.SourceURI, other.DedupeKey = "opaque revision:v2", "different display", "other-key"
	if second := sourceIdentityContractOperation(t, other); second.SourceIdentityHash != op.SourceIdentityHash {
		t.Fatal("scope, revision, display URI or dedupe key entered the identity digest")
	}
}

func TestOperationSourceIdentityLegacyAndPartialValidation(t *testing.T) {
	for mask := 0; mask < 8; mask++ {
		t.Run(fmt.Sprintf("tuple_mask_%d", mask), func(t *testing.T) {
			in := sourceIdentityContractInput()
			if mask&1 == 0 {
				in.SourceProvider = ""
			}
			if mask&2 == 0 {
				in.SourceAccount = ""
			}
			if mask&4 == 0 {
				in.SourceExternalID = ""
			}
			op, err := NewOperation(in, sourceIdentityContractTime)
			if mask != 0 && mask != 7 {
				if !errors.Is(err, ErrInvalidSourceIdentity) {
					t.Fatalf("partial identity accepted: %v", err)
				}
				return
			}
			if err != nil || Validate(op) != nil || (mask == 0 && op.SourceIdentityHash != "") {
				t.Fatalf("complete or legacy identity rejected: %+v / %v", op, err)
			}
			if mask == 0 {
				for _, revision := range []string{"", "legacy opaque", "legacy\nrevision", string([]byte{0xff}), strings.Repeat("x", 4097)} {
					in.SourceRevisionHash = revision
					legacy, err := NewOperation(in, sourceIdentityContractTime)
					if err != nil || Validate(legacy) != nil {
						t.Fatalf("unidentified legacy revision constrained: %v", err)
					}
				}
			}
		})
	}
	op := sourceIdentityContractOperation(t, sourceIdentityContractInput())
	for _, kind := range []string{"provider", "account", "external", "hash", "empty_hash", "uppercase_hash", "hash_only"} {
		t.Run(kind, func(t *testing.T) {
			bad := op
			switch kind {
			case "provider":
				bad.SourceProvider = "github"
			case "account":
				bad.SourceAccount += " "
			case "external":
				bad.SourceExternalID = ""
			case "hash":
				bad.SourceIdentityHash = strings.Repeat("0", 64)
			case "empty_hash":
				bad.SourceIdentityHash = ""
			case "uppercase_hash":
				bad.SourceIdentityHash = strings.ToUpper(bad.SourceIdentityHash)
			case "hash_only":
				bad.SourceProvider, bad.SourceAccount, bad.SourceExternalID = "", "", ""
			}
			if err := Validate(bad); !errors.Is(err, ErrInvalidSourceIdentity) {
				t.Fatalf("tampered identity validated: %v", err)
			}
		})
	}
	op.SourceURI = "arbitrary:display:only"
	if err := Validate(op); err != nil {
		t.Fatalf("display URI interpreted as identity: %v", err)
	}
}

func TestIdentifiedSourceRevisionValidation(t *testing.T) {
	invalid := []string{"", "  ", "\u2003", string([]byte{0xff}), strings.Repeat("x", 4097), strings.Repeat("\u00e9", 2048) + "x"}
	for control := 0; control < 32; control++ {
		invalid = append(invalid, "revision"+string(rune(control)))
	}
	invalid = append(invalid, "revision\x7f")
	for index, revision := range invalid {
		t.Run(fmt.Sprintf("invalid_%d", index), func(t *testing.T) {
			in := sourceIdentityContractInput()
			in.SourceRevisionHash = revision
			if _, err := NewOperation(in, sourceIdentityContractTime); !errors.Is(err, ErrInvalidSourceIdentity) {
				t.Fatalf("invalid identified revision accepted at intake: %v", err)
			}
			op := sourceIdentityContractOperation(t, sourceIdentityContractInput())
			op.SourceRevisionHash = revision
			if err := Validate(op); !errors.Is(err, ErrInvalidSourceIdentity) {
				t.Fatalf("invalid identified revision validated: %v", err)
			}
		})
	}
	for _, revision := range []string{"opaque revision:not-hex", " revision:\u00e9 ", strings.Repeat("x", 4096), strings.Repeat("\u00e9", 2048)} {
		in := sourceIdentityContractInput()
		in.SourceRevisionHash = revision
		if op := sourceIdentityContractOperation(t, in); op.SourceRevisionHash != revision {
			t.Fatal("meaningful revision bytes changed")
		}
	}
}

type sourceIdentityContractSnapshot struct {
	ops    map[uuid.UUID]models.Operation
	events []models.OperationEvent
	claims map[uuid.UUID]memoryExecutionClaim
}

func sourceIdentitySnapshot(repo *MemoryRepository) sourceIdentityContractSnapshot {
	snapshot := sourceIdentityContractSnapshot{
		ops: make(map[uuid.UUID]models.Operation), claims: make(map[uuid.UUID]memoryExecutionClaim),
		events: append([]models.OperationEvent{}, repo.events...),
	}
	for id, op := range repo.ops {
		snapshot.ops[id] = op
	}
	for id, claim := range repo.claims {
		snapshot.claims[id] = claim
	}
	return snapshot
}

func sourceIdentityAssertUnchanged(t *testing.T, repo *MemoryRepository, before sourceIdentityContractSnapshot) {
	t.Helper()
	if !reflect.DeepEqual(sourceIdentitySnapshot(repo), before) {
		t.Fatal("rejected call changed operation, audit or execution claim")
	}
}

func sourceIdentityIngest(svc *Service, contextual bool, in NewOperationInput) (IngestResult, error) {
	svc.now = func() time.Time { return sourceIdentityContractTime }
	if contextual {
		return svc.IngestContext(context.Background(), in)
	}
	return svc.Ingest(in)
}

func TestStructuredIdentityCanonicalDuplicateGuards(t *testing.T) {
	for _, contextual := range []bool{false, true} {
		for _, kind := range []string{"same", "provider", "account", "external", "revision", "legacy_existing", "legacy_incoming"} {
			t.Run(fmt.Sprintf("context_%t/%s", contextual, kind), func(t *testing.T) {
				repo := NewMemoryRepository()
				svc := NewService(repo)
				svc.now = func() time.Time { return sourceIdentityContractTime }
				existing, incoming := sourceIdentityContractInput(), sourceIdentityContractInput()
				wantErr := ErrInvalidRepositoryResult
				switch kind {
				case "same":
					wantErr = nil
				case "provider":
					incoming.SourceProvider = "github"
				case "account":
					incoming.SourceAccount = "another-account"
				case "external":
					incoming.SourceExternalID = "another-record"
				case "revision":
					incoming.SourceRevisionHash = "revision:v2"
				case "legacy_existing":
					existing.SourceProvider, existing.SourceAccount, existing.SourceExternalID = "", "", ""
					wantErr = ErrSourceIdentityMigrationRequired
				case "legacy_incoming":
					incoming.SourceProvider, incoming.SourceAccount, incoming.SourceExternalID = "", "", ""
				}
				seed := sourceIdentityContractOperation(t, existing)
				if _, err := repo.Create(&seed); err != nil {
					t.Fatal(err)
				}
				before := sourceIdentitySnapshot(repo)
				got, err := sourceIdentityIngest(svc, contextual, incoming)
				if wantErr == nil {
					if err != nil || got.Created || got.Operation.ID != seed.ID || got.Operation.SourceIdentityHash != seed.SourceIdentityHash {
						t.Fatalf("exact duplicate not reused: %+v / %v", got, err)
					}
				} else if !errors.Is(err, wantErr) || !reflect.DeepEqual(got, IngestResult{}) {
					t.Fatalf("duplicate crossed identity fence: %+v / %v; want %v", got, err, wantErr)
				}
				sourceIdentityAssertUnchanged(t, repo, before)
			})
		}
	}
}

func TestStructuredIdentityDistinctRevisionsAndRepositoryScopes(t *testing.T) {
	for _, contextual := range []bool{false, true} {
		t.Run(fmt.Sprintf("context_%t", contextual), func(t *testing.T) {
			repo := NewMemoryRepository()
			svc := NewService(repo)
			svc.now = func() time.Time { return sourceIdentityContractTime }
			in := sourceIdentityContractInput()
			first, err := sourceIdentityIngest(svc, contextual, in)
			if err != nil || !first.Created {
				t.Fatalf("initial intake: %+v / %v", first, err)
			}
			variants := []NewOperationInput{in, in, in}
			variants[0].SourceRevisionHash, variants[0].DedupeKey = "opaque revision:v2", "second-revision-key"
			variants[1].OwnerUserID = "another-owner"
			variants[2].WorkspaceID = "another-workspace"
			ids := map[uuid.UUID]bool{first.Operation.ID: true}
			for _, variant := range variants {
				got, err := sourceIdentityIngest(svc, contextual, variant)
				if err != nil || !got.Created || ids[got.Operation.ID] || got.Operation.SourceIdentityHash != first.Operation.SourceIdentityHash ||
					got.Operation.OwnerUserID != variant.OwnerUserID || got.Operation.WorkspaceID != variant.WorkspaceID || got.Operation.SourceRevisionHash != variant.SourceRevisionHash {
					t.Fatalf("revision or scope incorrectly merged: %+v / %v", got, err)
				}
				ids[got.Operation.ID] = true
			}
			if len(repo.ops) != 4 || len(repo.events) != 4 {
				t.Fatal("separate scoped revisions did not retain atomic creation events")
			}
		})
	}
}

func TestStructuredIdentityHistoricalLookupKeepsMigrationFence(t *testing.T) {
	for _, contextual := range []bool{false, true} {
		for _, kind := range []string{"legacy", "different_valid_identity", "malformed_identity"} {
			t.Run(fmt.Sprintf("context_%t/%s", contextual, kind), func(t *testing.T) {
				repo := NewMemoryRepository()
				incoming := sourceIdentityContractInput()
				incoming.LegacyDedupeKey = "historical-key"
				old := incoming
				old.DedupeKey = incoming.LegacyDedupeKey
				old.SourceAccount, old.SourceRevisionHash = "different historical account", "historical revision"
				if kind == "legacy" {
					old.SourceProvider, old.SourceAccount, old.SourceExternalID = "", "", ""
				}
				seed := sourceIdentityContractOperation(t, old)
				if _, err := repo.Create(&seed); err != nil {
					t.Fatal(err)
				}
				wantErr := ErrSourceIdentityMigrationRequired
				if kind == "malformed_identity" {
					// Simulate a corrupt persisted row; normal Create must reject it.
					seed.SourceIdentityHash = "corrupt"
					repo.ops[seed.ID] = seed
					wantErr = ErrInvalidRepositoryResult
				}
				before := sourceIdentitySnapshot(repo)
				got, err := sourceIdentityIngest(NewService(repo), contextual, incoming)
				if !errors.Is(err, wantErr) || !reflect.DeepEqual(got, IngestResult{}) {
					t.Fatalf("historical lookup crossed migration/format fence: %+v / %v; want %v", got, err, wantErr)
				}
				sourceIdentityAssertUnchanged(t, repo, before)
			})
		}
	}
}

func TestStructuredIdentityCreationRaceRejectsMismatchingCanonicalResult(t *testing.T) {
	for _, contextual := range []bool{false, true} {
		t.Run(fmt.Sprintf("context_%t", contextual), func(t *testing.T) {
			base := NewMemoryRepository()
			adapter := &invalidIntakeResultRepository{MemoryRepository: base}
			in := sourceIdentityContractInput()
			wrong := sourceIdentityMutated(t, sourceIdentityContractOperation(t, in), "account")
			adapter.lookup = func(string, string, string) (*models.Operation, bool, error) {
				if adapter.creates == 0 {
					return nil, false, nil
				}
				return &wrong, true, nil
			}
			adapter.write = func(*models.Operation) (*models.Operation, error) {
				return nil, ErrDuplicateDedupeKey
			}
			before := sourceIdentitySnapshot(base)
			got, err := sourceIdentityIngest(NewService(adapter), contextual, in)
			if !errors.Is(err, ErrInvalidRepositoryResult) || !reflect.DeepEqual(got, IngestResult{}) || adapter.creates != 1 || adapter.updates != 0 {
				t.Fatalf("creation race reused mismatching identity: %+v / %v", got, err)
			}
			sourceIdentityAssertUnchanged(t, base, before)
		})
	}
}

func TestStructuredIdentityIntakeRejectsCorruptedSuccessfulWrites(t *testing.T) {
	for _, contextual := range []bool{false, true} {
		for _, path := range []string{"create", "refresh"} {
			for _, kind := range []string{"provider", "account", "external", "hash", "revision", "clear"} {
				t.Run(fmt.Sprintf("context_%t/%s/%s", contextual, path, kind), func(t *testing.T) {
					base := NewMemoryRepository()
					in := sourceIdentityContractInput()
					if path == "refresh" {
						seed := sourceIdentityContractOperation(t, in)
						if _, err := base.Create(&seed); err != nil {
							t.Fatal(err)
						}
						in.EvidenceJSON = `{"updated":true}`
					}
					before := sourceIdentitySnapshot(base)
					adapter := &invalidIntakeResultRepository{MemoryRepository: base}
					adapter.write = func(op *models.Operation) (*models.Operation, error) {
						bad := sourceIdentityMutated(t, *op, kind)
						*op = bad
						return op, nil
					}
					got, err := sourceIdentityIngest(NewService(adapter), contextual, in)
					if !errors.Is(err, ErrInvalidRepositoryResult) || !reflect.DeepEqual(got, IngestResult{}) || adapter.creates+adapter.updates != 1 {
						t.Fatalf("corrupted successful response exposed/retried: %+v / %v, writes=%d", got, err, adapter.creates+adapter.updates)
					}
					// This adapter does not commit; the assertion is not a rollback claim.
					sourceIdentityAssertUnchanged(t, base, before)
				})
			}
		}
	}
}

func sourceIdentityMutated(t *testing.T, op models.Operation, kind string) models.Operation {
	t.Helper()
	switch kind {
	case "provider":
		op.SourceProvider = "github"
	case "account":
		op.SourceAccount += ":other"
	case "external":
		op.SourceExternalID += ":other"
	case "hash":
		op.SourceIdentityHash = strings.Repeat("0", 64)
		return op
	case "revision":
		op.SourceRevisionHash = "other opaque revision"
		return op
	case "clear":
		op.SourceProvider, op.SourceAccount, op.SourceExternalID, op.SourceIdentityHash = "", "", "", ""
		return op
	case "identify_legacy":
		op.SourceProvider, op.SourceAccount, op.SourceExternalID = "gmail", "new-account", "new-record"
		op.SourceRevisionHash = "new opaque revision"
	default:
		t.Fatalf("unknown mutation fixture %q", kind)
	}
	var err error
	op.SourceIdentityHash, err = SourceIdentityDigest(op.SourceProvider, op.SourceAccount, op.SourceExternalID)
	if err != nil {
		t.Fatal(err)
	}
	return op
}

func TestSourceIdentityImmutableAcrossMemoryMutationPaths(t *testing.T) {
	for _, path := range []string{"update", "atomic_update", "context_update", "save", "transition", "claimed", "claimed_service"} {
		for _, kind := range []string{"provider", "account", "external", "hash", "revision", "clear", "identify_legacy"} {
			t.Run(path+"/"+kind, func(t *testing.T) {
				repo := NewMemoryRepository()
				in := sourceIdentityContractInput()
				if kind == "identify_legacy" {
					in.SourceProvider, in.SourceAccount, in.SourceExternalID = "", "", ""
				}
				op := sourceIdentityContractOperation(t, in)
				if _, err := repo.Create(&op); err != nil {
					t.Fatal(err)
				}
				worker := uuid.MustParse("00000000-0000-4000-8000-000000000102")
				claim := ExecutionClaim{OperationID: op.ID, OwnerUserID: op.OwnerUserID, WorkspaceID: op.WorkspaceID, Owner: worker, Generation: 7}
				if strings.HasPrefix(path, "claimed") {
					// Fixed lease avoids scheduler/sleep dependencies and provider execution.
					repo.claims[op.ID] = memoryExecutionClaim{owner: worker, generation: 7, expiresAt: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)}
				}
				before := sourceIdentitySnapshot(repo)
				bad := sourceIdentityMutated(t, op, kind)
				svc := NewService(repo)
				svc.now = func() time.Time { return sourceIdentityContractTime.Add(time.Second) }
				var got *models.Operation
				var err error
				switch path {
				case "save":
					got, err = svc.Save(bad, "metadata_changed", "test", "identity must remain immutable")
				case "transition":
					got, err = svc.Transition(bad, StatusClassified, "test", "", "identity must remain immutable")
				case "claimed_service":
					got, err = svc.TransitionClaimed(context.Background(), claim, bad, StatusClassified, "test", "", "identity must remain immutable")
				case "claimed":
					var event models.OperationEvent
					bad, event, err = ApplyTransition(bad, StatusClassified, "test", "", "identity must remain immutable", sourceIdentityContractTime.Add(time.Second))
					if err != nil {
						t.Fatal(err)
					}
					got, err = repo.TransitionClaimed(context.Background(), claim, bad, event, true)
				default:
					bad.Version++
					bad.UpdatedAt = sourceIdentityContractTime.Add(time.Second)
					event := models.OperationEvent{OperationID: bad.ID, EventType: "metadata_changed", ActorType: "test", AfterStatus: bad.Status, PayloadJSON: "{}", CreatedAt: bad.UpdatedAt}
					switch path {
					case "update":
						got, err = repo.Update(&bad)
					case "atomic_update":
						got, err = repo.UpdateWithEvent(&bad, &event)
					case "context_update":
						got, err = repo.UpdateWithEventContext(context.Background(), &bad, &event)
					}
				}
				if !errors.Is(err, ErrSourceIdentityImmutable) || got != nil {
					t.Fatalf("mutation accepted/wrong sentinel: result=%+v err=%v", got, err)
				}
				sourceIdentityAssertUnchanged(t, repo, before)
			})
		}
	}
}

func TestSourceIdentityDirectCreateRejectsMalformedIdentity(t *testing.T) {
	for _, path := range []string{"create", "atomic_create", "context_create"} {
		for _, kind := range []string{"partial", "hash", "revision"} {
			t.Run(path+"/"+kind, func(t *testing.T) {
				repo := NewMemoryRepository()
				op := sourceIdentityContractOperation(t, sourceIdentityContractInput())
				switch kind {
				case "partial":
					op.SourceExternalID = ""
				case "hash":
					op.SourceIdentityHash = "forged"
				case "revision":
					op.SourceRevisionHash = "bad\nrevision"
				}
				before := sourceIdentitySnapshot(repo)
				event := models.OperationEvent{OperationID: op.ID, EventType: "created", ActorType: string(OwnerHAI), AfterStatus: op.Status, PayloadJSON: "{}", CreatedAt: sourceIdentityContractTime}
				var got *models.Operation
				var err error
				switch path {
				case "create":
					got, err = repo.Create(&op)
				case "atomic_create":
					got, err = repo.CreateWithEvent(&op, &event)
				case "context_create":
					got, err = repo.CreateWithEventContext(context.Background(), &op, &event)
				}
				if !errors.Is(err, ErrInvalidSourceIdentity) || got != nil {
					t.Fatalf("direct create bypassed validation: %+v / %v", got, err)
				}
				sourceIdentityAssertUnchanged(t, repo, before)
			})
		}
	}
}

func TestSourceIdentityUnchangedUpdatesAndLegacyRevisionsRemainAllowed(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		for _, path := range []string{"update", "atomic_update", "context_update", "save", "transition", "claimed", "claimed_service"} {
			t.Run(fmt.Sprintf("legacy_%t/%s", legacy, path), func(t *testing.T) {
				repo := NewMemoryRepository()
				in := sourceIdentityContractInput()
				if legacy {
					in.SourceProvider, in.SourceAccount, in.SourceExternalID = "", "", ""
				}
				op := sourceIdentityContractOperation(t, in)
				if _, err := repo.Create(&op); err != nil {
					t.Fatal(err)
				}
				op.SourceURI, op.Description = "updated display only", "new description"
				if legacy {
					op.SourceRevisionHash = "legacy\nrevision"
				}
				worker := uuid.MustParse("00000000-0000-4000-8000-000000000102")
				claim := ExecutionClaim{OperationID: op.ID, OwnerUserID: op.OwnerUserID, WorkspaceID: op.WorkspaceID, Owner: worker, Generation: 7}
				if strings.HasPrefix(path, "claimed") {
					repo.claims[op.ID] = memoryExecutionClaim{owner: worker, generation: 7, expiresAt: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)}
				}
				svc := NewService(repo)
				svc.now = func() time.Time { return sourceIdentityContractTime }
				var got *models.Operation
				var err error
				switch path {
				case "save":
					got, err = svc.Save(op, "metadata_changed", "test", "display-only update")
				case "transition":
					got, err = svc.Transition(op, StatusClassified, "test", "", "display-only update")
				case "claimed_service":
					got, err = svc.TransitionClaimed(context.Background(), claim, op, StatusClassified, "test", "", "display-only update")
				case "claimed":
					var next models.Operation
					var event models.OperationEvent
					next, event, err = ApplyTransition(op, StatusClassified, "test", "", "display-only update", sourceIdentityContractTime)
					if err != nil {
						t.Fatal(err)
					}
					got, err = repo.TransitionClaimed(context.Background(), claim, next, event, true)
				default:
					op.Version++
					event := models.OperationEvent{OperationID: op.ID, EventType: "metadata_changed", ActorType: "test", AfterStatus: op.Status, PayloadJSON: "{}", CreatedAt: sourceIdentityContractTime}
					switch path {
					case "update":
						got, err = repo.Update(&op)
					case "atomic_update":
						got, err = repo.UpdateWithEvent(&op, &event)
					case "context_update":
						got, err = repo.UpdateWithEventContext(context.Background(), &op, &event)
					}
				}
				if err != nil || got == nil || got.SourceRevisionHash != op.SourceRevisionHash || got.SourceIdentityHash != op.SourceIdentityHash || got.SourceURI != op.SourceURI || got.Version != 2 {
					t.Fatalf("permitted update refused/corrupted: %+v / %v", got, err)
				}
			})
		}
	}
}
