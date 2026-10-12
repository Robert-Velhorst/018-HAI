package accountfeed

import "testing"

func TestTrelloBridgeRequiresBothLiveAdapterCredentials(t *testing.T) {
	t.Setenv("TRELLO_API_KEY", "")
	t.Setenv("TRELLO_READ_TOKEN", "")

	bridge, ok := Bridge(ProviderTrello)
	if !ok {
		t.Fatal("Trello bridge is missing")
	}
	if bridge.CredentialEnv != "TRELLO_API_KEY, TRELLO_READ_TOKEN" {
		t.Fatalf("Trello credential setup=%q", bridge.CredentialEnv)
	}
	if status := bridge.ConnectionStatus(); status != ConnCredentialsRequired {
		t.Fatalf("Trello without credentials status=%q", status)
	}

	t.Setenv("TRELLO_READ_TOKEN", "read-token")
	if status := bridge.ConnectionStatus(); status != ConnCredentialsRequired {
		t.Fatalf("Trello without API key status=%q", status)
	}

	t.Setenv("TRELLO_API_KEY", "api-key")
	if status := bridge.ConnectionStatus(); status != ConnCredentialsPresentUnverified {
		t.Fatalf("Trello with both credentials status=%q", status)
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
	if permission.Status != ConnCredentialsRequired {
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
	if permission.Status != ConnCredentialsPresentUnverified {
		t.Fatalf("Gmail bootstrap-configured status=%q", permission.Status)
	}
	if permission.Granted {
		t.Fatal("Google OAuth client bootstrap must not be reported as an owner grant")
	}
}
