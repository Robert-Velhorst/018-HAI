package autoconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/IBM/sarama"
)

const (
	inboxFileMode       = 0o600
	inboxDirectoryMode  = 0o700
	defaultPruneLimit   = 1000
	maximumReceiptError = 1000
)

type inboxReceipt struct {
	Key        string    `json:"key"`
	EventID    string    `json:"eventId,omitempty"`
	Topic      string    `json:"topic"`
	Partition  int32     `json:"partition"`
	Offset     int64     `json:"offset"`
	Status     string    `json:"status"`
	Attempt    int       `json:"attempt"`
	LastError  string    `json:"lastError,omitempty"`
	RecordedAt time.Time `json:"recordedAt"`
}

// Inbox makes Kafka delivery restart-safe without adding another service or
// database. Receipts live on the existing persistent config bind mount. The
// effect and receipt check are serialized because nginx configuration updates
// modify one shared directory and must have a deterministic order.
type Inbox struct {
	dir         string
	maxAttempts int
	retention   time.Duration
	mu          sync.Mutex
	now         func() time.Time
}

func NewInbox(dir string, maxAttempts int, retention time.Duration) (*Inbox, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" || maxAttempts < 1 || retention <= 0 {
		return nil, fmt.Errorf("create event inbox: directory, retry budget, and retention are required")
	}
	if err := os.MkdirAll(dir, inboxDirectoryMode); err != nil {
		return nil, fmt.Errorf("create event inbox directory: %w", err)
	}
	inbox := &Inbox{dir: dir, maxAttempts: maxAttempts, retention: retention, now: time.Now}
	if _, err := inbox.Prune(defaultPruneLimit); err != nil {
		return nil, err
	}
	return inbox, nil
}

// Process returns terminal=true only when the caller may advance the Kafka
// offset. A transient failure remains uncommitted. Once the bounded retry
// budget is exhausted, a durable dead-letter receipt isolates the poison
// message and allows later messages to proceed.
func (i *Inbox) Process(msg *sarama.ConsumerMessage, effect func(*sarama.ConsumerMessage) error) (terminal bool, err error) {
	if i == nil || msg == nil || effect == nil {
		return false, fmt.Errorf("process event inbox: message and effect are required")
	}
	i.mu.Lock()
	defer i.mu.Unlock()

	key, eventID := inboxMessageKey(msg)
	base := filepath.Join(i.dir, inboxKeyHash(key))
	terminal, err = terminalReceiptExists(base)
	if err != nil || terminal {
		return terminal, err
	}

	if effectErr := effect(msg); effectErr != nil {
		attempt, recordErr := i.recordFailure(base, key, eventID, msg, effectErr)
		if recordErr != nil {
			return false, recordErr
		}
		if attempt >= i.maxAttempts {
			log.Printf("Dead-lettered Kafka event %s after %d attempts: %v", key, attempt, effectErr)
			return true, nil
		}
		return false, fmt.Errorf("process Kafka event %s attempt %d/%d: %w", key, attempt, i.maxAttempts, effectErr)
	}

	receipt := inboxReceipt{
		Key: key, EventID: eventID, Topic: msg.Topic, Partition: msg.Partition,
		Offset: msg.Offset, Status: "completed", RecordedAt: i.now().UTC(),
	}
	if err := writeReceiptExclusive(base+".done.json", receipt); err != nil && !errors.Is(err, os.ErrExist) {
		return false, fmt.Errorf("record completed Kafka event %s: %w", key, err)
	}
	i.removeAttempts(base)
	return true, nil
}

func (i *Inbox) recordFailure(base, key, eventID string, msg *sarama.ConsumerMessage, failure error) (int, error) {
	attempt := countAttemptReceipts(base) + 1
	if attempt > i.maxAttempts {
		attempt = i.maxAttempts
	}
	lastError := strings.TrimSpace(failure.Error())
	if len(lastError) > maximumReceiptError {
		lastError = lastError[:maximumReceiptError]
	}
	receipt := inboxReceipt{
		Key: key, EventID: eventID, Topic: msg.Topic, Partition: msg.Partition,
		Offset: msg.Offset, Status: "failed", Attempt: attempt,
		LastError: lastError, RecordedAt: i.now().UTC(),
	}
	attemptPath := fmt.Sprintf("%s.attempt-%03d.json", base, attempt)
	if err := writeReceiptExclusive(attemptPath, receipt); err != nil && !errors.Is(err, os.ErrExist) {
		return attempt, fmt.Errorf("record failed Kafka event %s: %w", key, err)
	}
	if attempt < i.maxAttempts {
		return attempt, nil
	}
	receipt.Status = "dead_lettered"
	if err := writeReceiptExclusive(base+".dead.json", receipt); err != nil && !errors.Is(err, os.ErrExist) {
		return attempt, fmt.Errorf("dead-letter Kafka event %s: %w", key, err)
	}
	i.removeAttempts(base)
	return attempt, nil
}

func (i *Inbox) removeAttempts(base string) {
	matches, _ := filepath.Glob(base + ".attempt-*.json")
	for _, path := range matches {
		_ = os.Remove(path)
	}
}

func (i *Inbox) Prune(limit int) (int, error) {
	if i == nil {
		return 0, fmt.Errorf("prune event inbox: inbox is required")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if limit <= 0 || limit > 5000 {
		limit = defaultPruneLimit
	}
	entries, err := os.ReadDir(i.dir)
	if err != nil {
		return 0, fmt.Errorf("read event inbox: %w", err)
	}
	cutoff := i.now().Add(-i.retention)
	removed := 0
	for _, entry := range entries {
		if removed >= limit || entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return removed, fmt.Errorf("inspect event inbox receipt: %w", err)
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(i.dir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, fmt.Errorf("prune event inbox receipt: %w", err)
		}
		removed++
	}
	return removed, nil
}

func terminalReceiptExists(base string) (bool, error) {
	for _, suffix := range []string{".done.json", ".dead.json"} {
		_, err := os.Stat(base + suffix)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, os.ErrNotExist):
			continue
		default:
			return false, err
		}
	}
	return false, nil
}

func countAttemptReceipts(base string) int {
	matches, err := filepath.Glob(base + ".attempt-*.json")
	if err != nil {
		return 0
	}
	return len(matches)
}

func writeReceiptExclusive(path string, receipt inboxReceipt) error {
	payload, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, inboxFileMode)
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(payload); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	remove = false
	return nil
}

func inboxMessageKey(msg *sarama.ConsumerMessage) (string, string) {
	var envelope struct {
		EventID string `json:"eventId"`
	}
	if json.Unmarshal(msg.Value, &envelope) == nil && canonicalUUID(envelope.EventID) {
		id := strings.ToLower(envelope.EventID)
		return "event:" + id, id
	}
	return fmt.Sprintf("offset:%s:%d:%d", msg.Topic, msg.Partition, msg.Offset), ""
}

func canonicalUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	compact := strings.ReplaceAll(value, "-", "")
	if len(compact) != 32 {
		return false
	}
	_, err := hex.DecodeString(compact)
	return err == nil
}

func inboxKeyHash(key string) string {
	digest := sha256.Sum256([]byte(key))
	return hex.EncodeToString(digest[:])
}
