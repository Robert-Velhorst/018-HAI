package identity

import "testing"

func TestCanReadLegacyOwnerlessDataFailsClosedAndMatchesExactly(t *testing.T) {
	t.Setenv(LegacyDataOwnerEnv, "")
	if CanReadLegacyOwnerlessData("alice") {
		t.Fatal("unset legacy owner granted ownerless-data access")
	}
	t.Setenv(LegacyDataOwnerEnv, "owner@example.test")
	if !CanReadLegacyOwnerlessData("owner@example.test") {
		t.Fatal("configured legacy owner was rejected")
	}
	for _, identity := range []string{"", "operator@example.test", "OWNER@example.test"} {
		if CanReadLegacyOwnerlessData(identity) {
			t.Fatalf("unexpected legacy-data access for %q", identity)
		}
	}
}
