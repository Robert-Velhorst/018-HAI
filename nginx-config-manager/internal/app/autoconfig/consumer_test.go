package autoconfig

import (
	"testing"
	"time"

	"github.com/IBM/sarama"
)

func TestConsumerConfigResumesFromOldestUncommittedOffset(t *testing.T) {
	cfg := newConsumerConfig()
	if cfg.Consumer.Offsets.Initial != sarama.OffsetOldest {
		t.Fatalf("initial offset = %d, want oldest", cfg.Consumer.Offsets.Initial)
	}
	if !cfg.Consumer.Offsets.AutoCommit.Enable || cfg.Consumer.Offsets.AutoCommit.Interval != time.Second {
		t.Fatalf("auto commit config = enabled %v interval %s", cfg.Consumer.Offsets.AutoCommit.Enable, cfg.Consumer.Offsets.AutoCommit.Interval)
	}
	if cfg.ClientID != "hai-nginx-config-manager" {
		t.Fatalf("client id = %q", cfg.ClientID)
	}
}

func TestNewConsumerRejectsMissingDurabilityInputs(t *testing.T) {
	if _, err := NewConsumer(nil, "automation-events", "group", nil); err == nil {
		t.Fatal("NewConsumer accepted an empty broker list and inbox")
	}
}
