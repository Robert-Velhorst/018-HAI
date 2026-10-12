package infra

import (
	"automation-hub-idp/internal/app/config"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/IBM/sarama/mocks"
)

func TestNewKafkaProducerRejectsMissingBrokerConfiguration(t *testing.T) {
	if _, err := NewKafkaProducer([]string{" ", ""}, "hai-idp"); err == nil {
		t.Fatal("NewKafkaProducer() should reject an empty broker list")
	}
}

func TestNewKafkaProducerRejectsMissingClientID(t *testing.T) {
	if _, err := NewKafkaProducer([]string{"localhost:9092"}, " "); err == nil {
		t.Fatal("NewKafkaProducer() should reject an empty client ID")
	}
}

func TestNewKafkaProducerConfiguresDurableIdempotentSyncProducer(t *testing.T) {
	mockProducer := mocks.NewSyncProducer(t, mocks.NewTestConfig())
	var gotBrokers []string
	var gotConfig *sarama.Config

	producer, err := newKafkaProducer(
		[]string{" kafka:9092 ", "", "kafka:9092", "backup:9092"},
		" hai-idp ",
		func(brokers []string, producerConfig *sarama.Config) (sarama.SyncProducer, error) {
			gotBrokers = append([]string(nil), brokers...)
			gotConfig = producerConfig
			return mockProducer, nil
		},
	)
	if err != nil {
		t.Fatalf("newKafkaProducer() error = %v", err)
	}
	if producer != mockProducer {
		t.Fatal("newKafkaProducer() did not return the producer created by the factory")
	}
	if want := []string{"kafka:9092", "backup:9092"}; !reflect.DeepEqual(gotBrokers, want) {
		t.Fatalf("brokers = %#v, want %#v", gotBrokers, want)
	}
	if gotConfig == nil {
		t.Fatal("producer config was not supplied to the factory")
	}
	if gotConfig.ClientID != "hai-idp" {
		t.Fatalf("ClientID = %q, want hai-idp", gotConfig.ClientID)
	}
	if gotConfig.Producer.RequiredAcks != sarama.WaitForAll {
		t.Fatalf("RequiredAcks = %v, want WaitForAll", gotConfig.Producer.RequiredAcks)
	}
	if !gotConfig.Producer.Return.Successes || !gotConfig.Producer.Return.Errors {
		t.Fatal("sync producer result channels must both be enabled")
	}
	if !gotConfig.Producer.Idempotent {
		t.Fatal("producer must be idempotent so retries do not duplicate messages")
	}
	if gotConfig.Net.MaxOpenRequests != 1 {
		t.Fatalf("Net.MaxOpenRequests = %d, want 1 for idempotent production", gotConfig.Net.MaxOpenRequests)
	}
	if gotConfig.ChannelBufferSize != 16 || gotConfig.Producer.MaxMessageBytes != 128*1024 {
		t.Fatal("identity producer buffer/message limits changed")
	}
	if gotConfig.Net.DialTimeout != 3*time.Second || gotConfig.Net.ReadTimeout != 10*time.Second || gotConfig.Net.WriteTimeout != 10*time.Second {
		t.Fatal("identity producer network budgets changed")
	}
	if gotConfig.Metadata.Timeout != 10*time.Second || gotConfig.Metadata.Retry.Max != 1 || gotConfig.Metadata.Retry.Backoff != 100*time.Millisecond {
		t.Fatal("identity producer metadata budgets changed")
	}
	if gotConfig.Producer.Timeout != 5*time.Second || gotConfig.Producer.Retry.Max != 1 || gotConfig.Producer.Retry.Backoff != 100*time.Millisecond {
		t.Fatal("identity producer acknowledgement/retry budgets changed")
	}
	if err := gotConfig.Validate(); err != nil {
		t.Fatalf("producer config should pass Sarama validation: %v", err)
	}
	if err := producer.Close(); err != nil {
		t.Fatalf("close producer: %v", err)
	}
}

func TestNewKafkaProducerRejectsInvalidClientIDBeforeConnecting(t *testing.T) {
	factoryCalled := false
	_, err := newKafkaProducer(
		[]string{"kafka:9092"},
		"invalid client id",
		func([]string, *sarama.Config) (sarama.SyncProducer, error) {
			factoryCalled = true
			return nil, nil
		},
	)
	if err == nil {
		t.Fatal("newKafkaProducer() should reject an invalid Kafka client ID")
	}
	if factoryCalled {
		t.Fatal("producer factory should not be called for an invalid configuration")
	}
}

func TestNewKafkaProducerWrapsFactoryFailure(t *testing.T) {
	expected := errors.New("dial failed")
	_, err := newKafkaProducer(
		[]string{"kafka:9092"},
		"hai-idp",
		func([]string, *sarama.Config) (sarama.SyncProducer, error) {
			return nil, expected
		},
	)
	if !errors.Is(err, expected) {
		t.Fatalf("newKafkaProducer() error = %v, want wrapped %v", err, expected)
	}
}

func TestNewKafkaProducerRejectsNilFactoryResult(t *testing.T) {
	_, err := newKafkaProducer(
		[]string{"kafka:9092"},
		"hai-idp",
		func([]string, *sarama.Config) (sarama.SyncProducer, error) {
			return nil, nil
		},
	)
	if err == nil {
		t.Fatal("newKafkaProducer() should reject a nil producer")
	}
}

func TestGetDefaultKafkaProducerRejectsUninitializedConfig(t *testing.T) {
	previous := config.KafkaConfig
	config.KafkaConfig = nil
	t.Cleanup(func() {
		config.KafkaConfig = previous
	})

	_, err := GetDefaultKafkaProducer()
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("GetDefaultKafkaProducer() error = %v, want uninitialized configuration error", err)
	}
}
