package background

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"automation-hub-backend/internal/operations"
)

func TestPublicSafeExecutionWithoutClaimRefusesBeforeIntent(t *testing.T) {
	service := operations.NewService(operations.NewMemoryRepository())
	op := createReadyRegressionOperation(t, service)
	beforeEvents, err := service.Events(op.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(t.TempDir(), "workspace")
	broker := newAuthorizedBackgroundTestBroker(t, workspace, op.OwnerUserID, op.WorkspaceID)
	out, err := ExecuteSafeOperation(context.Background(), service, broker, op, time.Now())
	if !errors.Is(err, operations.ErrSafeEffectUnsupported) || out.Operation != nil || out.Receipt != nil {
		t.Fatalf("public unclaimed dispatch reached execution: %+v / %v", out, err)
	}
	stored, err := service.Get(op.OwnerUserID, op.WorkspaceID, op.ID)
	if err != nil || !reflect.DeepEqual(*stored, op) {
		t.Fatalf("refusal mutated operation intent: %+v / %v", stored, err)
	}
	afterEvents, err := service.Events(op.ID)
	if err != nil || !reflect.DeepEqual(beforeEvents, afterEvents) {
		t.Fatalf("refusal mutated audit history: %v", err)
	}
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatalf("refusal entered filesystem: %v", err)
	}
}
