package services

import (
	"automation-hub-idp/internal/app/config"
	"automation-hub-idp/internal/app/services/iservice"
	"automation-hub-idp/internal/infra"
	"fmt"
	"github.com/IBM/sarama"
	"log"
	"strings"
	"sync"
)

type KafkaLogger struct {
	producer sarama.SyncProducer
	topic    string
	mu       sync.RWMutex
	closed   bool
	closeErr error
}

func NewKafkaLogger(brokers []string, topic string) (iservice.Logger, error) {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return nil, fmt.Errorf("create Kafka logger: topic is required")
	}
	clientID := "hai-idp-logger"
	if config.KafkaConfig != nil && strings.TrimSpace(config.KafkaConfig.ClientID) != "" {
		clientID = strings.TrimSpace(config.KafkaConfig.ClientID) + "-logger"
	}
	producer, err := infra.NewKafkaProducer(brokers, clientID)
	if err != nil {
		return nil, fmt.Errorf("create Kafka logger: %w", err)
	}

	return newKafkaLogger(producer, topic)
}

func newKafkaLogger(producer sarama.SyncProducer, topic string) (*KafkaLogger, error) {
	if producer == nil {
		return nil, fmt.Errorf("create Kafka logger: producer is not configured")
	}
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return nil, fmt.Errorf("create Kafka logger: topic is required")
	}
	return &KafkaLogger{
		producer: producer,
		topic:    topic,
	}, nil
}

func (k *KafkaLogger) sendMessage(level, message string, args ...interface{}) {
	if k == nil {
		log.Printf("Failed to send %s message to Kafka: producer is not configured", level)
		return
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	if k.producer == nil || k.closed {
		log.Printf("Failed to send %s message to Kafka: producer is unavailable", level)
		return
	}
	formattedMessage := fmt.Sprintf("[%s] %s", level, fmt.Sprintf(message, args...))
	msg := &sarama.ProducerMessage{
		Topic: k.topic,
		Value: sarama.StringEncoder(formattedMessage),
	}

	_, _, err := k.producer.SendMessage(msg)
	if err != nil {
		log.Printf("Failed to send %s message to Kafka: transport error; raw details withheld", level)
	}
}

func (k *KafkaLogger) Close() error {
	if k == nil {
		return nil
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.producer == nil || k.closed {
		return k.closeErr
	}
	k.closed = true
	if err := k.producer.Close(); err != nil {
		k.closeErr = fmt.Errorf("close Kafka logger: %w", err)
	}
	return k.closeErr
}

func (k *KafkaLogger) Info(message string, args ...interface{}) {
	k.sendMessage("INFO", message, args...)
}

func (k *KafkaLogger) Error(message string, args ...interface{}) {
	k.sendMessage("ERROR", message, args...)
}

func (k *KafkaLogger) Warn(message string, args ...interface{}) {
	k.sendMessage("WARN", message, args...)
}

func (k *KafkaLogger) Debug(message string, args ...interface{}) {
	k.sendMessage("DEBUG", message, args...)
}
