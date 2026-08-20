package executionapproval

import (
	"context"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/task"

	"github.com/google/uuid"
)

func TestOpsControlReviewResolverRequiresExactEffectBinding(t *testing.T) {
	now := time.Now().UTC()
	binding := strings.Repeat("a", 64)
	repository := task.NewMemoryTaskStateRepository()
	item, err := repository.CreateReviewItem("owner", task.ReviewQueueItem{
		ID:     uuid.NewString(),
		TaskID: "opscontrol:resume:" + binding,
		Request: task.IntakeRequest{
			OwnerIdentity: "owner",
			Request:       "Resume the current emergency-stop revision.",
			ProjectKey:    "runtime-control",
		},
		Reason:    "Owner review required.",
		Priority:  "high",
		Status:    "open",
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create review: %v", err)
	}
	if _, err := repository.ResolveReviewItem("owner", item.ID, task.ReviewResolution{
		Decision:   "approved",
		ResolvedAt: now,
	}); err != nil {
		t.Fatalf("resolve review: %v", err)
	}

	resolver, err := NewOpsControlReviewResolver(repository)
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	resolver.now = func() time.Time { return now.Add(time.Minute) }
	sourceID := OpsControlReviewPrefix + item.ID
	approved, err := resolver.Resolve(context.Background(), "owner", sourceID, binding)
	if err != nil || approved.SourceID != sourceID || approved.BindingDigest != binding {
		t.Fatalf("approved review = %#v, %v", approved, err)
	}

	if _, err := resolver.Resolve(context.Background(), "owner", sourceID, strings.Repeat("b", 64)); err == nil {
		t.Fatal("mismatched exact effect binding was accepted")
	}
}
