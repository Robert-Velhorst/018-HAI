package workflow

import (
	"encoding/hex"
	"reflect"
	"testing"
)

func TestWorkflowSourceRevisionSeparatesNestedValues(t *testing.T) {
	base := IntakeRequest{Input: "Review email", SourceType: "email", SourceID: "message-1"}
	t.Run("success criteria", func(t *testing.T) {
		first, second := base, base
		first.SuccessCriteria = []string{"Keep draft only\x1dCite sources"}
		second.SuccessCriteria = []string{"Keep draft only", "Cite sources"}
		if workflowSourceRevision(first, first.Input) == workflowSourceRevision(second, second.Input) {
			t.Fatal("different success criteria share a source revision")
		}
	})
	t.Run("selection context", func(t *testing.T) {
		first := workflowSourceRevision(base, base.Input, "first\x1esecond")
		second := workflowSourceRevision(base, base.Input, "first", "second")
		if first == second {
			t.Fatal("different selection contexts share a source revision")
		}
	})
	t.Run("adjacent scalar fields", func(t *testing.T) {
		first, second := base, base
		first.SourceLabel, first.ContentType = "first\x1fsecond", "third"
		second.SourceLabel, second.ContentType = "first", "second\x1fthird"
		if workflowSourceRevision(first, first.Input) == workflowSourceRevision(second, second.Input) {
			t.Fatal("different source field boundaries share a source revision")
		}
	})
	t.Run("literal escape prefix", func(t *testing.T) {
		first, second := base, base
		first.SuccessCriteria = []string{"first\x1dsecond"}
		second.SuccessCriteria = []string{"~hai-revision:" + hex.EncodeToString([]byte(first.SuccessCriteria[0]))}
		if workflowSourceRevision(first, first.Input) == workflowSourceRevision(second, second.Input) {
			t.Fatal("literal escape syntax aliases the value it encodes")
		}
	})
}

func TestWorkflowSourceRevisionPreservesOrdinaryLegacyDigest(t *testing.T) {
	request := IntakeRequest{
		Input: "Review email", SourceType: "email", SourceID: "message-1",
		SuccessCriteria: []string{"Keep draft only", "Cite sources"},
	}
	const legacy = "cba201638227f6404bfb6034db5b283a0580b4506dbaaab64971f5e5faf6719a"
	if got := workflowSourceRevision(request, request.Input, "selection-a", "selection-b"); got != legacy {
		t.Fatalf("ordinary legacy revision changed: got %s, want %s", got, legacy)
	}
}

func TestIntakeDoesNotDeduplicateChangedCriterionBoundaries(t *testing.T) {
	service := NewService(newFakeWorkflowRepo())
	request := IntakeRequest{
		OwnerIdentity: "alice", Input: "Prepare a source-backed draft.", ProjectKey: "018-HAI",
		SourceType: "email", SourceID: "criterion-boundaries", RequiresReview: true,
		SuccessCriteria: []string{"Keep draft only\x1dCite sources"},
	}
	first, err := service.Intake(request)
	if err != nil {
		t.Fatalf("first intake: %v", err)
	}
	request.SuccessCriteria = []string{"Keep draft only", "Cite sources"}
	second, err := service.Intake(request)
	if err != nil {
		t.Fatalf("changed intake: %v", err)
	}
	if first.Item.ID == second.Item.ID || first.Item.SourceRevision == second.Item.SourceRevision {
		t.Fatal("changed criterion boundaries reused the earlier workflow")
	}
	if !reflect.DeepEqual(second.Item.SuccessCriteria, request.SuccessCriteria) {
		t.Fatalf("stored criteria=%q, want %q", second.Item.SuccessCriteria, request.SuccessCriteria)
	}
	replay, err := service.Intake(request)
	if err != nil || replay == nil || replay.Item.ID != second.Item.ID {
		t.Fatalf("unchanged revised input did not replay: err=%v", err)
	}
}
