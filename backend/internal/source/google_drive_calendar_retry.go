package source

import (
	"errors"
	"net/http"

	"automation-hub-backend/internal/googleoauth"
)

func googleProviderSyncError(provider string, err error) error {
	if err == nil {
		return nil
	}
	var apiErr *googleoauth.ProviderAPIError
	if !errors.As(err, &apiErr) {
		return err
	}
	retryable := apiErr.StatusCode == 429 || apiErr.StatusCode >= 500 && apiErr.StatusCode != http.StatusNotImplemented && apiErr.StatusCode != http.StatusHTTPVersionNotSupported
	return newProviderSyncErrorWithOptions(provider, apiErr.StatusCode, apiErr, providerSyncErrorOptions{
		RetryableOverride: &retryable,
		RetryAfterHeader:  apiErr.RetryAfterHeader,
	})
}
