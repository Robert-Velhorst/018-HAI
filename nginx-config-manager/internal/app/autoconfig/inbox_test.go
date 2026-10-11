package autoconfig

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/IBM/sarama"
)

func TestInboxDeduplicatesCompletedEventAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	inbox, err := NewInbox(dir, 3, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	msg := inboxTestMessage("4f071772-1a61-41cc-8b61-cb9017cd2b52", 17)
	effects := 0
	effect := func(*sarama.ConsumerMessage) error {
		effects++
		return nil
	}

	for attempt := 0; attempt < 2; attempt++ {
		terminal, err := inbox.Process(msg, effect)
		if err != nil || !terminal {
			t.Fatalf("Process() = (%v, %v), want terminal success", terminal, err)
		}
	}
	restarted, err := NewInbox(dir, 3, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := restarted.Process(msg, effect)
	if err != nil || !terminal {
		t.Fatalf("Process() after restart = (%v, %v), want terminal success", terminal, err)
	}
	if effects != 1 {
		t.Fatalf("effect count = %d, want 1", effects)
	}
}

func TestInboxDeadLettersAfterBoundedFailures(t *testing.T) {
	inbox, err := NewInbox(t.TempDir(), 3, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	msg := inboxTestMessage("c5f26277-84ae-4917-8482-bfd07f74c693", 23)
	effects := 0
	effect := func(*sarama.ConsumerMessage) error {
		effects++
		return errors.New("invalid generated nginx config")
	}

	for attempt := 1; attempt <= 2; attempt++ {
		terminal, err := inbox.Process(msg, effect)
		if err == nil || terminal {
			t.Fatalf("attempt %d = (%v, %v), want retryable failure", attempt, terminal, err)
		}
	}
	terminal, err := inbox.Process(msg, effect)
	if err != nil || !terminal {
		t.Fatalf("final attempt = (%v, %v), want durable dead letter", terminal, err)
	}
	terminal, err = inbox.Process(msg, effect)
	if err != nil || !terminal {
		t.Fatalf("dead-letter replay = (%v, %v), want terminal skip", terminal, err)
	}
	if effects != 3 {
		t.Fatalf("effect count = %d, want 3", effects)
	}
	dead, err := filepath.Glob(filepath.Join(inbox.dir, "*.dead.json"))
	if err != nil || len(dead) != 1 {
		t.Fatalf("dead-letter receipts = %v, %v", dead, err)
	}
	attempts, err := filepath.Glob(filepath.Join(inbox.dir, "*.attempt-*.json"))
	if err != nil || len(attempts) != 0 {
		t.Fatalf("attempt receipts were not compacted: %v, %v", attempts, err)
	}
}

func TestInboxUsesOffsetIdentityForLegacyEnvelope(t *testing.T) {
	inbox, err := NewInbox(t.TempDir(), 2, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	msg := &sarama.ConsumerMessage{Topic: "automation-events", Partition: 0, Offset: 44, Value: []byte(`{"type":"legacy"}`)}
	effects := 0
	for attempt := 0; attempt < 2; attempt++ {
		terminal, err := inbox.Process(msg, func(*sarama.ConsumerMessage) error { effects++; return nil })
		if err != nil || !terminal {
			t.Fatalf("Process() = (%v, %v), want terminal success", terminal, err)
		}
	}
	if effects != 1 {
		t.Fatalf("effect count = %d, want 1", effects)
	}
}

func TestInboxPrunesExpiredReceiptsInBoundedBatch(t *testing.T) {
	inbox, err := NewInbox(t.TempDir(), 2, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	for _, name := range []string{"one.done.json", "two.dead.json"} {
		path := filepath.Join(inbox.dir, name)
		if err := os.WriteFile(path, []byte(`{}`), inboxFileMode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := inbox.Prune(1)
	if err != nil || removed != 1 {
		t.Fatalf("Prune(1) = (%d, %v), want one removal", removed, err)
	}
	entries, err := os.ReadDir(inbox.dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("remaining receipts = %d, %v", len(entries), err)
	}
}

func inboxTestMessage(eventID string, offset int64) *sarama.ConsumerMessage {
	return &sarama.ConsumerMessage{
		Topic: "automation-events", Partition: 0, Offset: offset,
		Value: []byte(`{"eventId":"` + eventID + `","type":"create","automation":{"name":"Test","urlPath":"test","host":"backend","port":80}}`),
	}
}
