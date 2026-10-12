package infra

import "testing"

func TestConfiguredFirstRunAdminRejectsMissingOrSamplePassword(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		password string
	}{
		{name: "missing password", email: "owner@example.com"},
		{name: "legacy default", email: "owner@example.com", password: "ChangeMe123!"},
		{name: "template placeholder", email: "owner@example.com", password: "change-this-owner-password"},
		{name: "too short", email: "owner@example.com", password: "short-pass"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := configuredFirstRunAdmin(test.email, test.password); err == nil {
				t.Fatal("expected unsafe first-run admin configuration to be rejected")
			}
		})
	}
}

func TestConfiguredFirstRunAdminAcceptsExplicitStrongPassword(t *testing.T) {
	email, password, err := configuredFirstRunAdmin("owner@example.com", "long-unique-owner-password")
	if err != nil {
		t.Fatalf("configuredFirstRunAdmin returned error: %v", err)
	}
	if email != "owner@example.com" || password != "long-unique-owner-password" {
		t.Fatalf("unexpected configured owner: %q / %q", email, password)
	}
}
