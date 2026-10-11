package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestConfigurationValidateRequiresDurableConsumerInputs(t *testing.T) {
	valid := Configuration{
		Brokers: []string{"kafka:9092"}, Topic: "automation-events",
		ConsumerGroup: "hai-nginx-config-manager-v1", InboxDir: t.TempDir(),
		MaxAttempts: 5, Retention: 30 * 24 * time.Hour,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid configuration rejected: %v", err)
	}
	valid.Brokers = nil
	if err := valid.Validate(); err == nil {
		t.Fatal("configuration without Kafka brokers was accepted")
	}
}

func TestInitBuildsPersistentInboxBelowConfigDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(configDir, dir)
	t.Setenv(kafkaBrokers, " kafka:9092, ,backup:9092 ")
	t.Setenv(maxAttempts, "7")
	t.Setenv(retention, "48")
	Init()

	if len(AppConfig.Brokers) != 2 || AppConfig.Brokers[0] != "kafka:9092" || AppConfig.Brokers[1] != "backup:9092" {
		t.Fatalf("brokers = %#v", AppConfig.Brokers)
	}
	if AppConfig.InboxDir != filepath.Join(dir, ".hai-event-inbox") {
		t.Fatalf("inbox dir = %q", AppConfig.InboxDir)
	}
	if AppConfig.MaxAttempts != 7 || AppConfig.Retention != 48*time.Hour {
		t.Fatalf("retry settings = %d, %s", AppConfig.MaxAttempts, AppConfig.Retention)
	}
}
