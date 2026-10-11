package safety

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestRedactSecretsPreservesBenignJSONBytes(t *testing.T) {
	inputs := []string{
		`{"z":1,"a":2}`,
		" \t{\n  \"z\": [true, null, {\"b\":2, \"a\":1}],\n  \"a\": \"ordinary\"\n}\r\n",
		`{"description":"<ordinary>& \u0061 \/ \"quote\" \\ path","amount":1.2300e+10}`,
		`[ {"z": 1, "a": 2}, false, null, 9007199254740993 ]`,
		`"ordinary \u0061 <text>"`,
		" \n42\t ",
		`{"z":"first","z":"second","a":null}`,
		` {"password": "[REDACTED]", "z": 1, "a": 2} `,
		`{"z":"token=[REDACTED]","a":"password=\"[REDACTED]\""}`,
	}
	for _, input := range inputs {
		if got := RedactSecrets(input); got != input {
			t.Errorf("benign JSON changed: got %q; want %q", got, input)
		}
	}
}

func TestRedactSecretsAgentTeamComparisonCompatibility(t *testing.T) {
	// Mirrors containsAgentTeamSecret's marshal-and-compare contract without
	// importing its package back into safety. The literal key guard stays tested separately.
	request := struct {
		Version          string   `json:"version"`
		Name             string   `json:"name"`
		Purpose          string   `json:"purpose"`
		AuthorityCeiling int      `json:"authorityCeiling"`
		AdvisoryOnly     bool     `json:"advisoryOnly"`
		EvidenceRefs     []string `json:"evidenceRefs"`
	}{"1.0.0", "Governed review team", "Coordinate bounded planning and review.", 4, true, []string{"audit://team/creation"}}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if RedactSecrets(string(payload)) != string(payload) {
		t.Fatal("marshal-and-compare rejected a benign agent-team-shaped request")
	}
}

func TestRedactSecretsCredentialFieldAliasesAcrossRepresentations(t *testing.T) {
	for _, key := range []string{"private_key", "private-key", "privateKey", "passphrase", "credential", "credentials", "encryption_key", "encryption-key", "encryptionKey"} {
		t.Run(key, func(t *testing.T) {
			if !IsSensitiveKey(key) {
				t.Fatalf("credential alias %q is not recognized", key)
			}
			payload, err := json.Marshal(map[string]string{key: "synthetic-credential", "status": "keep"})
			if err != nil {
				t.Fatal(err)
			}
			for _, input := range []string{string(payload), key + "=synthetic-credential status=keep", key + `: "synthetic-credential" status=keep`} {
				got := RedactSecrets(input)
				if strings.Contains(got, "synthetic-credential") || !strings.Contains(got, "status") || !strings.Contains(got, "keep") {
					t.Errorf("credential alias leaked or benign context lost: %q", got)
				}
				if RedactSecrets(got) != got {
					t.Errorf("redaction is not idempotent: %q", got)
				}
			}
		})
	}
	for _, key := range []string{"private_network", "credential_count", "encryption_enabled", "public_key_fingerprint", "status", "field"} {
		payload, _ := json.Marshal(map[string]string{key: "ordinary"})
		if IsSensitiveKey(key) || RedactSecrets(string(payload)) != string(payload) {
			t.Errorf("benign metadata %q was changed", key)
		}
	}
}

func TestRedactSecretsFiltersCredentialsInJSONFieldNames(t *testing.T) {
	for _, input := range []string{
		`{"Bearer synthetic-field-credential":"synthetic-unsafe-value","status":"keep"}`,
		`{"nested":{"B\u0065arer synthetic-field-credential":{"value":"synthetic-unsafe-value"}},"status":"keep"}`,
		`{"github_pat_abcdefghijklmnopqrstuvwxyz123456":"synthetic-unsafe-value","status":"keep"}`,
		`{"https://example.invalid/?token=synthetic-field-credential":"synthetic-unsafe-value","status":"keep"}`,
	} {
		got := RedactSecrets(input)
		if strings.Contains(got, "synthetic-") || strings.Contains(got, "abcdefghijklmnopqrstuvwxyz123456") || !json.Valid([]byte(got)) {
			t.Errorf("JSON field-name credentials leaked: %q", got)
		}
		var output map[string]any
		if err := json.Unmarshal([]byte(got), &output); err != nil || output["status"] != "keep" {
			t.Errorf("benign JSON context changed: %q", got)
		}
		if RedactSecrets(got) != got {
			t.Errorf("JSON field-name filtering is not idempotent: %q", got)
		}
	}
}

