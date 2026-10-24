package autoconfig

import (
	"bytes"
	"strings"
	"testing"
	"text/template"
)

func TestConfigTemplatePreservesParentSecurityHeadersAlongsideRotatedCookies(t *testing.T) {
	tmpl, err := template.New("automation-route").Parse(configTemplate)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := tmpl.Execute(&output, struct {
		URLPath string
		Host    string
		Port    int
	}{"dashboard", "backend", 8080}); err != nil {
		t.Fatal(err)
	}
	rendered := output.String()
	for _, directive := range []string{
		"location /dashboard/ {",
		"auth_request /auth-verify;",
		"add_header_inherit merge;",
		"add_header Set-Cookie $hai_refreshed_access_cookie always;",
		"add_header Set-Cookie $hai_refreshed_refresh_cookie always;",
		"proxy_pass http://$upstream_host:$upstream_port;",
	} {
		if strings.Count(rendered, directive) != 1 {
			t.Errorf("rendered route must contain directive exactly once: %s", directive)
		}
	}
	if strings.Contains(rendered, "add_header_inherit off;") {
		t.Fatal("rendered route disables parent security headers")
	}
}
