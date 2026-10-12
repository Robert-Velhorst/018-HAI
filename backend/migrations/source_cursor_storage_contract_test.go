package migrations

import (
	"reflect"
	"strings"
	"testing"

	"automation-hub-backend/internal/models"
)

func TestSourceSyncCursorStorageMigrationIsLosslessAndVersioned(t *testing.T) {
	up, err := Files.ReadFile("pre/0104_source_sync_cursor_storage.up.sql")
	if err != nil {
		t.Fatalf("read cursor storage migration: %v", err)
	}
	down, err := Files.ReadFile("pre/0104_source_sync_cursor_storage.down.sql")
	if err != nil {
		t.Fatalf("read cursor storage rollback: %v", err)
	}

	upSQL := strings.Join(strings.Fields(strings.ToLower(string(up))), " ")
	for _, required := range []string{
		"set local lock_timeout = '5s'",
		"alter table public.connected_sources alter column cursor type text",
		"alter table public.source_sync_jobs alter column cursor_before type text",
		"alter column cursor_after type text",
	} {
		if !strings.Contains(upSQL, required) {
			t.Errorf("cursor storage migration is missing %q", required)
		}
	}

	downSQL := strings.Join(strings.Fields(strings.ToLower(string(down))), " ")
	for _, required := range []string{
		"where length(cursor) > 512",
		"length(cursor_before) > 512 or length(cursor_after) > 512",
		"cannot restore 512-character source cursor columns",
		"alter column cursor type character varying(512)",
		"alter column cursor_before type character varying(512)",
	} {
		if !strings.Contains(downSQL, required) {
			t.Errorf("cursor storage rollback is missing loss-prevention guard %q", required)
		}
	}

	for _, field := range []struct {
		model any
		name  string
	}{
		{models.ConnectedSource{}, "Cursor"},
		{models.SourceSyncJob{}, "CursorBefore"},
		{models.SourceSyncJob{}, "CursorAfter"},
	} {
		structField, ok := reflect.TypeOf(field.model).FieldByName(field.name)
		if !ok {
			t.Fatalf("model %T has no %s field", field.model, field.name)
		}
		if got := structField.Tag.Get("gorm"); !strings.Contains(got, "type:text") {
			t.Errorf("%T.%s GORM tag = %q, want type:text", field.model, field.name, got)
		}
	}
}
