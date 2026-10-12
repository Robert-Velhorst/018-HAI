package phase2

import (
	"strings"
	"sync"
	"time"

	"automation-hub-backend/internal/safety"
)

// BlockRule is an operator "block similar" rule (§10.19). Future operations that
// match its operation type (and optional title keyword) are auto-blocked.
type BlockRule struct {
	OperationType string    `json:"operationType"`
	TitleKeyword  string    `json:"titleKeyword,omitempty"`
	Reason        string    `json:"reason"`
	SourceOpID    string    `json:"sourceOperationId"`
	CreatedAt     time.Time `json:"createdAt"`
}

// BlockRuleStore holds operator block-similar rules. It implements the
// background worker's BlockRules contract.
type BlockRuleStore struct {
	mu    sync.Mutex
	rules []BlockRule
}

// NewBlockRuleStore builds an empty store.
func NewBlockRuleStore() *BlockRuleStore { return &BlockRuleStore{} }

// Add registers a new block rule.
func (s *BlockRuleStore) Add(r BlockRule) {
	release := safety.AcquireEmergencyStopMutationFence()
	defer release()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules = append(s.rules, r)
}

// ShouldBlock reports whether an operation matches any rule.
func (s *BlockRuleStore) ShouldBlock(operationType, title string) (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lt := strings.ToLower(title)
	for _, r := range s.rules {
		if !strings.EqualFold(r.OperationType, operationType) {
			continue
		}
		if r.TitleKeyword != "" && !strings.Contains(lt, strings.ToLower(r.TitleKeyword)) {
			continue
		}
		return true, r.Reason
	}
	return false, ""
}

// List returns a snapshot of the rules.
func (s *BlockRuleStore) List() []BlockRule {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]BlockRule, len(s.rules))
	copy(out, s.rules)
	return out
}
