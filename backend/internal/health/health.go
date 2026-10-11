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
	"os"
	"strings"
	"time"

	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/doctor"
	"automation-hub-backend/internal/events"
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
//   - Kafka is not: committed events remain queued in the transactional outbox
//     until a configured broker becomes available.
//   - Redis is not: the backend does not connect to it yet (see RedisProbe).
//   - An LLM provider is not: generation is one capability, not the service.
func Probes(cfg config.Configuration) []doctor.Probe {
	return []doctor.Probe{
		PostgresProbe(cfg),
		RedisProbe(cfg),
		KafkaProbe(cfg),
		EventOutboxProbe(),
		LLMProviderProbe(),
	}
}

type postgresPinger interface {
	PingContext(context.Context) error
}

// PostgresProbe pings the authenticated pool used by domain routes. A plain TCP
// dial would prove only that something is listening on the port, while opening
// a separate pool on every readiness request creates avoidable connection churn
// and can disagree with the pool that is actually serving requests.
func PostgresProbe(cfg config.Configuration) doctor.Probe {
	return postgresProbe(cfg, func() (postgresPinger, error) {
		gormDB, err := infra.GetDefaultDB()
		if err != nil {
			return nil, err
		}
		return gormDB.DB()
	})
}

func postgresProbe(cfg config.Configuration, acquire func() (postgresPinger, error)) doctor.Probe {
	return doctor.Probe{
		Name:     "database.connection",
		Critical: true,
		Run: func(ctx context.Context) error {
			pinger, err := acquire()
			if err != nil {
				return fmt.Errorf("connect %s:%d/%s as %s: %w", cfg.DbHost, cfg.DbPort, cfg.DbName, cfg.DbUser, err)
			}
			if err := pinger.PingContext(ctx); err != nil {
				return fmt.Errorf("ping %s:%d/%s: %w", cfg.DbHost, cfg.DbPort, cfg.DbName, err)
			}
			return nil
		},
	}
}

// RedisProbe checks the Redis declared in the compose stack.
//
// Redis backs the shared rate limiter when configured. It remains a
// non-critical readiness dependency because router startup and runtime
// deliberately fall back to a bounded in-process limiter when Redis is
// unavailable.
func RedisProbe(cfg config.Configuration) doctor.Probe {
	addr := strings.TrimSpace(cfg.RedisAddr)
	return doctor.Probe{
		Name:     "redis.connection",
		Critical: false,
		Run: func(ctx context.Context) error {
			if addr == "" {
				return fmt.Errorf("REDIS_ADDR is not set; rate-limit state stays in-process and resets on restart")
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
			// Inline RESP keeps the readiness probe independent from the
			// rate-limiter client's lifecycle.
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
// degradation rather than an API outage: committed automation events remain in
// the Postgres outbox and are retried after the broker recovers.
func KafkaProbe(cfg config.Configuration) doctor.Probe {
	brokers := nonEmpty(cfg.Brokers)
	return doctor.Probe{
		Name:     "kafka.connection",
		Critical: false,
		Run: func(ctx context.Context) error {
			if len(brokers) == 0 {
				return fmt.Errorf("KAFKA_BROKERS is empty; committed events remain queued until a broker is configured")
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

// EventOutboxProbe exposes delivery failures separately from broker
// connectivity. A brief pending row is normal; dead letters or a queue that has
// made no progress for two minutes require operator attention.
func EventOutboxProbe() doctor.Probe {
	return doctor.Probe{
		Name:     "events.outbox",
		Critical: false,
		Run: func(ctx context.Context) error {
			db, err := infra.GetDefaultDB()
			if err != nil {
				return err
			}
			stats, err := events.NewOutboxStore(db).Stats(ctx)
			if err != nil {
				return err
			}
			if stats.DeadLettered > 0 {
				return fmt.Errorf("%d event deliveries are dead-lettered", stats.DeadLettered)
			}
			if stats.Pending > 0 && stats.OldestPendingAt != nil && time.Since(*stats.OldestPendingAt) > 2*time.Minute {
				return fmt.Errorf("%d event deliveries are pending; oldest is %s old", stats.Pending, time.Since(*stats.OldestPendingAt).Round(time.Second))
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
			endpoint := strings.TrimSpace(os.Getenv("OLLAMA_BASE_URL"))
			label := "OLLAMA_BASE_URL"
			if endpoint == "" {
				endpoint = strings.TrimSpace(os.Getenv("LLAMA_CPP_BASE_URL"))
				label = "LLAMA_CPP_BASE_URL"
			}
			if endpoint == "" {
				endpoint = strings.TrimSpace(os.Getenv("LOCALAI_BASE_URL"))
				label = "LOCALAI_BASE_URL"
			}
			if endpoint == "" {
				endpoint = strings.TrimSpace(os.Getenv("VLLM_BASE_URL"))
				label = "VLLM_BASE_URL"
			}
			if endpoint == "" {
				endpoint = strings.TrimSpace(os.Getenv("MISTRAL_RS_BASE_URL"))
				label = "MISTRAL_RS_BASE_URL"
			}
			if endpoint == "" && strings.EqualFold(strings.TrimSpace(os.Getenv("LITELLM_ENABLED")), "true") {
				endpoint = strings.TrimSpace(os.Getenv("LITELLM_BASE_URL"))
				label = "LITELLM_BASE_URL"
			}
			if endpoint == "" {
				endpoint = strings.TrimSpace(os.Getenv("FREE_CLOUD_OPENAI_BASE_URL"))
				label = "FREE_CLOUD_OPENAI_BASE_URL"
			}
			if endpoint == "" {
				return fmt.Errorf("no provider configured (OLLAMA_BASE_URL / LLAMA_CPP_BASE_URL / LOCALAI_BASE_URL / VLLM_BASE_URL / MISTRAL_RS_BASE_URL / enabled LITELLM_BASE_URL / FREE_CLOUD_OPENAI_BASE_URL unset); generation is unavailable")
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

func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