func TestRedactSecretsNoOpDoesNotWaiveSensitiveKeysOrLimits(t *testing.T) {
	for _, input := range []string{
		`{"key":"governed-review-team"}`,
		`{"executionAuthorizationRequired":true}`,
		`{"authorization":null}`,
		`{"api_key":42}`,
		`{"client_secret":[]}`,
		`{"refresh_token":{}}`,
	} {
		if got := RedactSecrets(input); got == input || !strings.Contains(got, "[REDACTED]") {
			t.Fatalf("sensitive-key guard was waived: %q", got)
		}
	}
	belowDepth := strings.Repeat("[", maxRedactionDepth-1) + "0" + strings.Repeat("]", maxRedactionDepth-1)
	if RedactSecrets(belowDepth) != belowDepth {
		t.Fatal("benign JSON below the depth bound changed")
	}
	atDepth := "[" + belowDepth + "]"
	if got := RedactSecrets(atDepth); got == atDepth || !strings.Contains(got, "[REDACTED_DEPTH_LIMIT]") {
		t.Fatal("benign JSON bypassed the depth guard")
	}
	atSize := `"` + strings.Repeat("<", maxRedactionBytes-2) + `"`
	if RedactSecrets(atSize) != atSize {
		t.Fatal("benign JSON at the input size bound changed")
	}
	if RedactSecrets(atSize+" ") != "[REDACTED_SIZE_LIMIT]" {
		t.Fatal("valid benign JSON bypassed the size guard")
	}
}

func TestRedactSecretsDuplicateJSONValuesCannotHideChanges(t *testing.T) {
	inputs := []string{
		`{"password":"synthetic-shadowed","password":"[REDACTED]"}`,
		`{"message":"B\u0065arer synthetic-shadowed-value","message":"ordinary"}`,
		`{"message":{"password":false},"message":"ordinary"}`,
		`{"message":` + strings.Repeat("[", maxRedactionDepth+1) + `0` + strings.Repeat("]", maxRedactionDepth+1) + `,"message":"ordinary"}`,
	}
	for _, input := range inputs {
		got := RedactSecrets(input)
		if got == input || !json.Valid([]byte(got)) || strings.Contains(got, "synthetic-") {
			t.Fatalf("shadowed value escaped sanitization: %q", got)
		}
	}
}

func TestRedactSecretsQuotedHTTPErrorURL(t *testing.T) {
	inputs := []string{
		`fetch json-feed: Get "http://127.0.0.1:1/feed?token=synthetic-source-value": dial tcp 127.0.0.1:1: connect: connection refused`,
		`Get "https://example.invalid/feed?token=synthetic-head\"synthetic-tail": dial tcp: refused`,
		`Get "https://example.invalid/feed?access_token=synthetic-access": dial tcp: refused`,
	}
	for _, input := range inputs {
		got := RedactSecrets(input)
		if strings.Contains(got, "synthetic-") || !strings.Contains(got, "token= [REDACTED]") || !strings.Contains(got, `": dial tcp`) {
			t.Fatalf("quoted URL leaked or error context changed: %q", got)
		}
		if RedactSecrets(got) != got {
			t.Fatalf("HTTP error redaction is not idempotent: %q", got)
		}
	}
}

