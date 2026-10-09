package googleoauth

import (
	"fmt"
	"net/http"
)

// ProviderAPIError preserves only safe response metadata needed by callers to
// classify retries. Provider response bodies are deliberately not retained.
type ProviderAPIError struct {
	Provider         string
	StatusCode       int
	RetryAfterHeader string
	cause            error
}

func (e *ProviderAPIError) Error() string {
	if e == nil {
		return "Google provider request failed"
	}
	provider := e.Provider
	if provider == "" {
		provider = "Google"
	}
	return fmt.Sprintf("%s API returned HTTP %d", provider, e.StatusCode)
}

func (e *ProviderAPIError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func newProviderAPIError(provider string, response *http.Response, cause error) *ProviderAPIError {
	return &ProviderAPIError{
		Provider:         provider,
		StatusCode:       response.StatusCode,
		RetryAfterHeader: response.Header.Get("Retry-After"),
		cause:            cause,
	}
}
