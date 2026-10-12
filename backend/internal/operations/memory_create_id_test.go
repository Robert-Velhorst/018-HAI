package operations

import (
	"reflect"
	"testing"
)

func TestMemoryCreateCannotReplaceExistingOperationIdentity(t *testing.T) {
	fixture := newClaimedEffectFixture(t, true)
	before := claimedEffectTakeSnapshot(fixture.repo)
	replacement := fixture.op
	replacement.DedupeKey = "different-dedupe-key"
	replacement.OwnerUserID = "another-owner"
	if _, err := fixture.repo.Create(&replacement); err == nil {
		t.Fatal("direct create replaced an existing running operation")
	}
	if !reflect.DeepEqual(claimedEffectTakeSnapshot(fixture.repo), before) {
		t.Fatal("duplicate identity changed operation, claim or audit")
	}
}
