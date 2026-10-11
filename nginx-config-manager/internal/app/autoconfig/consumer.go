package autoconfig

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"automation-hub-nginxconfigmanager/internal/app/config"

	"github.com/IBM/sarama"
)

const consumerRestartBackoff = time.Second

type Consumer struct {
	group sarama.ConsumerGroup
	topic string
	inbox *Inbox
	ready atomic.Bool
}

func NewConsumer(brokers []string, topic, groupID string, inbox *Inbox) (*Consumer, error) {
	if len(brokers) == 0 || topic == "" || groupID == "" || inbox == nil {
		return nil, fmt.Errorf("create Kafka consumer: brokers, topic, group id, and inbox are required")
	}
	group, err := sarama.NewConsumerGroup(brokers, groupID, newConsumerConfig())
	if err != nil {
		return nil, err
	}
	return &Consumer{group: group, topic: topic, inbox: inbox}, nil
}

func newConsumerConfig() *sarama.Config {
	cfg := sarama.NewConfig()
	cfg.ClientID = "hai-nginx-config-manager"
	cfg.Consumer.Group.Rebalance.Strategy = sarama.NewBalanceStrategyRange()
	cfg.Consumer.Offsets.Initial = sarama.OffsetOldest
	cfg.Consumer.Offsets.AutoCommit.Enable = true
	cfg.Consumer.Offsets.AutoCommit.Interval = time.Second
	return cfg
}

func DefaultConsumer() (*Consumer, error) {
	if err := config.AppConfig.Validate(); err != nil {
		return nil, err
	}
	inbox, err := NewInbox(config.AppConfig.InboxDir, config.AppConfig.MaxAttempts, config.AppConfig.Retention)
	if err != nil {
		return nil, err
	}
	consumer, err := NewConsumer(
		config.AppConfig.Brokers,
		config.AppConfig.Topic,
		config.AppConfig.ConsumerGroup,
		inbox,
	)
	if err != nil {
		return nil, fmt.Errorf("create default consumer: %w", err)
	}
	return consumer, nil
}

func (c *Consumer) Ready() bool {
	return c != nil && c.ready.Load()
}

func (c *Consumer) Start(ctx context.Context) error {
	if c == nil || c.group == nil || c.inbox == nil {
		return fmt.Errorf("start Kafka consumer: consumer is not initialized")
	}
	handler := &consumerGroupHandler{ready: &c.ready, inbox: c.inbox}
	for {
		if err := c.group.Consume(ctx, []string{c.topic}, handler); err != nil {
			c.ready.Store(false)
			if ctx.Err() != nil || errors.Is(err, sarama.ErrClosedConsumerGroup) {
				return nil
			}
			log.Printf("Kafka consumer group pass failed; retrying in %s: %v", consumerRestartBackoff, err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(consumerRestartBackoff):
			}
			continue
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

func (c *Consumer) Close() error {
	if c == nil || c.group == nil {
		return nil
	}
	c.ready.Store(false)
	return c.group.Close()
}

type consumerGroupHandler struct {
	ready *atomic.Bool
	inbox *Inbox
}

func (h *consumerGroupHandler) Setup(sarama.ConsumerGroupSession) error {
	h.ready.Store(true)
	return nil
}

func (h *consumerGroupHandler) Cleanup(sarama.ConsumerGroupSession) error {
	h.ready.Store(false)
	return nil
}

func (h *consumerGroupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		select {
		case <-session.Context().Done():
			return nil
		case msg, ok := <-claim.Messages():
			if !ok {
				return nil
			}
			terminal, err := h.inbox.Process(msg, applyMessage)
			if err != nil {
				return err
			}
			if terminal {
				session.MarkMessage(msg, "")
			}
		}
	}
}
