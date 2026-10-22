package googleoauth

import (
	"net/http"
	"time"
)

// Keep bearer-token API requests on the endpoint validated by the connector.
func googleAPIHTTPClient(configured *http.Client, defaultTimeout time.Duration) *http.Client {
	client := http.Client{Timeout: defaultTimeout}
	if configured != nil {
		client = *configured
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &client
}
