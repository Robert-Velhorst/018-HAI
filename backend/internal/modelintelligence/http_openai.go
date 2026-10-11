package modelintelligence

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func newDirectHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		// A model endpoint can receive task context. Do not follow a redirect
		// or inherit environment proxy settings that could move that context
		// outside the configured endpoint boundary.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		Transport:     &http.Transport{Proxy: nil},
	}
}

// validateEndpointURL rejects invalid, unspecified, link-local, and cloud
// metadata hosts (§10.17). Localhost/loopback is allowed for local endpoints.
func validateEndpointURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL scheme must be http/https, got %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL host is empty")
	}
	if strings.Contains(strings.ToLower(host), "metadata") {
		return fmt.Errorf("metadata host not allowed")
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil {
		switch {
		case ip.IsLoopback():
			return nil
		case ip.IsUnspecified():
			return fmt.Errorf("unspecified host not allowed")
		case ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast():
			return fmt.Errorf("link-local host not allowed")
		case ip.String() == "169.254.169.254":
			return fmt.Errorf("metadata host not allowed")
		}
	}
	return nil
}

// validateLocalEndpointURL narrows an OpenAI-compatible endpoint to the
// local machine. host.docker.internal is accepted so the Docker backend can
// reach a server deliberately bound on the Windows host.
func validateLocalEndpointURL(raw string) error {
	return validateLocalEndpointURLForProvider(raw, "")
}

func validateLocalEndpointURLForProvider(raw, providerID string) error {
	if err := validateEndpointURL(raw); err != nil {
		return err
	}
	u, _ := url.Parse(raw)
	host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(u.Hostname())), ".")
	if host == "localhost" || host == "host.docker.internal" || (providerID == "ollama" && host == "ollama-local") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("local provider endpoint must use localhost, loopback, or host.docker.internal")
}

func isLocalEndpointURL(raw string) bool {
	return validateLocalEndpointURL(raw) == nil
}

// probeModelsEndpoint performs a truthful GET against an OpenAI-compatible
// /models path and maps the result onto a ProviderStatus.
func probeModelsEndpoint(ctx context.Context, client *http.Client, providerID, baseURL, probePath string, now time.Time) ProbeResult {
	return probeModelsEndpointWithBearer(ctx, client, providerID, baseURL, probePath, "", now)
}

func probeModelsEndpointWithBearer(ctx context.Context, client *http.Client, providerID, baseURL, probePath, bearerToken string, now time.Time) ProbeResult {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+probePath, nil)
	if err != nil {
		return ProbeResult{ProviderID: providerID, Status: ProviderFailed, Detail: err.Error(), CheckedAt: now}
	}
	if strings.TrimSpace(bearerToken) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearerToken))
	}
	resp, err := client.Do(req)
	if err != nil {
		return ProbeResult{ProviderID: providerID, Status: ProviderUnavailable, Detail: err.Error(), CheckedAt: now}
	}
	defer resp.Body.Close()
	dur := time.Since(start).Milliseconds()
	if resp.StatusCode != http.StatusOK {
		return ProbeResult{ProviderID: providerID, Status: ProviderUnavailable, DurationMs: dur, Detail: fmt.Sprintf("probe HTTP %d", resp.StatusCode), CheckedAt: now}
	}
	var body struct {
		Data []json.RawMessage `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return ProbeResult{ProviderID: providerID, Status: ProviderActive, ModelsSeen: len(body.Data), DurationMs: dur, Detail: "probe ok", CheckedAt: now}
}

// chatCompletion performs a bounded OpenAI-compatible chat completion and
// returns the assistant text plus telemetry.
func chatCompletion(ctx context.Context, client *http.Client, providerID, modelID, baseURL, genPath string, req InferenceRequest) (InferenceResult, error) {
	return chatCompletionWithBearer(ctx, client, providerID, modelID, baseURL, genPath, "", req)
}

func chatCompletionWithBearer(ctx context.Context, client *http.Client, providerID, modelID, baseURL, genPath, bearerToken string, req InferenceRequest) (InferenceResult, error) {
	payload := map[string]any{
		"model":      modelID,
		"messages":   []map[string]string{{"role": "user", "content": req.Prompt}},
		"max_tokens": req.MaxOutputTokens,
	}
	buf, _ := json.Marshal(payload)
	start := time.Now()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+genPath, strings.NewReader(string(buf)))
	if err != nil {
		return InferenceResult{ProviderID: providerID, OK: false, Error: err.Error()}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(bearerToken) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearerToken))
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return InferenceResult{ProviderID: providerID, OK: false, Error: err.Error()}, err
	}
	defer resp.Body.Close()
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return InferenceResult{ProviderID: providerID, OK: false, Error: err.Error()}, err
	}
	text := ""
	if len(out.Choices) > 0 {
		text = out.Choices[0].Message.Content
	}
	res := InferenceResult{ProviderID: providerID, ModelID: modelID, Lane: req.Lane, Output: text, OK: true}
	res.DurationMs = time.Since(start).Milliseconds()
	res.InputTokensEstimate = estimateTokens(req.Prompt)
	res.OutputTokensEstimate = estimateTokens(text)
	if res.DurationMs > 0 {
		res.TokensPerSecond = float64(res.OutputTokensEstimate) / (float64(res.DurationMs) / 1000)
	}
	return res, nil
}
