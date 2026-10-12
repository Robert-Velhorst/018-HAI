package models

import (
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestOpenClawReceiptMatchesMigrationTable(t *testing.T) {
	parsed, err := schema.Parse(&OpenClawGatewaySessionReceipt{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Table != "openclaw_gateway_session_receipts" {
		t.Fatalf("ORM table %q differs from migration table", parsed.Table)
	}
}

func TestOpenClawArtifactMatchesMigrationTable(t *testing.T) {
	parsed, err := schema.Parse(&OpenClawGatewayArtifactReceipt{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Table != "openclaw_gateway_artifact_receipts" {
		t.Fatalf("ORM table %q differs from migration table", parsed.Table)
	}
}
