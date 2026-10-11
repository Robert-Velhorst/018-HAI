// Package apierror defines a small, stable catalog of API error codes and a
// consistent JSON error envelope. Handlers can map failures to a known code so
// clients and the troubleshooting guide share one vocabulary.
package apierror

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"automation-hub-backend/internal/safety"
)

// Code is a stable, machine-readable error identifier.
type Code string

const (
	CodeBadRequest   Code = "bad_request"
	CodeUnauthorized Code = "unauthorized"
	CodeForbidden    Code = "forbidden"
	CodeNotFound     Code = "not_found"
	CodeConflict     Code = "conflict"
	CodeValidation   Code = "validation_failed"
	CodeRateLimited  Code = "rate_limited"
	CodeUnavailable  Code = "unavailable"
	CodeInternal     Code = "internal_error"
)

// HTTPStatus maps a code to its HTTP status. Unknown codes default to 500 so a
// missing mapping fails safe rather than leaking a 200.
func (c Code) HTTPStatus() int {
	switch c {
	case CodeBadRequest:
		return http.StatusBadRequest
	case CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict:
		return http.StatusConflict
	case CodeValidation:
		return http.StatusUnprocessableEntity
	case CodeRateLimited:
		return http.StatusTooManyRequests
	case CodeUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// Error is a structured API error carrying a code, a human message, and
// optional field-level details.
type Error struct {
	Code    Code              `json:"code"`
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
}

// MarshalJSON applies the public redaction boundary even when callers serialize
// an Error directly instead of wrapping it in Envelope.
func (e *Error) MarshalJSON() ([]byte, error) {
	if e == nil {
		return []byte("null"), nil
	}
	public := e.Envelope().Error
	type wireError Error
	return json.Marshal((*wireError)(public))
}

// New builds an error with the given code and message.
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// WithDetail attaches a field-level detail and returns the error for chaining.
func (e *Error) WithDetail(field, detail string) *Error {
	if e.Details == nil {
		e.Details = map[string]string{}
	}
	e.Details[field] = detail
	return e
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	return string(e.Code) + ": " + safety.RedactSecrets(e.Message)
}

// HTTPStatus returns the HTTP status for this error's code.
func (e *Error) HTTPStatus() int { return e.Code.HTTPStatus() }

// Envelope is the top-level shape returned to clients: {"error": {...}}.
type Envelope struct {
	Error *Error `json:"error"`
}

// Envelope captures a filtered public snapshot without modifying the original error.
func (e *Error) Envelope() Envelope {
	if e == nil {
		return Envelope{}
	}
	public := &Error{Code: e.Code, Message: safety.RedactSecrets(e.Message)}
	if e.Details != nil {
		public.Details = make(map[string]string, len(e.Details))
		for field, detail := range e.Details {
			safeField := safety.RedactSecrets(field)
			if safeField != field || safety.IsSensitiveKey(field) {
				public.Details[safeField] = "[REDACTED]"
			} else {
				public.Details[safeField] = safety.RedactSecrets(detail)
			}
		}
	}
	return Envelope{Error: public}
}

// PublicMessage returns a stable fallback for unexpected errors. A handler may
// pass a deliberately constructed API Error when its message is safe for the
// caller; all other errors can contain provider, database, filesystem, or
// credential context and must not be echoed in an HTTP response.
func PublicMessage(err error, fallback string) string {
	fallback = strings.TrimSpace(fallback)
	if fallback == "" {
		fallback = "The request could not be completed"
	}
	var public *Error
	if !errors.As(err, &public) || public == nil {
		return fallback
	}
	message := strings.TrimSpace(safety.RedactSecrets(public.Message))
	if message == "" || len(message) > 500 {
		return fallback
	}
	return message
}
