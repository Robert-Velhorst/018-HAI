package accountfeed

import "testing"

func TestAutoposterBridgeRequiresCompleteCredentialConfiguration(t *testing.T) {
	keys := []string{"HAI_AUTOPOSTER_BASE_URL", "HAI_AUTOPOSTER_TOKEN", "HAI_AUTOPOSTER_REMOTE_OWNER"}
	values := []string{"https://autoposter.example.test", "hai_synthetic_test_token", "synthetic-owner"}
	for _, test := range []struct {
		name    string
		missing int
		blank   string
	}{
		{name: "missing URL", missing: 0},
		{name: "missing token", missing: 1},
		{name: "missing remote owner", missing: 2},
		{name: "whitespace token", missing: 1, blank: " \t\n"},
		{name: "complete but unverified", missing: -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			for i, key := range keys {
				value := values[i]
				if i == test.missing {
					value = test.blank
				}
				t.Setenv(key, value)
			}
			want := ConnCredentialsRequired
			if test.missing < 0 {
				want = ConnCredentialsPresentUnverified
			}
			bridge, ok := Bridge(ProviderAutoposter)
			if !ok {
				t.Fatal("Autoposter bridge is missing")
			}
			if got := bridge.ConnectionStatus(); got != want {
				t.Fatalf("bridge status=%q, want %q", got, want)
			}
			permission, ok := NewPermissionRegistry().Permission(ProviderAutoposter)
			if !ok || permission.Status != want || permission.Granted != (test.missing < 0) {
				t.Fatalf("permission=%+v, present=%t; want status=%q", permission, ok, want)
			}
			if NewPermissionRegistry().WriteAllowed(ProviderAutoposter) {
				t.Fatal("credential configuration must never grant write authority")
			}
		})
	}
}

func TestTrelloBridgeDoesNotInferLiveConnectionFromCredentials(t *testing.T) {
	t.Setenv("TRELLO_API_KEY", "")
	t.Setenv("TRELLO_READ_TOKEN", "")

	bridge, ok := Bridge(ProviderTrello)
	if !ok {
		t.Fatal("Trello bridge is missing")
	}
	if bridge.CredentialEnv != "TRELLO_API_KEY, TRELLO_READ_TOKEN" {
		t.Fatalf("Trello credential setup=%q", bridge.CredentialEnv)
	}
	if status := bridge.ConnectionStatus(); status != ConnContractOnly {
		t.Fatalf("Trello without credentials status=%q, want contract-only", status)
	}

	t.Setenv("TRELLO_READ_TOKEN", "read-token")
	if status := bridge.ConnectionStatus(); status != ConnContractOnly {
		t.Fatalf("Trello with partial credentials status=%q, want contract-only", status)
	}

	t.Setenv("TRELLO_API_KEY", "api-key")
	if status := bridge.ConnectionStatus(); status != ConnContractOnly {
		t.Fatalf("Trello with credentials but no provider read status=%q, want contract-only", status)
	}
}

func TestBridgeContractsNameLiveSourceAdapterConfiguration(t *testing.T) {
	for _, test := range []struct {
		provider Provider
		want     string
	}{
		{provider: ProviderGmail, want: "GOOGLE_OAUTH_CLIENT_ID, GOOGLE_OAUTH_CLIENT_SECRET"},
		{provider: ProviderGoogleDrive, want: "GOOGLE_OAUTH_CLIENT_ID, GOOGLE_OAUTH_CLIENT_SECRET"},
		{provider: ProviderGoogleCalendar, want: "GOOGLE_OAUTH_CLIENT_ID, GOOGLE_OAUTH_CLIENT_SECRET"},
		{provider: ProviderGitHub, want: "GITHUB_SOURCE_TOKEN"},
		{provider: ProviderTrello, want: "TRELLO_API_KEY, TRELLO_READ_TOKEN"},
	} {
		t.Run(test.provider.String(), func(t *testing.T) {
			bridge, ok := Bridge(test.provider)
			if !ok {
				t.Fatalf("bridge missing for %s", test.provider)
			}
			if bridge.CredentialEnv != test.want {
				t.Fatalf("credential setup=%q, want %q", bridge.CredentialEnv, test.want)
			}
		})
	}
}

func TestGoogleBridgeDoesNotTreatLegacyTokenAsAnOAuthGrant(t *testing.T) {
	t.Setenv("GMAIL_OAUTH_TOKEN", "legacy-token")
	t.Setenv("GOOGLE_OAUTH_CLIENT_ID", "")
	t.Setenv("GOOGLE_OAUTH_CLIENT_SECRET", "")

	permission, ok := NewPermissionRegistry().Permission(ProviderGmail)
	if !ok {
		t.Fatal("Gmail permission is missing")
	}
	if permission.Status != ConnContractOnly {
		t.Fatalf("Gmail bootstrap status=%q", permission.Status)
	}
	if permission.Granted {
		t.Fatal("legacy token must not be reported as a Google OAuth grant")
	}

	t.Setenv("GOOGLE_OAUTH_CLIENT_ID", "client-id")
	t.Setenv("GOOGLE_OAUTH_CLIENT_SECRET", "client-secret")
	permission, ok = NewPermissionRegistry().Permission(ProviderGmail)
	if !ok {
		t.Fatal("Gmail permission is missing after OAuth bootstrap configuration")
	}
	if permission.Status != ConnContractOnly {
		t.Fatalf("Gmail bootstrap-configured status=%q", permission.Status)
	}
	if permission.Granted {
		t.Fatal("Google OAuth client bootstrap must not be reported as an owner grant")
	}
}
