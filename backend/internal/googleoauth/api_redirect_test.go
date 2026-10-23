package googleoauth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestGoogleAPIRequestsDoNotFollowRedirects(t *testing.T) {
	for _, api := range []struct {
		name string
		read func(string, *http.Client) error
	}{
		{name: "calendar", read: func(base string, client *http.Client) error {
			_, err := (CalendarClient{BaseURL: base, AccessToken: "fixture-token", HTTPClient: client}).ListPrimaryEventsPage(context.Background(), "", "", "", 1)
			return err
		}},
		{name: "drive", read: func(base string, client *http.Client) error {
			_, err := (DriveClient{BaseURL: base, AccessToken: "fixture-token", HTTPClient: client}).GetStartPageToken(context.Background())
			return err
		}},
		{name: "gmail", read: func(base string, client *http.Client) error {
			_, err := (GmailClient{BaseURL: base, AccessToken: "fixture-token", HTTPClient: client}).GetProfileHistoryID(context.Background())
			return err
		}},
		{name: "people", read: func(base string, client *http.Client) error {
			_, err := (PeopleClient{BaseURL: base, AccessToken: "fixture-token", HTTPClient: client}).ListConnectionsPage(context.Background(), "", "", 1)
			return err
		}},
	} {
		for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect} {
			for _, injected := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d/injected=%t", api.name, status, injected), func(t *testing.T) {
					var redirected, forwardedToken atomic.Int32
					target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						redirected.Add(1)
						if r.Header.Get("Authorization") != "" {
							forwardedToken.Add(1)
						}
						_, _ = w.Write([]byte(`{}`))
					}))
					defer target.Close()
					origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						http.Redirect(w, r, target.URL, status)
					}))
					defer origin.Close()
					var client *http.Client
					if injected {
						client = origin.Client()
						client.CheckRedirect = func(*http.Request, []*http.Request) error { return nil }
					}
					err := api.read(origin.URL, client)
					if redirected.Load() != 0 || forwardedToken.Load() != 0 || err == nil {
						t.Fatalf("redirect boundary failed: target requests=%d token-bearing requests=%d error=%v", redirected.Load(), forwardedToken.Load(), err)
					}
					if injected && client.CheckRedirect(nil, nil) != nil {
						t.Fatal("shared caller client was mutated")
					}
				})
			}
		}
	}
}

func TestGoogleAPIClientPreservesCallerTransportAndTimeout(t *testing.T) {
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	configured := &http.Client{
		Jar:           jar,
		Transport:     transport,
		Timeout:       9 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return nil },
	}
	client := googleAPIHTTPClient(configured, 30*time.Second)
	if client == configured || client.Transport != transport || client.Timeout != configured.Timeout || client.Jar != configured.Jar {
		t.Fatal("API boundary lost caller configuration or reused its mutable client")
	}
	if configured.CheckRedirect(nil, nil) != nil || client.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Fatal("redirect policy was not isolated to the connector client copy")
	}
	if googleAPIHTTPClient(nil, 20*time.Second).Timeout != 20*time.Second {
		t.Fatal("default connector timeout changed")
	}
}
