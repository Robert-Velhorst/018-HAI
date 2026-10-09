package memory

import (
	"testing"
)

// buildMultiProjectHandler seeds memories across two project keys to verify that
// project-scoped listing never leaks another project's data.
func buildMultiProjectHandler(t *testing.T) *Handler {
	t.Helper()
	service := NewService(newFakeRepository())
	scoped := service.(OwnerScopedService)
	seed := []CreateRequest{
		{ProjectKey: "project-a", Kind: "note", Content: "alpha one"},
		{ProjectKey: "project-a", Kind: "note", Content: "alpha two"},
		{ProjectKey: "project-a", Kind: "note", Content: "alpha three"},
		{ProjectKey: "project-b", Kind: "note", Content: "bravo one"},
		{ProjectKey: "project-b", Kind: "note", Content: "bravo two"},
	}
	for _, req := range seed {
		if _, err := scoped.CreateForOwner("test-owner", req); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return NewHandler(service)
}

func TestQueryIsolatesByProjectKey(t *testing.T) {
	h := buildMultiProjectHandler(t)

	a := doQuery(t, h, "projectKey=project-a")
	if a.Total != 3 {
		t.Fatalf("project-a total = %d, want 3", a.Total)
	}
	for _, m := range a.Items {
		if m.ProjectKey != "project-a" {
			t.Fatalf("project-a query leaked memory from %q", m.ProjectKey)
		}
	}

	b := doQuery(t, h, "projectKey=project-b")
	if b.Total != 2 {
		t.Fatalf("project-b total = %d, want 2", b.Total)
	}
	for _, m := range b.Items {
		if m.ProjectKey != "project-b" {
			t.Fatalf("project-b query leaked memory from %q", m.ProjectKey)
		}
	}
}

func TestQueryWithoutProjectKeySeesAll(t *testing.T) {
	h := buildMultiProjectHandler(t)
	all := doQuery(t, h, "")
	if all.Total != 5 {
		t.Fatalf("unscoped total = %d, want 5", all.Total)
	}
}
