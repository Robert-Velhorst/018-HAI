package accountfeed

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"automation-hub-backend/internal/safety"
)

// MarshalJSON protects historical configurations without changing the URL used
// by repositories, validation or fetching. The alias avoids recursive marshaling.
func (f Feed) MarshalJSON() ([]byte, error) {
	type publicFeed Feed
	view := publicFeed(f)
	view.URL = publicFeedURL(f.URL)
	return json.Marshal(view)
}

func publicFeedURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Opaque != "" {
		return "[REDACTED_URL_ERROR]"
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		// Partial query parsing must never leave unexamined credentials visible.
		return "[REDACTED_URL_ERROR]"
	}
	changed := u.User != nil || u.Fragment != "" || u.RawFragment != ""
	u.User, u.Fragment, u.RawFragment = nil, "", ""
	queryChanged := false
	for key := range query {
		if sourceURISecret.MatchString(key + "=") {
			query.Set(key, "[REDACTED]")
			queryChanged = true
		}
	}
	// Canonicalize only this copy so shared URL redaction can be compared
	// without rewriting credential-free URLs or treating '&' as plain text.
	u.RawQuery = query.Encode()
	canonical := u.String()
	redacted := safety.RedactURL(canonical)
	if changed || queryChanged || redacted != canonical {
		return redacted
	}
	return raw
}

// MarshalJSON sanitizes only the public copy, including a separate errors slice.
// Ordinary opaque cursors remain valid; no intake or cursor validation changes.
func (r SyncReport) MarshalJSON() ([]byte, error) {
	type publicReport SyncReport
	view := publicReport(r)
	cursor := strings.ToLower(strings.TrimSpace(r.Cursor))
	if strings.HasPrefix(cursor, "http://") || strings.HasPrefix(cursor, "https://") {
		view.Cursor = publicFeedURL(r.Cursor)
	} else {
		view.Cursor = safety.RedactSecrets(r.Cursor)
	}
	if r.Errors != nil {
		view.Errors = make([]string, len(r.Errors))
		for i, message := range r.Errors {
			if strings.Contains(message, "://") {
				// Keep arbitrary URL credentials/fragments out of diagnostics,
				// using the same public summary as the normal sync error path.
				view.Errors[i] = publicSyncError(errors.New(message))
			} else {
				view.Errors[i] = safety.RedactSecrets(message)
			}
		}
	}
	return json.Marshal(view)
}
