package phase2

import (
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/safety"
)

func TestBlockRuleMutationWaitsForFinalEffectFence(t *testing.T) {
	store := NewBlockRuleStore()
	release := safety.AcquireExecutionCommitFence()
	var once sync.Once
	finish := func() { once.Do(release) }
	defer finish()
	started, done := make(chan struct{}), make(chan struct{})
	go func() {
		close(started)
		store.Add(BlockRule{OperationType: "organize", TitleKeyword: "older", Reason: "operator block"})
		close(done)
	}()
	<-started
	select {
	case <-done:
		t.Fatal("block rule crossed an admitted final-effect boundary")
	case <-time.After(30 * time.Millisecond):
	}
	finish()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("block rule mutation did not resume after the effect")
	}
	if blocked, reason := store.ShouldBlock("organize", "Organize older workspace notes"); !blocked || reason != "operator block" {
		t.Fatalf("operator rule was not retained: %t / %q", blocked, reason)
	}
}