func TestQuotedURLsUseDecodedSensitiveQueryPolicy(t *testing.T) {
	for _, query := range []string{
		"sessionToken=synthetic-secret", "key=synthetic-secret", "%74oken=synthetic-secret",
		"sessionToken=synthetic-secret&sessionToken=synthetic-second", "token=%zzsynthetic-secret",
	} {
		input := `Get "https://example.invalid/feed?` + query + `": dial tcp: refused`
		got := RedactSecrets(input)
		if strings.Contains(got, "synthetic-") || !strings.Contains(got, `": dial tcp: refused`) {
			t.Fatalf("URL policy leaked a query or removed diagnostic context: %q", got)
		}
		if RedactSecrets(got) != got {
			t.Fatalf("quoted URL policy is not idempotent: %q", got)
		}
	}
	input := `Get "https://ordinary:synthetic-secret@example.invalid/feed?sort=z&order=a": dial tcp: refused`
	if got := RedactSecrets(input); strings.Contains(got, "synthetic-") || !strings.Contains(got, `": dial tcp: refused`) {
		t.Fatalf("quoted URL userinfo leaked: %q", got)
	}
	ordinary := `Get "https://example.invalid/feed?z=1&a=2": dial tcp: refused`
	if RedactSecrets(ordinary) != ordinary {
		t.Fatal("ordinary quoted URL changed")
	}
}

func TestStructuredAndPlainURLsUseDecodedSensitiveQueryPolicy(t *testing.T) {
	for _, input := range []string{
		"https://example.invalid/feed?sessionToken=synthetic-secret",
		`{"url":"https://example.invalid/feed?%74oken=synthetic-secret"}`,
		`"https://ordinary:synthetic-secret@example.invalid/feed?key=synthetic-secret"`,
	} {
		got := RedactSecrets(input)
		if strings.Contains(got, "synthetic-secret") || !strings.Contains(got, "REDACTED") {
			t.Fatalf("URL value escaped credential sanitization: %q", got)
		}
		if RedactSecrets(got) != got {
			t.Fatalf("URL value redaction is not idempotent: %q", got)
		}
	}
}

func TestMalformedAndGoQuotedURLsFailClosedWithoutLosingExplanation(t *testing.T) {
	for _, raw := range []string{
		"https://example.invalid/%zz?sessionToken=synthetic-secret",
		"https://[invalid/feed?key=synthetic-secret",
	} {
		if got := RedactSecrets(raw); strings.Contains(got, "synthetic-secret") || !strings.Contains(got, "REDACTED") {
			t.Fatalf("malformed absolute URL escaped sanitization: %q", got)
		}
	}
	for _, input := range []string{
		`parse "https://example.invalid/%zz?sessionToken=synthetic-secret": invalid URL escape`,
		`parse "https://example.invalid/feed?sessionToken=synthetic-secret\x7f": invalid control character`,
		`parse "https://example.invalid/feed?sessionToken=synthetic-secret\xff": invalid byte`,
		`parse "https://example.invalid/feed?sessionToken=synthetic-secret\xzz": invalid escape`,
	} {
		got := RedactSecrets(input)
		if strings.Contains(got, "synthetic-secret") || !strings.Contains(got, `": invalid`) {
			t.Fatalf("malformed quoted URL leaked or lost explanation: %q", got)
		}
		if RedactSecrets(got) != got {
			t.Fatalf("malformed quoted URL redaction is not idempotent: %q", got)
		}
	}
	ordinary := `diagnostic "ordinary\xzz": invalid escape`
	if RedactSecrets(ordinary) != ordinary {
		t.Fatal("non-URL undecodable quote was discarded")
	}
}

