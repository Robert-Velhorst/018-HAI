package config

import (
	"reflect"
	"testing"
)

func TestInitDisablesEventBusWithoutDialTargets(t *testing.T) {
	t.Setenv(eventBusEnabled, "false")
	t.Setenv(kafkaBrokers, "kafka:9092")
	t.Setenv(kafkaTopic, "automation-events")
	t.Setenv(imageSaveDir, t.TempDir())

	Init()

	if len(AppConfig.Brokers) != 0 {
		t.Fatalf("Brokers = %v, want no broker targets when event bus is disabled", AppConfig.Brokers)
	}
	if AppConfig.Topic != "" {
		t.Fatalf("Topic = %q, want empty when event bus is disabled", AppConfig.Topic)
	}
}

func TestInitKeepsConfiguredEventBusTargets(t *testing.T) {
	t.Setenv(eventBusEnabled, "true")
	t.Setenv(kafkaBrokers, " kafka:9092, backup:9092 ")
	t.Setenv(kafkaTopic, "automation-events")
	t.Setenv(imageSaveDir, t.TempDir())

	Init()

	if !reflect.DeepEqual(AppConfig.Brokers, []string{"kafka:9092", "backup:9092"}) {
		t.Fatalf("Brokers = %v, want configured broker targets", AppConfig.Brokers)
	}
	if AppConfig.Topic != "automation-events" {
		t.Fatalf("Topic = %q, want configured event topic", AppConfig.Topic)
	}
}
