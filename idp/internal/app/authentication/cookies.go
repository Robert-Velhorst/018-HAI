package authentication

import (
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	googleOAuthStateCookie      = "hai_google_oauth_state"
	googleOAuthReturnURLCookie  = "hai_google_oauth_return_url"
	googleOAuthRememberMeCookie = "hai_google_remember_me"
	rememberMeCookie            = "hai_remember_me"
	rememberMePersistentValue   = "1"
	rememberMeSessionValue      = "0"
)

func setAccessTokenCookie(w http.ResponseWriter, value string, expires time.Time) {
	setAccessTokenCookieWithPersistence(w, value, expires, true)
}

func setRefreshTokenCookie(w http.ResponseWriter, value string, expires time.Time) {
	setRefreshTokenCookieWithPersistence(w, value, expires, true)
}

func setAccessTokenCookieWithPersistence(w http.ResponseWriter, value string, expires time.Time, persistent bool) {
	setAuthCookieWithPersistence(w, "access_token", value, expires, persistent)
}

func setRefreshTokenCookieWithPersistence(w http.ResponseWriter, value string, expires time.Time, persistent bool) {
	setAuthCookieWithPersistence(w, "refresh_token", value, expires, persistent)
}

func setRememberMeCookie(w http.ResponseWriter, persistent bool, expires time.Time) {
	setAuthCookieWithPersistence(w, rememberMeCookie, rememberMeValue(persistent), expires, persistent)
}

func setAuthCookieWithPersistence(w http.ResponseWriter, name string, value string, expires time.Time, persistent bool) {
	http.SetCookie(w, authCookie(name, value, expires, persistent))
}

func authCookie(name string, value string, expires time.Time, persistent bool) *http.Cookie {
	cookie := &http.Cookie{
		Name:     name,
		Value:    value,
		HttpOnly: true,
		Secure:   authCookieSecure(),
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
	}
	if persistent {
		cookie.Expires = expires
	}
	return cookie
}

func rememberMeValue(persistent bool) string {
	if persistent {
		return rememberMePersistentValue
	}
	return rememberMeSessionValue
}

// Sessions issued before remember-me existed were persistent. Preserve that
// behavior for clients that do not yet send the preference cookie.
func authCookiesShouldPersist(r *http.Request) bool {
	cookie, err := r.Cookie(rememberMeCookie)
	if err != nil {
		return true
	}
	return cookie.Value == rememberMePersistentValue
}

func setGoogleRememberMeCookie(w http.ResponseWriter, persistent bool, expires time.Time) {
	cookie := &http.Cookie{
		Name:     googleOAuthRememberMeCookie,
		Value:    rememberMeValue(persistent),
		HttpOnly: true,
		Secure:   authCookieSecure(),
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	}
	if persistent {
		cookie.Expires = expires
	}
	http.SetCookie(w, cookie)
}

func googleAuthCookiesShouldPersist(r *http.Request) bool {
	cookie, err := r.Cookie(googleOAuthRememberMeCookie)
	if err != nil {
		return true
	}
	return cookie.Value == rememberMePersistentValue
}

func clearGoogleRememberMeCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     googleOAuthRememberMeCookie,
		Value:    "",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   authCookieSecure(),
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})
}

func clearAuthCookies(w http.ResponseWriter) {
	expired := time.Unix(1, 0).UTC()
	for _, name := range []string{"access_token", "refresh_token", rememberMeCookie} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Expires:  expired,
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   authCookieSecure(),
			SameSite: http.SameSiteStrictMode,
			Path:     "/",
		})
	}
}

func setGoogleOAuthStateCookie(w http.ResponseWriter, state string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     googleOAuthStateCookie,
		Value:    state,
		Expires:  expires,
		HttpOnly: true,
		Secure:   authCookieSecure(),
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})
}

func clearGoogleOAuthStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     googleOAuthStateCookie,
		Value:    "",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   authCookieSecure(),
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})
}

func setGoogleOAuthReturnURLCookie(w http.ResponseWriter, returnURL string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     googleOAuthReturnURLCookie,
		Value:    returnURL,
		Expires:  expires,
		HttpOnly: true,
		Secure:   authCookieSecure(),
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})
}

func clearGoogleOAuthReturnURLCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     googleOAuthReturnURLCookie,
		Value:    "",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   authCookieSecure(),
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})
}

func authCookieSecure() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("IDP_COOKIE_SECURE")))
	switch value {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return true
	}
}
