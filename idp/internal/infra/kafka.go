package infra

import (
	"automation-hub-idp/internal/app/config"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/IBM/sarama"
)

var validKafkaClientID = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func NewKafkaProducer(brokers []string, client string) (sarama.SyncProducer, error) {
	return newKafkaProducer(brokers, client, sarama.NewSyncProducer)
}

type kafkaProducerFactory func([]string, *sarama.Config) (sarama.SyncProducer, error)

func newKafkaProducer(brokers []string, client string, factory kafkaProducerFactory) (sarama.SyncProducer, error) {
	if factory == nil {
		return nil, fmt.Errorf("Kafka producer factory is required")
	}

	cleanBrokers := make([]string, 0, len(brokers))
	seenBrokers := make(map[string]struct{}, len(brokers))
	for _, broker := range brokers {
		broker = strings.TrimSpace(broker)
		if broker == "" {
			continue
		}
		if _, duplicate := seenBrokers[broker]; duplicate {
			continue
		}
		seenBrokers[broker] = struct{}{}
		cleanBrokers = append(cleanBrokers, broker)
	}
	if len(cleanBrokers) == 0 {
		return nil, fmt.Errorf("at least one Kafka broker is required")
	}
	client = strings.TrimSpace(client)
	if client == "" {
		return nil, fmt.Errorf("Kafka client ID is required")
	}
	if !validKafkaClientID.MatchString(client) {
		return nil, fmt.Errorf("Kafka client ID contains invalid characters; use only letters, numbers, dot, underscore, or hyphen")
	}

	producerConfig := sarama.NewConfig()
	producerConfig.ClientID = client
	producerConfig.Net.MaxOpenRequests = 1
	producerConfig.Net.DialTimeout = 3 * time.Second
	producerConfig.Net.ReadTimeout = 10 * time.Second
	producerConfig.Net.WriteTimeout = 10 * time.Second
	producerConfig.Metadata.Timeout = 10 * time.Second
	producerConfig.Metadata.Retry.Max = 1
	producerConfig.Metadata.Retry.Backoff = 100 * time.Millisecond
	producerConfig.ChannelBufferSize = 16
	producerConfig.Producer.MaxMessageBytes = 128 * 1024
	producerConfig.Producer.Timeout = 5 * time.Second
	producerConfig.Producer.Retry.Max = 1
	producerConfig.Producer.Retry.Backoff = 100 * time.Millisecond
	producerConfig.Producer.Idempotent = true
	producerConfig.Producer.RequiredAcks = sarama.WaitForAll
	producerConfig.Producer.Return.Successes = true
	producerConfig.Producer.Return.Errors = true
	if err := producerConfig.Validate(); err != nil {
		return nil, fmt.Errorf("invalid Kafka producer configuration: %w", err)
	}

	producer, err := factory(cleanBrokers, producerConfig)
	if err != nil {
		if producer != nil {
			err = errors.Join(err, producer.Close())
		}
		return nil, fmt.Errorf("failed to create Kafka producer: %w", err)
	}
	if producer == nil {
		return nil, fmt.Errorf("failed to create Kafka producer: producer factory returned nil")
	}

	return producer, nil
}

func GetDefaultKafkaProducer() (sarama.SyncProducer, error) {
	if config.KafkaConfig == nil {
		return nil, fmt.Errorf("Kafka configuration is not initialized")
	}
	return NewKafkaProducer(config.KafkaConfig.BrokersAddr, config.KafkaConfig.ClientID)
}
