package services

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IBM/sarama"
)

type lifecycleProducer struct {
	sarama.SyncProducer
	send  func(*sarama.ProducerMessage) (int32, int64, error)
	close func() error
}

func (p lifecycleProducer) SendMessage(m *sarama.ProducerMessage) (int32, int64, error) {
	return p.send(m)
}
func (p lifecycleProducer) Close() error { return p.close() }

func TestKafkaLoggerCloseOnceAndNoSendAfterClose(t *testing.T) {
	var closed, sent atomic.Int32
	failure := errors.New("synthetic close failure")
	p := lifecycleProducer{send: func(*sarama.ProducerMessage) (int32, int64, error) { sent.Add(1); return 0, 0, nil }, close: func() error { closed.Add(1); return failure }}
	k, err := newKafkaLogger(p, "logs")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !errors.Is(k.Close(), failure) {
				t.Error("close failure lost")
			}
		}()
	}
	wg.Wait()
	k.Info("synthetic private message after close")
	if closed.Load() != 1 || sent.Load() != 0 {
		t.Fatalf("close=%d send=%d", closed.Load(), sent.Load())
	}
	var absent *KafkaLogger
	if absent.Close() != nil || (&KafkaLogger{}).Close() != nil {
		t.Fatal("empty logger cleanup failed")
	}
}

func TestKafkaLoggerCloseWaitsForSend(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	closed := make(chan struct{})
	p := lifecycleProducer{send: func(*sarama.ProducerMessage) (int32, int64, error) { close(started); <-release; return 0, 0, nil }, close: func() error { close(closed); return nil }}
	k, err := newKafkaLogger(p, "logs")
	if err != nil {
		t.Fatal(err)
	}
	doneSend := make(chan struct{})
	go func() { k.Info("in-flight message"); close(doneSend) }()
	<-started
	if k.mu.TryLock() {
		k.mu.Unlock()
		close(release)
		t.Fatal("send does not hold the lifecycle lock")
	}
	doneClose := make(chan error, 1)
	go func() { doneClose <- k.Close() }()
	select {
	case <-closed:
		close(release)
		t.Fatal("producer closed during send")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	select {
	case <-doneSend:
	case <-time.After(time.Second):
		t.Fatal("send did not finish")
	}
	select {
	case err := <-doneClose:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not finish")
	}
}

func TestKafkaLoggerTransportErrorDoesNotLeakPayload(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	p := lifecycleProducer{send: func(*sarama.ProducerMessage) (int32, int64, error) {
		return 0, 0, errors.New("password=synthetic-transport-secret")
	}, close: func() error { return nil }}
	k, err := newKafkaLogger(p, "logs")
	if err != nil {
		t.Fatal(err)
	}
	k.Error("operation failed")
	if !strings.Contains(output.String(), "transport error") || strings.Contains(output.String(), "synthetic-transport-secret") {
		t.Fatal("transport diagnostic leaks or is missing")
	}
	if err := k.Close(); err != nil {
		t.Fatal(err)
	}
}
