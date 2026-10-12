package source

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"automation-hub-backend/internal/durablejob"
)

const maxProviderSyncRetryAfter = durablejob.MaxRetryAfter

// providerSyncErrorOptions carries trusted policy metadata parsed at the
// provider boundary. RetryAfterHeader must be the response header value, not
// a provider body or a caller-supplied request field.
type providerSyncErrorOptions struct {
	RetryableOverride *bool
	RetryAfterHeader  string
	Now               time.Time
	PublicMessage     string
}

// providerSyncError carries a safe public message and retry guidance for a
// failed remote read. The original error is retained for internal errors.As
// checks, but is never included in the response body.
type providerSyncError struct {
	provider       string
	message        string
	statusCode     int
	upstreamStatus int
	retryable      bool
	retryAfter     time.Duration
	cause          error
}

func (e *providerSyncError) Error() string {
	if e == nil {
		return "provider sync failed"
	}
	return fmt.Sprintf("%s sync failed: %s", e.provider, e.message)
}

func (e *providerSyncError) Unwrap() error { return e.cause }

// Retryable implements the durable worker's optional retry-classification
// contract without coupling source errors to the durablejob package.
func (e *providerSyncError) Retryable() bool { return e != nil && e.retryable }

// RetryAfter is a bounded minimum delay before a durable retry.
func (e *providerSyncError) RetryAfter() time.Duration {
	if e == nil {
		return 0
	}
	return e.retryAfter
}

func newProviderSyncError(provider string, upstreamStatus int, cause error) *providerSyncError {
	return newProviderSyncErrorWithOptions(provider, upstreamStatus, cause, providerSyncErrorOptions{})
}

func newProviderSyncErrorWithOptions(provider string, upstreamStatus int, cause error, options providerSyncErrorOptions) *providerSyncError {
	result := &providerSyncError{
		provider:       strings.TrimSpace(provider),
		statusCode:     http.StatusBadGateway,
		upstreamStatus: upstreamStatus,
		cause:          cause,
	}
	if result.provider == "" {
		result.provider = "provider"
	}
	contextFailure := errors.Is(cause, context.DeadlineExceeded) || errors.Is(cause, context.Canceled)
	switch {
	case errors.Is(cause, context.DeadlineExceeded):
		result.statusCode = http.StatusGatewayTimeout
		result.retryable = true
		result.message = "provider request timed out; retry after checking connection health"
	case errors.Is(cause, context.Canceled):
		result.statusCode = http.StatusServiceUnavailable
		result.retryable = true
		result.message = "provider request was interrupted; retry when the connection is available"
	case upstreamStatus == http.StatusTooManyRequests:
		result.statusCode = http.StatusServiceUnavailable
		result.retryable = true
		result.message = "provider rate limit remained active after bounded retries"
	case upstreamStatus == http.StatusRequestTimeout:
		result.statusCode = http.StatusGatewayTimeout
		result.retryable = true
		result.message = "provider request timed out; retry after checking connection health"
	case upstreamStatus >= 300 && upstreamStatus < 400:
		result.message = "provider redirected the request; review the source configuration"
	case upstreamStatus >= http.StatusInternalServerError:
		result.retryable = upstreamStatus != http.StatusNotImplemented && upstreamStatus != http.StatusHTTPVersionNotSupported
		if result.retryable {
			result.message = "provider returned a temporary error; retry after checking connection health"
		} else {
			result.message = "provider does not support this request; review the source configuration"
		}
	case upstreamStatus == http.StatusUnauthorized || upstreamStatus == http.StatusForbidden:
		result.message = "provider rejected the configured credentials; reconnect or review account access"
	case upstreamStatus >= http.StatusBadRequest:
		result.message = "provider rejected the read request; review the source configuration"
	default:
		result.retryable = true
		result.message = "provider request failed; retry after checking connection health"
	}

	// Providers can classify ambiguous statuses (for example, a GitHub 403
	// carrying rate-limit headers) without weakening the default fail-closed
	// treatment of ordinary authorization failures.
	if !contextFailure && options.RetryableOverride != nil {
		result.retryable = *options.RetryableOverride
		if result.retryable && upstreamStatus == http.StatusForbidden {
			result.statusCode = http.StatusServiceUnavailable
			result.message = "provider temporarily refused the request; retry after checking connection health"
		} else if result.retryable && (upstreamStatus == http.StatusUnauthorized || upstreamStatus >= http.StatusInternalServerError) {
			result.message = "provider temporarily refused the request; retry after checking connection health"
		} else if !result.retryable && upstreamStatus >= http.StatusInternalServerError && upstreamStatus != http.StatusNotImplemented && upstreamStatus != http.StatusHTTPVersionNotSupported {
			result.message = "provider response requires manual review before another attempt"
		} else if !result.retryable && upstreamStatus == 0 {
			result.message = "provider response could not be processed; review the source configuration"
		}
	}

	if !contextFailure && result.retryable {
		now := options.Now
		if now.IsZero() {
			now = time.Now().UTC()
		}
		delay, valid, exceedsLimit := parseProviderRetryAfter(options.RetryAfterHeader, now)
		if exceedsLimit {
			// Do not clamp a valid longer provider delay and then retry too early.
			// Require manual review instead of scheduling an unsafe automatic retry.
			result.retryable = false
			result.message = "provider requested a delay beyond the automatic retry limit; review before retrying"
		} else if valid {
			result.retryAfter = delay
		}
	}
	if strings.TrimSpace(options.PublicMessage) != "" {
		result.message = strings.TrimSpace(options.PublicMessage)
	}
	return result
}

func parseProviderRetryAfter(value string, now time.Time) (time.Duration, bool, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false, false
	}
	if isDecimalRetryAfter(value) {
		seconds, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return 0, true, true
		}
		if seconds < 0 {
			return 0, false, false
		}
		maxSeconds := int64(maxProviderSyncRetryAfter / time.Second)
		if seconds > maxSeconds {
			return 0, true, true
		}
		return time.Duration(seconds) * time.Second, true, false
	}

	date, err := http.ParseTime(value)
	if err != nil {
		return 0, false, false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	delay := date.Sub(now)
	if delay < 0 {
		delay = 0
	}
	if delay > maxProviderSyncRetryAfter {
		return 0, true, true
	}
	return delay, true, false
}

func isDecimalRetryAfter(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

type syncFailureDetails struct {
	statusCode int
	message    string
	retryable  bool
	retryAfter time.Duration
}

func classifySyncFailure(err error) (syncFailureDetails, bool) {
	if err == nil {
		return syncFailureDetails{}, false
	}
	var providerErr *providerSyncError
	if errors.As(err, &providerErr) {
		return syncFailureDetails{
			statusCode: providerErr.statusCode,
			message:    providerErr.Error(),
			retryable:  providerErr.Retryable(),
			retryAfter: providerErr.RetryAfter(),
		}, true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return syncFailureDetails{
			statusCode: http.StatusGatewayTimeout,
			message:    "source sync timed out; retry after checking connection health",
			retryable:  true,
		}, true
	}
	if errors.Is(err, context.Canceled) {
		return syncFailureDetails{
			statusCode: http.StatusServiceUnavailable,
			message:    "source sync was canceled before completion",
			retryable:  true,
		}, true
	}
	return syncFailureDetails{}, false
}
