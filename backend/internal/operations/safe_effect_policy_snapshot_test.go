package operations

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"automation-hub-backend/internal/models"
)

func TestSafeEffectRejectsSubstitutedPolicyContent(t *testing.T) {
	for _, identified := range []bool{false, true} {
		for name, change := range map[string]func(*models.Operation){
			"title":          func(op *models.Operation) { op.Title = "unblocked caller title" },
			"description":    func(op *models.Operation) { op.Description = "unverified caller description" },
			"operation_type": func(op *models.Operation) { op.OperationType = "unblocked_caller_category" },
		} {
			t.Run(name+map[bool]string{false: "/legacy", true: "/identified"}[identified], func(t *testing.T) {
				fixture := newClaimedEffectFixture(t, identified)
				before := claimedEffectTakeSnapshot(fixture.repo)
				expected := fixture.op
				change(&expected)
				called := false
				err := fixture.service.WithClaimedSafeEffect(context.Background(), fixture.claim, expected, func(context.Context) error { called = true; return nil })
				if !errors.Is(err, ErrStaleOperation) || called {
					t.Fatalf("caller policy content reached the effect: called=%t / %v", called, err)
				}
				if after := claimedEffectTakeSnapshot(fixture.repo); !reflect.DeepEqual(before, after) {
					t.Fatal("policy snapshot refusal changed retained repository state")
				}
				if err := fixture.service.WithClaimedSafeEffect(context.Background(), fixture.claim, fixture.op, func(context.Context) error { return nil }); err != nil {
					t.Fatalf("exact locked content was refused: %v", err)
				}
			})
		}
	}
}
