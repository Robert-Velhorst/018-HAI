package frameworkregistry

import (
	"strings"
	"testing"
)

func TestTypedTeamMetadataDoesNotWaiveCredentialDetection(t *testing.T) {
	team := AgentTeamContract{Key: "governed-review-team", ExecutionAuthorizationRequired: true}
	if containsAgentTeamSecret(team) {
		t.Fatal("typed identity and approval metadata was mistaken for credentials")
	}
	for _, secret := range []string{"token=synthetic-secret", "Bearer synthetic-secret-value", `{"password":false}`} {
		t.Run(secret, func(t *testing.T) {
			withSecret := team
			withSecret.Key = secret
			if !containsAgentTeamSecret(withSecret) {
				t.Fatal("team identifier escaped value scanning")
			}
			withSecret = team
			withSecret.Roles = []TeamRoleContract{{Purpose: secret}}
			if !containsAgentTeamSecret(withSecret) {
				t.Fatal("nested role escaped scanning")
			}
		})
	}
	for _, value := range []any{
		map[string]any{"key": "synthetic-secret"},
		map[string]any{"executionAuthorizationRequired": "synthetic-secret"},
		AgentTeamContract{Key: "review-team", Purpose: strings.Repeat("x", 1<<20)},
	} {
		if !containsAgentTeamSecret(value) {
			t.Fatal("generic credentials or size guard were waived")
		}
	}
	if team.Key != "governed-review-team" || !team.ExecutionAuthorizationRequired {
		t.Fatal("validation changed original team metadata")
	}
}
