package doctor

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestRunProbesRedactsCredentialsWithoutChangingFailureSeverity(t *testing.T) {
	password := strings.Join([]string{"fixture", "userinfo"}, "-")
	token := strings.Join([]string{"fixture", "query"}, "-")
	endpoint := &url.URL{Scheme: "https", Host: "provider.example.invalid", Path: "/v1",
		User: url.UserPassword("audit-user", password)}
	endpoint.RawQuery = url.Values{"api_key": {token}, "project": {"hai"}}.Encode()
	for _, critical := range []bool{false, true} {
		checks := RunProbes(context.Background(), time.Second, []Probe{{
			Name: "llm.provider", Critical: critical, Run: func(context.Context) error {
				return errors.New("reach " + endpoint.String() + " (OLLAMA_BASE_URL): connection refused")
			},
		}})
		want := SeverityWarn
		if critical {
			want = SeverityFail
		}
		if len(checks) != 1 || checks[0].Name != "llm.provider" || checks[0].Severity != want {
			t.Fatalf("failure semantics changed: %#v", checks)
		}
		for _, secret := range []string{"audit-user", password, token} {
			if strings.Contains(checks[0].Detail, secret) {
				t.Fatalf("readiness detail exposed a credential: %s", checks[0].Detail)
			}
		}
		if !strings.Contains(checks[0].Detail, "connection refused") || !strings.Contains(checks[0].Detail, "OLLAMA_BASE_URL") {
			t.Fatalf("failure cause lost: %s", checks[0].Detail)
		}
	}
}
