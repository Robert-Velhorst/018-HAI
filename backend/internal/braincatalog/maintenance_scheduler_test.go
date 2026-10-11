package braincatalog

import "testing"

func TestCatalogMaintenanceNeedsReportOnlyForFailures(t *testing.T) {
	tests := []struct {
		name string
		run  CatalogRevalidationRun
		want bool
	}{
		{name: "quiet successful run", run: CatalogRevalidationRun{}, want: false},
		{name: "entry failure", run: CatalogRevalidationRun{Failed: 1}, want: true},
		{name: "collection failure", run: CatalogRevalidationRun{CollectionReview: &CatalogCollectionRevalidationRun{Failed: true}}, want: true},
		{name: "discovery failure", run: CatalogRevalidationRun{RepositoryDiscoveryReview: &CatalogRepositoryDiscoveryRevalidationRun{Failed: true}}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := catalogMaintenanceNeedsReport(test.run); got != test.want {
				t.Fatalf("catalogMaintenanceNeedsReport(%#v) = %t, want %t", test.run, got, test.want)
			}
		})
	}
}
