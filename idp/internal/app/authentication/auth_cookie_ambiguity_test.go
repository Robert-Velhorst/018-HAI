package authentication

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"automation-hub-idp/internal/app/dto"

	"github.com/gin-gonic/gin"
)

type countingCookieAuthService struct {
	*middlewareAuthService
	authenticationCalls int
}

func (s *countingCookieAuthService) IsUserAuthenticated(ctx context.Context, token string) (bool, error) {
	s.authenticationCalls++
	return s.middlewareAuthService.IsUserAuthenticated(ctx, token)
}

func TestIdentityRejectsAmbiguousSessionCookiesBeforeAuthenticationOrRefresh(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name                          string
		cookies                       []http.Cookie
		expired                       bool
		wantStatus, checks, refreshes int
	}{
		{name: "duplicate access", cookies: []http.Cookie{{Name: "access_token", Value: "valid"}, {Name: "access_token", Value: "other"}}, wantStatus: 401},
		{name: "duplicate empty access with refresh", cookies: []http.Cookie{{Name: "access_token", Value: ""}, {Name: "access_token", Value: "valid"}, {Name: "refresh_token", Value: "refresh"}}, wantStatus: 401},
		{name: "duplicate refresh", cookies: []http.Cookie{{Name: "refresh_token", Value: "refresh"}, {Name: "refresh_token", Value: "other"}}, wantStatus: 401},
		{name: "duplicate refresh with valid access", cookies: []http.Cookie{{Name: "access_token", Value: "valid"}, {Name: "refresh_token", Value: "refresh"}, {Name: "refresh_token", Value: "other"}}, wantStatus: 401},
		{name: "single access", cookies: []http.Cookie{{Name: "access_token", Value: "valid"}}, wantStatus: 200, checks: 1},
		{name: "single refresh", cookies: []http.Cookie{{Name: "refresh_token", Value: "refresh"}}, wantStatus: 200, refreshes: 1},
		{name: "expired access with refresh", cookies: []http.Cookie{{Name: "access_token", Value: "expired"}, {Name: "refresh_token", Value: "refresh"}}, expired: true, wantStatus: 200, checks: 1, refreshes: 1},
		{name: "unrelated duplicates", cookies: []http.Cookie{{Name: "access_token", Value: "valid"}, {Name: "other", Value: "one"}, {Name: "other", Value: "two"}}, wantStatus: 200, checks: 1},
		{name: "no credentials", wantStatus: 401},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &countingCookieAuthService{middlewareAuthService: &middlewareAuthService{
				valid:         !test.expired,
				refreshResult: &dto.TokenDetails{AccessToken: "fresh", AtExpires: time.Now().Add(time.Hour).Unix()},
			}}
			router := gin.New()
			router.GET("/check", NewHandler(service).IsUserAuthenticated)
			request := httptest.NewRequest(http.MethodGet, "/check", nil)
			for _, cookie := range test.cookies {
				request.AddCookie(&cookie)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != test.wantStatus || service.authenticationCalls != test.checks || service.refreshCalls != test.refreshes {
				t.Fatalf("status=%d checks=%d refreshes=%d; want status=%d checks=%d refreshes=%d",
					recorder.Code, service.authenticationCalls, service.refreshCalls, test.wantStatus, test.checks, test.refreshes)
			}
		})
	}
}
