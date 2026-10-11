package identity

import (
	"os"
	"strings"
)

const LegacyDataOwnerEnv = "HAI_LEGACY_DATA_OWNER_IDENTITY"

// CanReadLegacyOwnerlessData identifies the single authenticated principal
// allowed to inspect pre-ownership migration records. An unset value fails
// closed so ownerless data never becomes shared operator data by accident.
func CanReadLegacyOwnerlessData(ownerIdentity string) bool {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	legacyOwner := strings.TrimSpace(os.Getenv(LegacyDataOwnerEnv))
	return ownerIdentity != "" && legacyOwner != "" && ownerIdentity == legacyOwner
}
