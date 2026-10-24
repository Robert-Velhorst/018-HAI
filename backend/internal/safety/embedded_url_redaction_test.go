package safety

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestRedactSecretsRemovesCredentialsFromEmbeddedUnquotedURLs(t *testing.T) {
	password := strings.Join([]string{"fixture", "userinfo"}, "-")
	token := strings.Join([]string{"fixture", "query"}, "-")
	endpoint := &url.URL{Scheme: "https", Host: "provider.example.invalid", Path: "/v1",
		User: url.UserPassword("audit-user", password)}
	endpoint.RawQuery = url.Values{"api_key": {token}, "project": {"hai"}}.Encode()
	plain := "reach " + endpoint.String() + " (OLLAMA_BASE_URL): connection refused"
	encoded, err := json.Marshal(map[string]string{"detail": plain})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{plain, string(encoded)} {
		output := RedactSecrets(input)
		for _, secret := range []string{"audit-user", password, token} {
			if strings.Contains(output, secret) {
				t.Fatalf("embedded URL credential survived redaction: %s", output)
			}
		}
		for _, detail := range []string{"provider.example.invalid", "OLLAMA_BASE_URL", "connection refused"} {
			if !strings.Contains(output, detail) {
				t.Fatalf("useful diagnostic %q lost: %s", detail, output)
			}
		}
		if json.Valid([]byte(input)) && !json.Valid([]byte(output)) {
			t.Fatal("redaction produced invalid JSON")
		}
		if RedactSecrets(output) != output {
			t.Fatal("embedded URL redaction is not idempotent")
		}
	}
}

func TestRedactSecretsPreservesBenignEmbeddedURLsExactly(t *testing.T) {
	input := "reach https://provider.example.invalid/v1?project=hai: connection refused"
	if output := RedactSecrets(input); output != input {
		t.Fatalf("benign diagnostic changed: %q", output)
	}
}

func TestRedactSecretsFailsClosedForMalformedEmbeddedCredentialURL(t *testing.T) {
	password := strings.Join([]string{"fixture", "malformed"}, "-")
	input := "reach https://audit-user:" + password + "@%zz.invalid/v1: connection refused"
	output := RedactSecrets(input)
	if strings.Contains(output, "audit-user") || strings.Contains(output, password) ||
		!strings.Contains(output, "[REDACTED_URL_ERROR]") || !strings.Contains(output, "connection refused") {
		t.Fatalf("malformed embedded URL was not safely redacted: %q", output)
	}
}
