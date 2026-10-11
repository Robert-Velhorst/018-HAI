// Package health performs live connectivity probes against the dependencies the
// backend actually runs on.
//
// This exists because doctor.Diagnose deliberately performs no I/O: it inspects
// configuration values only. That makes it fast and deterministic, but it also
// means it will happily report "ready" for a process whose database, cache and
// event bus are all down — every DB_* variable is still a non-empty string. A
// readiness answer that cannot fail for the reasons readiness exists to catch is
// not a readiness answer.
//
// The probes below open real connections, authenticate where the dependency
// supports it, and report what they actually found.
package health

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/doctor"
	"automation-hub-backend/internal/infra"

	"github.com/IBM/sarama"
)

// DefaultTimeout bounds each individual probe. Readiness is polled by
// orchestrators on a short interval, so a probe must fail fast rather than
// queue up behind an unreachable host.
const DefaultTimeout = 3 * time.Second

// Probes returns the live dependency probes for a configuration.
//
// Criticality reflects what the process can actually serve without:
//   - Postgres is critical: every domain route reads or writes it.
//   - Kafka is not: the publisher degrades to a no-op when brokers are absent.
//   - Redis is not: the backend does not connect to it yet (see RedisProbe).
//   - An LLM provider is not: generation is one capability, not the service.
func Probes(cfg config.Configuration) []doctor.Probe {
	return []doctor.Probe{
		PostgresProbe(cfg),
		RedisProbe(cfg),
		KafkaProbe(cfg),
		LLMProviderProbe(),
	}
}

// PostgresProbe opens a real, authenticated connection and pings it. A plain TCP
// dial would prove only that something is listening on the port; it would still
// pass with wrong credentials or a missing database, which are exactly the
// failures an operator needs readiness to surface.
func PostgresProbe(cfg config.Configuration) doctor.Probe {
	return doctor.Probe{
		Name:     "database.connection",
		Critical: true,
		Run: func(ctx context.Context) error {
			gormDB, err := infra.NewPostgresDatabaseContext(ctx, cfg.DbUser, cfg.DbPassword, cfg.DbName, cfg.DbHost, cfg.DbPort)
			if err != nil {
				return fmt.Errorf("connect %s:%d/%s as %s: %w", cfg.DbHost, cfg.DbPort, cfg.DbName, cfg.DbUser, err)
			}
			sqlDB, err := gormDB.DB()
			if err != nil {
				return fmt.Errorf("acquire pool: %w", err)
			}
			defer sqlDB.Close()
			if err := sqlDB.PingContext(ctx); err != nil {
				return fmt.Errorf("ping %s:%d/%s: %w", cfg.DbHost, cfg.DbPort, cfg.DbName, err)
			}
			return nil
		},
	}
}

// RedisProbe checks the Redis declared in the compose stack.
//
// Redis backs the optional shared rate limiter. When it is absent or
// unavailable, the limiter uses bounded in-process counters. Redis is therefore
// a degradation rather than a hard availability requirement. The probe still
// reports it honestly, because an operator needs to distinguish durable shared
// limits from per-process limits that reset on restart.
func RedisProbe(cfg config.Configuration) doctor.Probe {
	addr := strings.TrimSpace(cfg.RedisAddr)
	return doctor.Probe{
		Name:     "redis.connection",
		Critical: false,
		Run: func(ctx context.Context) error {
			if addr == "" {
				return fmt.Errorf("REDIS_ADDR is not set; quota and rate-limit state stays in-process and resets on restart")
			}
			var dialer net.Dialer
			conn, err := dialer.DialContext(ctx, "tcp", addr)
			if err != nil {
				return fmt.Errorf("dial %s: %w", addr, err)
			}
			defer conn.Close()

			if deadline, ok := ctx.Deadline(); ok {
				_ = conn.SetDeadline(deadline)
			}
			// Inline RESP so a liveness check does not pull in a Redis client
			// the backend has no other use for yet.
			if _, err := conn.Write([]byte("*1\r\n$4\r\nPING\r\n")); err != nil {
				return fmt.Errorf("write PING to %s: %w", addr, err)
			}
			reply, err := bufio.NewReader(conn).ReadString('\n')
			if err != nil {
				return fmt.Errorf("read PING reply from %s: %w", addr, err)
			}
			if !strings.HasPrefix(reply, "+PONG") {
				return fmt.Errorf("unexpected PING reply from %s: %q", addr, strings.TrimSpace(reply))
			}
			return nil
		},
	}
}

