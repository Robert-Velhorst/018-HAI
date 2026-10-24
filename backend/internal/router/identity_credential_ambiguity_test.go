package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/identity"
)

func TestIdentityRejectsAmbiguousCredentialCollections(t *testing.T) {
	previous := config.AppConfig.JWTSecret
	config.AppConfig.JWTSecret = "router-ambiguity-fixture-secret"
	t.Cleanup(func() { config.AppConfig.JWTSecret = previous })
	now := time.Now()
	sign := func(role string) string {
		return identity.SignToken(identity.Claims{Subject: role + "-user", Role: role, Issuer: "hai-idp", Audience: "hai", TokenType: "access", IssuedAt: now.Unix(), Expiry: now.Add(time.Hour).Unix()}, config.AppConfig.JWTSecret)
	}
	owner, viewer := sign("owner"), sign("viewer")
	for _, test := range []struct {
		name             string
		headers, cookies []string
		want             int
	}{
		{"valid then invalid Authorization", []string{"Bearer " + owner, "Bearer invalid"}, nil, http.StatusUnauthorized},
		{"invalid then valid Authorization", []string{"Bearer invalid", "Bearer " + owner}, nil, http.StatusUnauthorized},
		{"identical Authorization", []string{"Bearer " + owner, "Bearer " + owner}, nil, http.StatusUnauthorized},
		{"empty then valid Authorization with cookie", []string{"", "Bearer " + owner}, []string{owner}, http.StatusUnauthorized},
		{"valid then empty Authorization", []string{"Bearer " + owner, ""}, nil, http.StatusUnauthorized},
		{"valid then invalid access cookie", nil, []string{owner, "invalid"}, http.StatusUnauthorized},
		{"identical access cookies", nil, []string{owner, owner}, http.StatusUnauthorized},
		{"different signed cookie identities", nil, []string{owner, viewer}, http.StatusUnauthorized},
		{"single Authorization", []string{"Bearer " + owner}, nil, http.StatusOK},
		{"single cookie", nil, []string{owner}, http.StatusOK},
		{"explicit bearer retains cookie precedence", []string{"Bearer " + owner}, []string{viewer}, http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/x", nil)
			for _, header := range test.headers {
				request.Header.Add("Authorization", header)
			}
			for _, cookie := range test.cookies {
				request.AddCookie(&http.Cookie{Name: "access_token", Value: cookie})
			}
			request.AddCookie(&http.Cookie{Name: "unrelated", Value: "fixture"})
			response := httptest.NewRecorder()
			newIdentityEngine().ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d", response.Code, test.want)
			}
		})
	}
}
