package router

import (
	"automation-hub-idp/internal/app/config"
	"automation-hub-idp/internal/app/services"
	"automation-hub-idp/internal/infra"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	trustedGatewayHostname = "gateway"
	gatewayIPCacheTTL      = 5 * time.Second
	gatewayLookupTimeout   = 500 * time.Millisecond
)

type gatewayIPLookup func(context.Context) ([]net.IP, error)

func Initialize() error {
	return InitializeContext(context.Background())
}

func InitializeContext(ctx context.Context) error {
	router := gin.New()
	router.Use(gin.LoggerWithFormatter(formatAccessLog), privateRecovery())
	if err := router.SetTrustedProxies(nil); err != nil {
		return err
	}
	router.Use(newTrustedGatewayClientIPMiddleware(func(ctx context.Context) ([]net.IP, error) {
		return net.DefaultResolver.LookupIP(ctx, "ip", trustedGatewayHostname)
	}))

	database, err := infra.GetDefaultDBContext(ctx)
	if err != nil {
		return err
	}
	pool, err := database.DB()
	if err != nil {
		return err
	}
	resources := &services.OwnedResources{}
	return runWithOwnedDatabase(func() error { return errors.Join(resources.Close(), pool.Close()) }, func() error {
		if err := initializeRoutes(router, database, resources); err != nil {
			return err
		}
		port := ":" + config.ServerConfig.Port
		return serveIdentity(ctx, newHTTPServer(port, router), 90*time.Second)
	})
}

func runWithOwnedDatabase(closePool func() error, run func() error) (err error) {
	defer func() {
		if !errors.Is(err, errIdentityDrainIncomplete) {
			err = errors.Join(err, closePool())
		}
	}()
	return run()
}

func privateRecovery() gin.HandlerFunc {
	// A nil writer avoids Gin's header dump, panic payload and source-file stack reads.
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, _ any) {
		log.Print("HAI identity request panic recovered")
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	})
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		// Allow the bounded OAuth and SMTP operations to finish before writing.
		WriteTimeout:   90 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 32 * 1024,
	}
}

func newTrustedGatewayClientIPMiddleware(lookup gatewayIPLookup) gin.HandlerFunc {
	// Trust only the exact addresses resolved for Docker's gateway service. If
	// lookup fails, leave RemoteAddr untouched and ignore forwarded headers.
	var (
		mu         sync.Mutex
		gatewayIPs = map[string]struct{}{}
		refreshAt  time.Time
	)

	return func(c *gin.Context) {
		peer, _, err := net.SplitHostPort(c.Request.RemoteAddr)
		peerIP := net.ParseIP(peer)
		if err != nil || peerIP == nil {
			c.Next()
			return
		}

		mu.Lock()
		if !time.Now().Before(refreshAt) {
			ctx, cancel := context.WithTimeout(context.Background(), gatewayLookupTimeout)
			resolved, lookupErr := lookup(ctx)
			cancel()

			gatewayIPs = make(map[string]struct{}, len(resolved))
			if lookupErr == nil {
				for _, ip := range resolved {
					if canonical := canonicalUnicastIP(ip); canonical != "" {
						gatewayIPs[canonical] = struct{}{}
					}
				}
			}
			refreshAt = time.Now().Add(gatewayIPCacheTTL)
		}
		_, trustedPeer := gatewayIPs[peerIP.String()]
		mu.Unlock()

		if trustedPeer {
			// Nginx overwrites this header with its direct client address. Accept
			// exactly one literal unicast IP; ambiguous/missing input has no
			// attributable client identity and must not fall back to the gateway.
			forwardedValues := c.Request.Header.Values("X-Forwarded-For")
			if len(forwardedValues) != 1 || strings.Contains(forwardedValues[0], ",") {
				c.Request.RemoteAddr = ""
			} else if clientIP := net.ParseIP(strings.TrimSpace(forwardedValues[0])); canonicalUnicastIP(clientIP) == "" {
				c.Request.RemoteAddr = ""
			} else {
				c.Request.RemoteAddr = net.JoinHostPort(canonicalUnicastIP(clientIP), "0")
			}
		}

		c.Next()
	}
}

func canonicalUnicastIP(ip net.IP) string {
	if ip == nil {
		return ""
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		ip = ipv4
	} else if ipv6 := ip.To16(); ipv6 != nil {
		ip = ipv6
	} else {
		return ""
	}
	if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return ""
	}
	return ip.String()
}

func formatAccessLog(params gin.LogFormatterParams) string {
	path := accessLogPath(params.Path)
	errorMessage := params.ErrorMessage
	if errorMessage != "" || containsPasswordResetTokenPath(params.Path) {
		errorMessage = "[redacted]"
	}
	return fmt.Sprintf("[GIN] %v |%3d| %13v | %15s |%-7s %#v\n%s",
		params.TimeStamp.Format("2006/01/02 - 15:04:05"),
		params.StatusCode,
		params.Latency,
		params.ClientIP,
		params.Method,
		path,
		errorMessage,
	)
}

func accessLogPath(requestPath string) string {
	path := strings.SplitN(requestPath, "?", 2)[0]
	const marker = "/auth/confirm-password-reset/"
	lowerPath := strings.ToLower(path)
	if index := strings.Index(lowerPath, marker); index >= 0 {
		return path[:index] + marker + ":reset-token"
	}
	return path
}

func containsPasswordResetTokenPath(requestPath string) bool {
	const marker = "/auth/confirm-password-reset/"
	return strings.Contains(strings.ToLower(requestPath), marker)
}