// KafkaProbe connects to the configured brokers. Kafka being down is a
// degradation rather than an outage: the event publisher falls back to a no-op,
// so this is reported as a warning and does not make the service unready.
func KafkaProbe(cfg config.Configuration) doctor.Probe {
	brokers := nonEmpty(cfg.Brokers)
	return doctor.Probe{
		Name:     "kafka.connection",
		Critical: false,
		Run: func(ctx context.Context) error {
			if len(brokers) == 0 {
				return fmt.Errorf("KAFKA_BROKERS is empty; event publishing is disabled and events are dropped")
			}
			saramaCfg := sarama.NewConfig()
			saramaCfg.Net.DialTimeout = DefaultTimeout
			saramaCfg.Net.ReadTimeout = DefaultTimeout
			saramaCfg.Net.WriteTimeout = DefaultTimeout
			saramaCfg.Metadata.Retry.Max = 0

			client, err := sarama.NewClient(brokers, saramaCfg)
			if err != nil {
				return fmt.Errorf("connect brokers %s: %w", strings.Join(brokers, ","), err)
			}
			defer client.Close()

			if len(client.Brokers()) == 0 {
				return fmt.Errorf("no reachable brokers among %s", strings.Join(brokers, ","))
			}
			return nil
		},
	}
}

// LLMProviderProbe reports whether a generation provider is actually reachable.
// With no provider configured the backend cannot generate at all, which is worth
// stating plainly rather than discovering at the first prompt.
func LLMProviderProbe() doctor.Probe {
	return doctor.Probe{
		Name:     "llm.provider",
		Critical: false,
		Run: func(ctx context.Context) error {
			endpoint, label := configuredLLMProviderEndpoint()
			if endpoint == "" {
				return fmt.Errorf("no provider configured (OLLAMA_BASE_URL / LLAMA_CPP_BASE_URL / LM_STUDIO_BASE_URL / LOCALAI_BASE_URL / VLLM_BASE_URL / SGLANG_BASE_URL / DSPARK_BASE_URL / MISTRAL_RS_BASE_URL / enabled LITELLM_BASE_URL / FREE_CLOUD_OPENAI_BASE_URL unset); generation is unavailable")
			}

			request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/"), nil)
			if err != nil {
				return fmt.Errorf("build request for %s (%s): %w", endpoint, label, err)
			}
			response, err := (&http.Client{Timeout: DefaultTimeout}).Do(request)
			if err != nil {
				return fmt.Errorf("reach %s (%s): %w", endpoint, label, err)
			}
			defer response.Body.Close()

			// Any HTTP response proves the provider is listening; a 404 on the
			// root path is normal for an inference server.
			if response.StatusCode >= 500 {
				return fmt.Errorf("provider %s returned HTTP %d", endpoint, response.StatusCode)
			}
			return nil
		},
	}
}

// configuredLLMProviderEndpoint must track the local and free-provider
// endpoints accepted by llm.DefaultPolicyFromEnv. Readiness should describe the
// usable routing surface, not a smaller, stale subset of it.
func configuredLLMProviderEndpoint() (endpoint, label string) {
	for _, candidate := range []struct {
		env     string
		enabled bool
	}{
		{env: "OLLAMA_BASE_URL", enabled: true},
		{env: "LLAMA_CPP_BASE_URL", enabled: true},
		{env: "LM_STUDIO_BASE_URL", enabled: true},
		{env: "LOCALAI_BASE_URL", enabled: true},
		{env: "VLLM_BASE_URL", enabled: true},
		{env: "SGLANG_BASE_URL", enabled: true},
		{env: "DSPARK_BASE_URL", enabled: dsparkProviderEnabled()},
		{env: "MISTRAL_RS_BASE_URL", enabled: true},
		{env: "LITELLM_BASE_URL", enabled: strings.EqualFold(strings.TrimSpace(os.Getenv("LITELLM_ENABLED")), "true")},
		{env: "FREE_CLOUD_OPENAI_BASE_URL", enabled: true},
	} {
		if candidate.enabled {
			if value := strings.TrimSpace(os.Getenv(candidate.env)); value != "" {
				return value, candidate.env
			}
		}
	}
	return "", ""
}

// dsparkProviderEnabled mirrors the router's local-only DSpark policy. An
// endpoint string alone is not enough: treating a disabled or non-local DSpark
// endpoint as ready would make the health report contradict the route policy.
func dsparkProviderEnabled() bool {
	endpoint := strings.TrimSpace(os.Getenv("DSPARK_BASE_URL"))
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Hostname() == "" || !envEnabled("DSPARK_ENABLED") {
		return false
	}
	return isLocalModelHost(parsed.Hostname())
}

func envEnabled(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return value == "1" || value == "true" || value == "yes"
}

func isLocalModelHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "localhost" || host == "host.docker.internal" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
