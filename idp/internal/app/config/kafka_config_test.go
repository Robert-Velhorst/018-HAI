package config

import (
	"strings"
	"testing"
)

func TestKafkaConfigTrimsRequiredValues(t *testing.T) {
	t.Setenv(eventBusEnabled, "true")
	t.Setenv(loggerTopic, " idp-logs ")
	t.Setenv(mailTopic, " idp-mail ")
	t.Setenv(brokersAddr, " kafka:9092, ,backup:9092 ")
	t.Setenv(clientID, " hai-idp ")

	cfg, err := newKafkaConfig()
	if err != nil {
		t.Fatalf("newKafkaConfig() error = %v", err)
	}
	if cfg.LoggerTopic != "idp-logs" || cfg.MailTopic != "idp-mail" || cfg.ClientID != "hai-idp" {
		t.Fatalf("unexpected normalized config: %#v", cfg)
	}
	if got := strings.Join(cfg.BrokersAddr, ","); got != "kafka:9092,backup:9092" {
		t.Fatalf("brokers = %q", got)
	}
}

func TestKafkaConfigAllowsDisabledEventBusWithoutKafkaSettings(t *testing.T) {
	t.Setenv(eventBusEnabled, "false")
	t.Setenv(loggerTopic, "")
	t.Setenv(mailTopic, "")
	t.Setenv(brokersAddr, "")
	t.Setenv(clientID, "")

	cfg, err := newKafkaConfig()
	if err != nil {
		t.Fatalf("newKafkaConfig() error = %v", err)
	}
	if cfg.Enabled {
		t.Fatal("disabled event bus must remain disabled")
	}
	if len(cfg.BrokersAddr) != 0 || cfg.LoggerTopic != "" || cfg.MailTopic != "" {
		t.Fatalf("disabled config should not retain Kafka settings: %#v", cfg)
	}
}

func TestKafkaConfigRejectsMissingRequiredValues(t *testing.T) {
	for _, test := range []struct {
		name string
		key  string
		want string
	}{
		{name: "logger topic", key: loggerTopic, want: loggerTopic},
		{name: "mail topic", key: mailTopic, want: mailTopic},
		{name: "brokers", key: brokersAddr, want: brokersAddr},
		{name: "client id", key: clientID, want: clientID},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(eventBusEnabled, "true")
			t.Setenv(loggerTopic, "logs")
			t.Setenv(mailTopic, "mail")
			t.Setenv(brokersAddr, "kafka:9092")
			t.Setenv(clientID, "hai-idp")
			t.Setenv(test.key, " ")

			_, err := newKafkaConfig()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("newKafkaConfig() error = %v, want context for %s", err, test.want)
			}
		})
	}
}
