package events

import (
	"automation-hub-backend/internal/config"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/IBM/sarama"
)

var (
	ErrInvalidEvent = errors.New("automation event is invalid")
	ErrUnavailable  = errors.New("Kafka event publisher is unavailable")
)

const reconnectCooldown = 2 * time.Second

type publisherState struct {
	mu            sync.Mutex
	producer      sarama.SyncProducer
	brokers       []string
	nextConnectAt time.Time
	lastError     error
	closed        bool
}

// Publisher is cheap to copy: all mutable producer state is shared through
// state, which preserves the existing service constructor API without copying
// a mutex or a live Sarama producer.
type Publisher struct {
	state *publisherState
	topic string
}

func NewPublisher(brokers []string, topic string) (*Publisher, error) {
	producer, err := newSyncProducer(brokers)
	if err != nil {
		return nil, err
	}

	return &Publisher{
		state: &publisherState{
			producer: producer,
			brokers:  append([]string(nil), brokers...),
		},
		topic: topic,
	}, nil
}

// DefaultPublisher returns a reconnecting publisher. An explicitly omitted
// broker list disables publication; a configured but unavailable broker is
// retried by the durable outbox without blocking backend startup.
func DefaultPublisher() *Publisher {
	brokers := nonEmptyBrokers(config.AppConfig.Brokers)
	if len(brokers) == 0 {
		log.Printf("Kafka unavailable: no brokers configured; durable events will remain queued")
		return &Publisher{topic: config.AppConfig.Topic}
	}
	producer, err := NewPublisher(brokers, config.AppConfig.Topic)
	if err != nil {
		log.Printf("Kafka unavailable at startup (%v); durable events will retry", err)
		return &Publisher{
			state: &publisherState{
				brokers:       append([]string(nil), brokers...),
				nextConnectAt: time.Now().Add(reconnectCooldown),
				lastError:     err,
			},
			topic: config.AppConfig.Topic,
		}
	}
	return producer
}

func nonEmptyBrokers(brokers []string) []string {
	out := make([]string, 0, len(brokers))
	for _, b := range brokers {
		if strings.TrimSpace(b) != "" {
			out = append(out, b)
		}
	}
	return out
}

func (p *Publisher) Close() error {
	if p == nil || p.state == nil {
		return nil
	}
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	p.state.closed = true
	if p.state.producer == nil {
		return nil
	}
	err := p.state.producer.Close()
	p.state.producer = nil
	return err
}

func (p *Publisher) Publish(event *AutomationEvent) error {
	if err := normalizeAutomationEvent(event); err != nil {
		return err
	}
	if p == nil || p.state == nil || len(p.state.brokers) == 0 {
		return fmt.Errorf("%w: no Kafka brokers are configured", ErrUnavailable)
	}
	message, err := json.Marshal(event)
	if err != nil {
		return err
	}

	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	if p.state.closed {
		return fmt.Errorf("%w: publisher is closed", ErrUnavailable)
	}
	if p.state.producer == nil {
		if time.Now().Before(p.state.nextConnectAt) {
			return fmt.Errorf("%w: %v", ErrUnavailable, p.state.lastError)
		}
		producer, connectErr := newSyncProducer(p.state.brokers)
		if connectErr != nil {
			p.state.lastError = connectErr
			p.state.nextConnectAt = time.Now().Add(reconnectCooldown)
			return fmt.Errorf("%w: %v", ErrUnavailable, connectErr)
		}
		p.state.producer = producer
		p.state.lastError = nil
	}

	msg := &sarama.ProducerMessage{
		Topic: p.topic,
		Value: sarama.StringEncoder(message),
		Key:   sarama.StringEncoder(event.Automation.ID.String()),
	}

	_, _, err = p.state.producer.SendMessage(msg)
	if err != nil {
		_ = p.state.producer.Close()
		p.state.producer = nil
		p.state.lastError = err
		p.state.nextConnectAt = time.Now().Add(reconnectCooldown)
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}

	log.Printf("Sent message to Kafka topic %s", p.topic)
	return nil
}

func newSyncProducer(brokers []string) (sarama.SyncProducer, error) {
	newConfig := sarama.NewConfig()
	newConfig.Producer.RequiredAcks = sarama.WaitForAll
	newConfig.Producer.Retry.Max = 5
	newConfig.Producer.Return.Successes = true
	return sarama.NewSyncProducer(brokers, newConfig)
}
