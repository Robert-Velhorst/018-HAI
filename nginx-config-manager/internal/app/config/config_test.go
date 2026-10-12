package config

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestBrokerListTrimsAndDropsBlankEntries(t *testing.T) {
	t.Setenv(kafkaBrokers, " kafka:9092, ,backup:9092 ")

	brokers := getStringListFromEnv(kafkaBrokers, "")
	if want := []string{"kafka:9092", "backup:9092"}; !reflect.DeepEqual(brokers, want) {
		t.Fatalf("getStringListFromEnv() = %v, want %v", brokers, want)
	}
}

func TestInitBuildsDurableConsumerDefaults(t *testing.T) {
	t.Setenv(configDir, t.TempDir())
	t.Setenv(kafkaBrokers, "broker:9092")
	t.Setenv(kafkaTopic, "automation-events")
	t.Setenv(reloadEnabled, "false")
	t.Setenv(consumerGroup, "hai-config-consumer")
	t.Setenv(inboxDir, "")
	t.Setenv(maxAttempts, "5")
	t.Setenv(inboxRetention, "720h")
	t.Setenv(healthPort, "8080")

	Init()
	if err := AppConfig.Validate(); err != nil {
		t.Fatalf("AppConfig.Validate(): %v", err)
	}
	if AppConfig.ConsumerGroup != "hai-config-consumer" || AppConfig.MaxAttempts != 5 ||
		AppConfig.Retention != 720*time.Hour || AppConfig.HealthPort != "8080" {
		t.Fatalf("unexpected runtime settings: %#v", AppConfig)
	}
	if AppConfig.InboxDir != AppConfig.ConfigDir+string(filepath.Separator)+".hai-event-inbox" {
		t.Fatalf("inbox directory = %q, want persistent child of config directory", AppConfig.InboxDir)
	}
}

func TestValidateFailsClosedForUnsafeOrIncompleteSettings(t *testing.T) {
	valid := Configuration{
		ConfigDir: t.TempDir(), Brokers: []string{"broker:9092"}, Topic: "automation-events",
		ConsumerGroup: "hai-config-consumer", InboxDir: t.TempDir(), MaxAttempts: 5,
		Retention: 24 * time.Hour, HealthPort: "8080",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid configuration rejected: %v", err)
	}

	unsafe := valid
	unsafe.ReloadEnabled = true
	if err := unsafe.Validate(); err == nil {
		t.Fatal("reload without an approved runtime capability was accepted")
	}
	invalidBroker := valid
	invalidBroker.Brokers = []string{"broker:not-a-port"}
	if err := invalidBroker.Validate(); err == nil {
		t.Fatal("invalid broker port was accepted")
	}
}
