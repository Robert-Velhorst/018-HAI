package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestRuntimeRoleProvisionersRejectExamplePasswordsBeforeExternalCommands(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		need []string
		cmd  string
	}{
		{
			name: "windows operator wrapper",
			path: "../../scripts/provision-runtime-db-role.ps1",
			need: []string{
				"$ownerPassword -like 'change-this-*'",
				"$password -like 'change-this-*'",
			},
			cmd: "& docker compose",
		},
		{
			name: "container role reconciler",
			path: "../../services/postgres-runtime-role/provision-runtime-role.sh",
			need: []string{
				`case "$PGPASSWORD" in` + "\n" + "  change-this-*)",
				`case "$HAI_RUNTIME_DB_PASSWORD" in` + "\n" + "  change-this-*)",
			},
			cmd: "psql -X -v ON_ERROR_STOP=1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contents, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatalf("read provisioning entry point: %v", err)
			}
			source := strings.ReplaceAll(string(contents), "\r\n", "\n")
			commandAt := strings.Index(source, tc.cmd)
			if commandAt < 0 {
				t.Fatalf("expected external command %q not found", tc.cmd)
			}
			for _, fragment := range tc.need {
				guardAt := strings.Index(source, fragment)
				if guardAt < 0 {
					t.Errorf("missing placeholder rejection %q", fragment)
					continue
				}
				if guardAt > commandAt {
					t.Errorf("placeholder rejection %q occurs after the external command", fragment)
				}
			}
		})
	}
}
