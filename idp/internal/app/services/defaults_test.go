package services

import (
	"automation-hub-idp/internal/app/config"
	"testing"
)

func TestDefaultLoggerUsesLocalLoggerWithoutKafka(t *testing.T) {
	previous := config.KafkaConfig
	config.KafkaConfig = nil
	t.Cleanup(func() { config.KafkaConfig = previous })

	logger, err := DefaultLogger()
	if err != nil {
		t.Fatalf("DefaultLogger() error = %v", err)
	}
	if _, ok := logger.(*localLogger); !ok {
		t.Fatalf("DefaultLogger() = %T, want *localLogger", logger)
	}
}

func TestDefaultMessageSenderDropsEventsWithoutKafka(t *testing.T) {
	previous := config.KafkaConfig
	config.KafkaConfig = nil
	t.Cleanup(func() { config.KafkaConfig = previous })

	sender, err := NewDefaultMessageSender()
	if err != nil {
		t.Fatalf("NewDefaultMessageSender() error = %v", err)
	}
	if err := sender.Send("account-events", map[string]string{"email": "operator@example.com"}); err != nil {
		t.Fatalf("local event sender Send() error = %v", err)
	}
}
