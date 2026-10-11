package authentication

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuthCookiesAreSecureByDefault(t *testing.T) {
	t.Setenv("IDP_COOKIE_SECURE", "")
	recorder := httptest.NewRecorder()

	setAccessTokenCookie(recorder, "access-token", time.Now().Add(time.Hour))

	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure {
		t.Fatalf("default cookie = %#v, want one Secure cookie", cookies)
	}
}

func TestAuthCookiesAllowExplicitInsecureLocalHTTP(t *testing.T) {
	t.Setenv("IDP_COOKIE_SECURE", "false")
	recorder := httptest.NewRecorder()

	setRefreshTokenCookie(recorder, "refresh-token", time.Now().Add(time.Hour))

	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Secure {
		t.Fatalf("local cookie = %#v, want one explicitly insecure cookie", cookies)
	}
}

func TestClearAuthCookiesExpiresAccessAndRefreshTokens(t *testing.T) {
	t.Setenv("IDP_COOKIE_SECURE", "true")
	recorder := httptest.NewRecorder()

	clearAuthCookies(recorder)

	cookies := recorder.Result().Cookies()
	if len(cookies) != 3 {
		t.Fatalf("cookies = %d, want access, refresh, and remember-me cookies", len(cookies))
	}
	deleted := map[string]bool{}
	for _, cookie := range cookies {
		if cookie.MaxAge >= 0 || !cookie.HttpOnly || !cookie.Secure || cookie.Path != "/" {
			t.Fatalf("deletion cookie %q is not hardened: %#v", cookie.Name, cookie)
		}
		deleted[cookie.Name] = true
	}
	for _, name := range []string{"access_token", "refresh_token", rememberMeCookie} {
		if !deleted[name] {
			t.Errorf("missing deletion for %q", name)
		}
	}
}

func TestSessionAuthCookiesRemainHttpOnlySecureAndSameSiteWithoutPersistence(t *testing.T) {
	t.Setenv("IDP_COOKIE_SECURE", "true")
	recorder := httptest.NewRecorder()
	expires := time.Now().Add(24 * time.Hour)
	setAccessTokenCookieWithPersistence(recorder, "access-token", expires, false)
	setRefreshTokenCookieWithPersistence(recorder, "refresh-token", expires, false)
	setRememberMeCookie(recorder, false, expires)

	cookies := recorder.Result().Cookies()
	if len(cookies) != 3 {
		t.Fatalf("cookies = %d, want 3", len(cookies))
	}
	for _, cookie := range cookies {
		if !cookie.Expires.IsZero() || cookie.MaxAge != 0 {
			t.Errorf("cookie %q should be browser-session scoped: %#v", cookie.Name, cookie)
		}
		if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
			t.Errorf("cookie %q lost security attributes: %#v", cookie.Name, cookie)
		}
	}
}

func TestPersistentAuthCookiesUseConfiguredTokenExpirations(t *testing.T) {
	t.Setenv("IDP_COOKIE_SECURE", "true")
	recorder := httptest.NewRecorder()
	accessExpires := time.Unix(1_900_000_000, 0)
	refreshExpires := time.Unix(1_900_086_400, 0)
	setAccessTokenCookieWithPersistence(recorder, "access-token", accessExpires, true)
	setRefreshTokenCookieWithPersistence(recorder, "refresh-token", refreshExpires, true)
	setRememberMeCookie(recorder, true, refreshExpires)

	byName := map[string]*http.Cookie{}
	for _, cookie := range recorder.Result().Cookies() {
		byName[cookie.Name] = cookie
	}
	for name, expected := range map[string]time.Time{
		"access_token":   accessExpires,
		"refresh_token":  refreshExpires,
		rememberMeCookie: refreshExpires,
	} {
		cookie := byName[name]
		if cookie == nil || !cookie.Expires.Equal(expected) {
			t.Errorf("cookie %q = %#v, want expiration %v", name, cookie, expected)
		}
	}
}
