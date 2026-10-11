package infra

import (
	"errors"
	"strings"
	"testing"

	"github.com/IBM/sarama"
)

type partialKafkaProducer struct {
	sarama.SyncProducer
	closed   int
	closeErr error
}

func (p *partialKafkaProducer) Close() error { p.closed++; return p.closeErr }

func TestKafkaFactoryPartialFailureClosesProducerAndPreservesErrors(t *testing.T) {
	factoryError := errors.New("factory failure")
	closeError := errors.New("close failure")
	for _, cleanupError := range []error{nil, closeError} {
		partial := &partialKafkaProducer{closeErr: cleanupError}
		producer, err := newKafkaProducer([]string{"kafka:9092"}, "hai-idp", func([]string, *sarama.Config) (sarama.SyncProducer, error) { return partial, factoryError })
		if producer != nil || partial.closed != 1 || !errors.Is(err, factoryError) {
			t.Fatalf("producer=%v closed=%d err=%v", producer, partial.closed, err)
		}
		if cleanupError != nil && !errors.Is(err, cleanupError) {
			t.Fatal("cleanup error lost")
		}
	}
}

func TestKafkaInvalidClientIdentifierDoesNotEchoInput(t *testing.T) {
	secret := "synthetic secret identifier"
	called := false
	_, err := newKafkaProducer([]string{"kafka:9092"}, secret, func([]string, *sarama.Config) (sarama.SyncProducer, error) { called = true; return nil, nil })
	if err == nil || strings.Contains(err.Error(), secret) || called {
		t.Fatal("invalid identifier echoed or attempted connection")
	}
}