func TestRedactSecretsStructuredJSON(t *testing.T) {
	f1Input := `{"access_token":"` + "synthetic-secret-123" + `","refresh_token":"synthetic-refresh-456"}`
	tests := []struct {
		name, input, want string
	}{
		{"F1", f1Input, `{"access_token":"[REDACTED]","refresh_token":"[REDACTED]"}`},
		{"nested arrays", `{"data":[{"password":"synthetic-password","nested":[{"client_secret":"synthetic-client","api_key":"synthetic-api"}]}],"ok":true,"count":9007199254740993,"none":null}`, `{"data":[{"password":"[REDACTED]","nested":[{"client_secret":"[REDACTED]","api_key":"[REDACTED]"}]}],"ok":true,"count":9007199254740993,"none":null}`},
		{"escaped strings and key", `{"pass\u0077ord":"synthetic-quote\"suffix\\tail\nline","description":"ordinary \"quote\" and \\ path","access_token":"synthetic-\u0073ecret"}`, `{"password":"[REDACTED]","description":"ordinary \"quote\" and \\ path","access_token":"[REDACTED]"}`},
		{"root array and secret containers", `[{"token":["synthetic-array",{"value":"synthetic-nested"}],"secret":{"value":"synthetic-object"},"name":"keep"},42,false,null]`, `[{"token":"[REDACTED]","secret":"[REDACTED]","name":"keep"},42,false,null]`},
		{"key variants", `{"AUTHORIZATION":"Basic synthetic-basic","api-key":"synthetic-hyphen","apiKey":"synthetic-camel","pwd":"synthetic-pwd","key":"synthetic-key","sessionToken":123456,"clientSecret":false}`, `{"AUTHORIZATION":"[REDACTED]","api-key":"[REDACTED]","apiKey":"[REDACTED]","pwd":"[REDACTED]","key":"[REDACTED]","sessionToken":"[REDACTED]","clientSecret":"[REDACTED]"}`},
		{"nonsecret data", `{"name":"keep","amount":1.2300e+10,"items":[true,false,null,{},[]],"description":"spaces, semicolons; quotes \"ok\""}`, `{"name":"keep","amount":1.2300e+10,"items":[true,false,null,{},[]],"description":"spaces, semicolons; quotes \"ok\""}`},
		{"patterns inside nonsecret strings", `{"message":"Bearer synthetic-bearer-value ghp_abcdefghijklmnopqrstuvwxyz123456"}`, `{"message":"Bearer [REDACTED] [REDACTED_PROVIDER_TOKEN]"}`},
		{"duplicate sensitive keys", `{"password":"synthetic-first","password":"synthetic-second","name":"keep"}`, `{"password":"[REDACTED]","name":"keep"}`},
		{"root string", `"Bearer synthetic-bearer-value"`, `"Bearer [REDACTED]"`},
		{"pretty multiline JSON", "{\n\"password\":\"synthetic-multiline\",\n\"data\":[{\"client_secret\":\"synthetic-nested\",\"name\":\"keep\"}]\n}", `{"password":"[REDACTED]","data":[{"client_secret":"[REDACTED]","name":"keep"}]}`},
		{"embedded JSON string", `{"message":"{\"password\":\"synthetic-embedded\",\"name\":\"keep\"}"}`, `{"message":"{\"password\":\"[REDACTED]\",\"name\":\"keep\"}"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := RedactSecrets(tt.input)
			decode := func(raw string) any {
				d := json.NewDecoder(strings.NewReader(raw))
				d.UseNumber()
				var value any
				if err := d.Decode(&value); err != nil {
					t.Fatalf("invalid JSON output: %v; %q", err, raw)
				}
				return value
			}
			if !reflect.DeepEqual(decode(output), decode(tt.want)) {
				t.Fatalf("got %s; want %s", output, tt.want)
			}
			if strings.Contains(output, "synthetic-") {
				t.Fatalf("synthetic secret leaked: %s", output)
			}
			if again := RedactSecrets(tt.input); again != output {
				t.Fatalf("nondeterministic output: %q != %q", again, output)
			}
			if again := RedactSecrets(output); again != output {
				t.Fatalf("not idempotent: %q != %q", again, output)
			}
		})
	}
}

func TestRedactSecretsMixedAndMalformedJSON(t *testing.T) {
	tests := []struct {
		name, input, keep string
	}{
		{"mixed log", `INFO result={"access_token":"synthetic-access","refresh_token":"synthetic-refresh","name":"keep"} done`, `"name":"keep"`},
		{"escaped value", `log {"password":"synthetic-head\"synthetic-tail\\synthetic-end","name":"keep"} suffix`, `"name":"keep"`},
		{"escaped key", `log {"pass\u0077ord":"synthetic-escaped-key","name":"keep"}`, `"name":"keep"`},
		{"invalid JSON", `{"password":"synthetic-invalid",broken,"name":"keep"}`, `"name":"keep"`},
		{"truncated string", `INFO {"refresh_token":"synthetic-truncated\"synthetic-tail`, "INFO"},
		{"truncated escape", `{"password":"synthetic-trailing\`, "password"},
		{"truncated object", `INFO {"secret":{"ordinary":"synthetic-container"`, "INFO"},
		{"secret array", `INFO {"token":["synthetic-one",{"ordinary":"synthetic-two"}],"name":"keep"}`, `"name":"keep"`},
		{"mismatched container", `INFO {"secret":["synthetic-one"},"synthetic-two"]}`, "INFO"},
		{"multiline", "start\n{\n\"password\": \"synthetic-head\nsynthetic-tail\",\n\"name\":\"keep\"\n}\nend", `"name":"keep"`},
		{"NDJSON", "{\"password\":\"synthetic-one\",\"name\":\"keep\"}\n{\"access_token\":\"synthetic-two\"}", `"name":"keep"`},
		{"single quotes", `log {'password':'synthetic-head\'synthetic-tail','name':'keep'}`, `'name':'keep'`},
		{"unquoted assignment with quoted value", `password="synthetic-head\"synthetic-tail with spaces" name=keep`, "name=keep"},
		{"quoted numeric value", `log {"token":123456789,"name":"keep"}`, `"name":"keep"`},
		{"quoted-key assignment", `log "client_secret"="synthetic-assignment" name=keep`, "name=keep"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := RedactSecrets(tt.input)
			if strings.Contains(output, "synthetic-") || strings.Contains(output, "123456789") {
				t.Fatalf("secret leaked: %q", output)
			}
			if !strings.Contains(output, tt.keep) || !strings.Contains(output, "[REDACTED]") {
				t.Fatalf("missing preserved field or marker: %q", output)
			}
			if RedactSecrets(tt.input) != output || RedactSecrets(output) != output {
				t.Fatalf("not deterministic/idempotent: %q", output)
			}
		})
	}
}

func TestRedactSecretsExistingPatterns(t *testing.T) {
	input := "password=hunter2 token: abcdefghij Authorization: Bearer secret-token-value sk-abcdefghijklmnopqrstuvwxyz123456 ghp_abcdefghijklmnopqrstuvwxyz123456 xoxb-abcdefghijklmnopqrstuvwxyz123456\n-----BEGIN PRIVATE KEY-----\nsynthetic-private\n-----END PRIVATE KEY-----"
	output := RedactSecrets(input)
	for _, secret := range []string{"hunter2", "abcdefghij", "secret-token-value", "abcdefghijklmnopqrstuvwxyz123456", "synthetic-private"} {
		if strings.Contains(output, secret) {
			t.Fatalf("existing pattern leaked %q: %s", secret, output)
		}
	}
	if !strings.Contains(output, "[REDACTED_PRIVATE_KEY]") || !strings.Contains(output, "[REDACTED_PROVIDER_TOKEN]") {
		t.Fatalf("existing pattern markers lost: %q", output)
	}
}

func TestRedactSecretsEveryTruncatedStringBoundary(t *testing.T) {
	secret, err := json.Marshal("synthetic-head\"synthetic-tail\\synthetic-end\nline")
	if err != nil {
		t.Fatal(err)
	}
	for cut := 0; cut <= len(secret); cut++ {
		input := `INFO {"password":` + string(secret[:cut])
		output := RedactSecrets(input)
		want := `INFO {"password":"[REDACTED]"`
		if cut == 0 {
			want = `INFO {"password":[REDACTED]`
		}
		if output != want {
			t.Fatalf("cut %d: got %q; want %q", cut, output, want)
		}
	}
}

func TestRedactSecretsResourceLimits(t *testing.T) {
	t.Run("oversize input", func(t *testing.T) {
		input := strings.Repeat("x", maxRedactionBytes) + `{"password":"synthetic-oversize"}`
		if got := RedactSecrets(input); got != "[REDACTED_SIZE_LIMIT]" {
			t.Fatalf("oversize output not discarded: length %d", len(got))
		}
	})
	t.Run("output expansion", func(t *testing.T) {
		input := "[" + strings.Repeat(`{"token":""},`, 60000) + "null]"
		if len(input) > maxRedactionBytes {
			t.Fatal("fixture must fit the input bound")
		}
		if got := RedactSecrets(input); got != "[REDACTED_SIZE_LIMIT]" {
			t.Fatalf("expanded output not discarded: length %d", len(got))
		}
	})
	t.Run("structured depth", func(t *testing.T) {
		input := `{"keep":"visible","data":` + strings.Repeat("[", maxRedactionDepth+10) + `{"password":"synthetic-deep"}` + strings.Repeat("]", maxRedactionDepth+10) + "}"
		got := RedactSecrets(input)
		if !json.Valid([]byte(got)) || strings.Contains(got, "synthetic-") || !strings.Contains(got, "[REDACTED_DEPTH_LIMIT]") || !strings.Contains(got, "visible") {
			t.Fatalf("depth bound failed: %q", got)
		}
		if RedactSecrets(got) != got {
			t.Fatalf("depth-bound output not idempotent: %q", got)
		}
	})
	t.Run("fallback depth", func(t *testing.T) {
		input := `INFO {"secret":` + strings.Repeat("[", maxRedactionDepth+10) + `"synthetic-deep"` + strings.Repeat("]", maxRedactionDepth+10)
		if got := RedactSecrets(input); got != `INFO {"secret":[REDACTED]` {
			t.Fatalf("fallback depth not fail-closed: %q", got)
		}
	})
	t.Run("decoder depth limit", func(t *testing.T) {
		input := `{"password":"synthetic-before","data":` + strings.Repeat("[", 10001) + `{"refresh_token":"synthetic-after"}` + strings.Repeat("]", 10001) + "}"
		got := RedactSecrets(input)
		if strings.Contains(got, "synthetic-") {
			t.Fatalf("standard decoder limit fallback leaked: %q", got)
		}
	})
}

func FuzzRedactSecretsQuotedValues(f *testing.F) {
	for _, seed := range []string{"synthetic-secret", "synthetic-quote\"tail\\end\nline", "", "\x00\xff", "[{\"nested\":true}]"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, secret string) {
		if len(secret) > 4096 {
			t.Skip()
		}
		encoded, err := json.Marshal(struct {
			Password string `json:"password"`
			Name     string `json:"name"`
		}{secret, "keep"})
		if err != nil {
			t.Fatal(err)
		}
		for _, prefix := range []string{"", "INFO "} {
			input := prefix + string(encoded)
			output := RedactSecrets(input)
			if prefix != "" {
				if output != `INFO {"password":"[REDACTED]","name":"keep"}` {
					t.Fatalf("mixed output leaked/changed: %q", output)
				}
			} else {
				if output != `{"name":"keep","password":"[REDACTED]"}` {
					t.Fatalf("structured output leaked/changed: %q", output)
				}
			}
			if RedactSecrets(input) != output || RedactSecrets(output) != output {
				t.Fatalf("not deterministic/idempotent: %q", output)
			}
		}
	})
}

func TestRedactSecretsRemovesCommonSecretValues(t *testing.T) {
	input := "password=hunter2 token: abcdefghij Authorization: Bearer secret-token-value sk-abcdefghijklmnopqrstuvwxyz123456"
	output := RedactSecrets(input)

	for _, leaked := range []string{"hunter2", "abcdefghij", "secret-token-value", "sk-abcdefghijklmnopqrstuvwxyz123456"} {
		if strings.Contains(output, leaked) {
			t.Fatalf("redacted output leaked %q: %s", leaked, output)
		}
	}
	if strings.Count(output, "[REDACTED]") < 2 {
		t.Fatalf("expected redaction markers in %q", output)
	}
}

func TestRedactURLRemovesUserInfoAndSensitiveQueryValues(t *testing.T) {
	output := RedactURL("https://user:pass@example.com/run?token=abc123456&project=hai&api_key=secret-key")

	for _, leaked := range []string{"user:pass", "abc123456", "secret-key"} {
		if strings.Contains(output, leaked) {
			t.Fatalf("redacted URL leaked %q: %s", leaked, output)
		}
	}
	if !strings.Contains(output, "project=hai") {
		t.Fatalf("non-sensitive query value should remain: %s", output)
	}
}

func TestRedactURLPreservesNonsecretDataWithKeyVariants(t *testing.T) {
	output := RedactURL("https://user:synthetic-userinfo@example.invalid/path?api-key=synthetic-api&pwd=synthetic-password&Authorization=synthetic-auth&project=hai#keep")
	if strings.Contains(output, "synthetic-") || !strings.Contains(output, "project=hai") || !strings.HasSuffix(output, "#keep") {
		t.Fatalf("URL secrets leaked or ordinary data lost: %q", output)
	}
}

func TestRedactSecretsCookiesAndFineGrainedProviderTokens(t *testing.T) {
	for _, input := range []string{
		`{"cookie":"session=synthetic-cookie; csrf=synthetic-csrf","name":"keep"}`,
		`{"Set-Cookie":"session=synthetic-cookie; HttpOnly","name":"keep"}`,
		`{"setCookie":"session=synthetic-cookie; HttpOnly","name":"keep"}`,
		`{"set_cookie":"session=synthetic-cookie; HttpOnly","name":"keep"}`,
		"Cookie: session=synthetic-cookie; csrf=synthetic-csrf\nname=keep",
		"Set-Cookie: session=synthetic-cookie; Path=/; HttpOnly\nname=keep",
		"cookie=session=synthetic-cookie; csrf=synthetic-csrf\nname=keep",
		"https://example.invalid/feed?cookie=synthetic-cookie&project=keep",
		"diagnostic github_pat_abcdefghijklmnopqrstuvwxyz0123456789 name=keep",
	} {
		got := RedactSecrets(input)
		if strings.Contains(got, "synthetic-") || strings.Contains(got, "github_pat_") || !strings.Contains(got, "keep") || !strings.Contains(got, "REDACTED") {
			t.Fatalf("credential leaked or ordinary context lost: %q", got)
		}
		if RedactSecrets(got) != got {
			t.Fatalf("credential redaction is not idempotent: %q", got)
		}
	}
}

func TestRedactSecretsIncompletePrivateKeysDiscardUntrustedTail(t *testing.T) {
	for _, kind := range []string{"PRIVATE KEY", "RSA PRIVATE KEY", "EC PRIVATE KEY", "OPENSSH PRIVATE KEY"} {
		prefix := "name=keep\n"
		input := prefix + "-----BEGIN " + kind + "-----\nsynthetic-private-body\n"
		if got := RedactSecrets(input); got != prefix+"[REDACTED_PRIVATE_KEY]" {
			t.Fatalf("incomplete private key was not discarded: %q", got)
		}
		complete := input + "-----END " + kind + "-----\nstatus=keep"
		if got := RedactSecrets(complete); got != prefix+"[REDACTED_PRIVATE_KEY]\nstatus=keep" {
			t.Fatalf("complete private key boundary lost: %q", got)
		}
	}
	benign := "-----BEGIN CERTIFICATE-----\npublic-certificate\n-----END CERTIFICATE-----"
	if RedactSecrets(benign) != benign {
		t.Fatal("ordinary public certificate changed")
	}
}
