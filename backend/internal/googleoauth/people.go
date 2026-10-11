package googleoauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrPeopleSyncTokenExpired = errors.New("people sync token expired")

const (
	DefaultPeopleBaseURL = "https://people.googleapis.com/v1"
	peopleFields         = "names,emailAddresses,phoneNumbers,organizations,addresses,birthdays,metadata"
	maxPeopleBodyBytes   = 4 << 20
)

type PeopleClient struct {
	AccessToken string
	BaseURL     string
	HTTPClient  *http.Client
}

type PersonValue struct {
	Value string `json:"value"`
}

type PersonName struct {
	DisplayName string `json:"displayName"`
}

type PersonOrganization struct {
	Name  string `json:"name"`
	Title string `json:"title"`
}

type PersonAddress struct {
	FormattedValue string `json:"formattedValue"`
}

type PersonBirthday struct {
	Date struct {
		Year  int `json:"year"`
		Month int `json:"month"`
		Day   int `json:"day"`
	} `json:"date"`
}

type Person struct {
	ResourceName   string               `json:"resourceName"`
	Etag           string               `json:"etag"`
	Names          []PersonName         `json:"names"`
	EmailAddresses []PersonValue        `json:"emailAddresses"`
	PhoneNumbers   []PersonValue        `json:"phoneNumbers"`
	Organizations  []PersonOrganization `json:"organizations"`
	Addresses      []PersonAddress      `json:"addresses"`
	Birthdays      []PersonBirthday     `json:"birthdays"`
	Metadata       struct {
		Deleted bool `json:"deleted"`
	} `json:"metadata"`
}

type PeoplePage struct {
	Connections   []Person `json:"connections"`
	NextPageToken string   `json:"nextPageToken"`
	NextSyncToken string   `json:"nextSyncToken"`
}

func (c PeopleClient) ListConnectionsPage(
	ctx context.Context,
	pageToken string,
	syncToken string,
	pageSize int,
) (PeoplePage, error) {
	if !validGoogleEndpoint(c.baseURL(), "people.googleapis.com", "/v1") {
		return PeoplePage{}, fmt.Errorf("people API endpoint must use HTTPS on Google's People API host or a loopback test server")
	}
	if pageSize <= 0 || pageSize > 1000 {
		pageSize = 100
	}
	query := url.Values{}
	query.Set("personFields", peopleFields)
	query.Set("pageSize", strconv.Itoa(pageSize))
	query.Set("sortOrder", "LAST_MODIFIED_DESCENDING")
	if strings.TrimSpace(pageToken) != "" {
		query.Set("pageToken", pageToken)
	}
	if strings.TrimSpace(syncToken) != "" {
		query.Set("syncToken", syncToken)
	} else {
		query.Set("requestSyncToken", "true")
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.baseURL()+"/people/me/connections?"+query.Encode(),
		nil,
	)
	if err != nil {
		return PeoplePage{}, err
	}
	request.Header.Set("Authorization", "Bearer "+c.AccessToken)
	request.Header.Set("Accept", "application/json")
	response, err := c.httpClient().Do(request)
	if err != nil {
		return PeoplePage{}, fmt.Errorf("people request failed: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxPeopleBodyBytes+1))
	if err != nil {
		return PeoplePage{}, err
	}
	if len(body) > maxPeopleBodyBytes {
		return PeoplePage{}, fmt.Errorf("people response exceeded the safety limit")
	}
	if response.StatusCode == http.StatusUnauthorized {
		return PeoplePage{}, fmt.Errorf("people returned 401: access token is invalid or expired")
	}
	if response.StatusCode == http.StatusGone {
		return PeoplePage{}, ErrPeopleSyncTokenExpired
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return PeoplePage{}, fmt.Errorf("people returned HTTP %d", response.StatusCode)
	}
	var page PeoplePage
	if err := json.Unmarshal(body, &page); err != nil {
		return PeoplePage{}, fmt.Errorf("people returned unparseable JSON: %w", err)
	}
	return page, nil
}

func (c PeopleClient) baseURL() string {
	if strings.TrimSpace(c.BaseURL) != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return DefaultPeopleBaseURL
}

func (c PeopleClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}
