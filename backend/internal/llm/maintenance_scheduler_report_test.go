package llm

import "testing"

func TestModelMaintenanceNeedsReportForUpdatesOrFailures(t *testing.T) {
	tests := []struct {
		name string
		run  ModelMaintenanceRun
		want bool
	}{
		{name: "quiet reused run", run: ModelMaintenanceRun{Reused: 2}, want: false},
		{name: "provider-managed cloud models", run: ModelMaintenanceRun{Eligible: 2, ProviderManaged: 2}, want: false},
		{name: "model update", run: ModelMaintenanceRun{Updated: 1}, want: true},
		{name: "model failure", run: ModelMaintenanceRun{Failed: 1}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := modelMaintenanceNeedsReport(test.run); got != test.want {
				t.Fatalf("modelMaintenanceNeedsReport(%#v) = %t, want %t", test.run, got, test.want)
			}
		})
	}
}
