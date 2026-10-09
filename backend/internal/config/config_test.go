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

func TestDatabaseCredentialDefaultsDependOnRunMode(t *testing.T) {
	tests := []struct {
		name             string
		mode             string
		wantUser, wantPW string
	}{
		{name: "development defaults fail safe", mode: "development"},
		{name: "local defaults fail safe", mode: "local"},
		{name: "production has no credential defaults", mode: "production"},
		{name: "production mode is case insensitive", mode: " Production "},
		{name: "demo keeps local defaults", mode: "demo", wantUser: "postgres", wantPW: "postgres"},
		{name: "test keeps local defaults", mode: "test", wantUser: "postgres", wantPW: "postgres"},
		{name: "unknown mode fails safe", mode: "unexpected"},
		{name: "empty mode fails safe", mode: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotUser, gotPW := databaseCredentialDefaults(tt.mode)
			if gotUser != tt.wantUser || gotPW != tt.wantPW {
				t.Fatalf("databaseCredentialDefaults(%q) = (%q, %q), want (%q, %q)",
					tt.mode, gotUser, gotPW, tt.wantUser, tt.wantPW)
			}
		})
	}
}
