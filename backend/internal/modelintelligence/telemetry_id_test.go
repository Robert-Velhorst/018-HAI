package modelintelligence

import (
	"errors"
	"testing"
)

func TestTelemetryIDsAreUniqueAcrossIndependentlySeededStores(t *testing.T) {
	seed := []ModelRunTelemetry{{ID: "existing-row"}}
	first := NewTelemetryStore()
	second := NewTelemetryStore()
	first.Seed(seed)
	second.Seed(seed)

	firstRow, err := first.RecordChecked(ModelRunTelemetry{})
	if err != nil {
		t.Fatalf("record first telemetry row: %v", err)
	}
	secondRow, err := second.RecordChecked(ModelRunTelemetry{})
	if err != nil {
		t.Fatalf("record second telemetry row: %v", err)
	}
	if firstRow.ID == "" || secondRow.ID == "" {
		t.Fatalf("generated IDs must not be empty: first=%q second=%q", firstRow.ID, secondRow.ID)
	}
	if firstRow.ID == secondRow.ID {
		t.Fatalf("independent stores reused telemetry ID %q", firstRow.ID)
	}
}

func TestTelemetryIDGenerationFailureDoesNotAppendRow(t *testing.T) {
	wantErr := errors.New("random source unavailable")
	store := NewTelemetryStore()
	store.newID = func() (string, error) { return "", wantErr }

	row, err := store.RecordChecked(ModelRunTelemetry{ProviderID: "provider"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("RecordChecked() error = %v, want %v", err, wantErr)
	}
	if row.ID != "" {
		t.Fatalf("failed ID generation returned ID %q", row.ID)
	}
	if got := store.All(); len(got) != 0 {
		t.Fatalf("failed ID generation appended %d rows, want none", len(got))
	}
}
