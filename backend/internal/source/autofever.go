package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"automation-hub-backend/internal/models"
)

const (
	autofeverConnectorKey = "autofever"
	autofeverTokenEnv     = "HAI_AUTOFEVER_CONNECTOR_TOKEN"
	autofeverOwnerEnv     = "HAI_AUTOFEVER_CONNECTOR_OWNER_IDENTITY"
)

func autofeverConfigured() bool {
	return autofeverTokenForOwner(strings.TrimSpace(os.Getenv(autofeverOwnerEnv))) != ""
}

func autofeverTokenForOwner(ownerIdentity string) string {
	token := strings.TrimSpace(os.Getenv(autofeverTokenEnv))
	configuredOwner := strings.TrimSpace(os.Getenv(autofeverOwnerEnv))
	if token == "" || configuredOwner == "" || ownerIdentity == "" ||
		strings.TrimSpace(ownerIdentity) != ownerIdentity || ownerIdentity != configuredOwner {
		return ""
	}
	for _, r := range token {
		if r <= 0x20 || r == 0x7f {
			return ""
		}
	}
	return token
}

func autofeverCredentialsBelongToOwner(ownerIdentity string) bool {
	return autofeverTokenForOwner(ownerIdentity) != ""
}

func validateAutofeverTarget(raw string) (*url.URL, error) {
	target, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || target.Scheme == "" || target.Hostname() == "" {
		return nil, fmt.Errorf("Autofever sync target must be an absolute HTTP(S) feed URL")
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return nil, fmt.Errorf("Autofever sync target must use HTTP or HTTPS")
	}
	if target.User != nil {
		return nil, fmt.Errorf("Autofever credentials must not be embedded in syncTarget")
	}
	if target.RawQuery != "" || target.Fragment != "" || target.RawPath != "" {
		return nil, fmt.Errorf("Autofever sync target must not include a query, fragment, or escaped path")
	}
	if target.Path != "/api/integrations/hai/feed" {
		return nil, fmt.Errorf("Autofever sync target path must be /api/integrations/hai/feed")
	}
	if !sourceHTTPHostAllowed(target.Hostname()) {
		return nil, fmt.Errorf("Autofever host %s is not allowlisted; set CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS deliberately", target.Hostname())
	}
	if sourceHTTPAddressBlocked(target.Hostname()) && !sourceHTTPLoopbackAddressAllowed(sourceHTTPURLAddress(target)) {
		return nil, fmt.Errorf("Autofever target uses link-local, metadata, or unspecified address space")
	}
	if target.Scheme == "http" && !sourceHTTPHostIsLoopback(target.Hostname()) && !strings.EqualFold(target.Hostname(), "host.docker.internal") {
		return nil, fmt.Errorf("Autofever requires HTTPS except for loopback or host.docker.internal development targets")
	}
	return target, nil
}

func fetchAutofever(ctx context.Context, source *models.ConnectedSource) ([]ImportItem, string, error) {
	if ctx == nil {
		return nil, "", fmt.Errorf("Autofever request context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if source == nil {
		return nil, "", fmt.Errorf("source is required")
	}
	token := autofeverTokenForOwner(source.OwnerIdentity)
	if token == "" {
		return nil, "", fmt.Errorf("Autofever credentials are not assigned to this connected source owner")
	}
	target, err := validateAutofeverTarget(source.SyncTarget)
	if err != nil {
		return nil, "", err
	}
	if source.Cursor != "" {
		if len(source.Cursor) > 500 {
			return nil, "", fmt.Errorf("Autofever cursor exceeds 500 characters")
		}
		query := target.Query()
		query.Set("cursor", source.Cursor)
		target.RawQuery = query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("create Autofever request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Request-ID", "hai-autofever-"+source.ID.String())
	client := &http.Client{
		Timeout:   sourceHTTPTimeout(),
		Transport: sourceHTTPTransport(),
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("fetch Autofever: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, "", fmt.Errorf("Autofever returned HTTP %d", response.StatusCode)
	}
	maxBytes := sourceHTTPMaxBytes()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read Autofever feed: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, "", fmt.Errorf("Autofever response exceeds %d bytes", maxBytes)
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return []ImportItem{}, source.Cursor, nil
	}
	var envelope jsonFeedEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, "", fmt.Errorf("decode Autofever feed: %w", err)
	}
	nextCursor := strings.TrimSpace(envelope.NextCursor)
	if len(nextCursor) > 500 {
		return nil, "", fmt.Errorf("Autofever next cursor exceeds 500 characters")
	}
	if len(envelope.Items) > 0 && (nextCursor == "" || nextCursor == source.Cursor) {
		return nil, "", fmt.Errorf("Autofever feed returned items without advancing its cursor")
	}
	if nextCursor == "" {
		nextCursor = source.Cursor
	}
	return normalizeFeedItems(envelope.Items, source), nextCursor, nil
}
