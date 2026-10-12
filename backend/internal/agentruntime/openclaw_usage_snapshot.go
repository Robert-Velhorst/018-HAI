package agentruntime

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"automation-hub-backend/internal/safety"
)

type GatewayUsageTotals struct {
	Input              int64    `json:"input"`
	Output             int64    `json:"output"`
	CacheRead          int64    `json:"cacheRead"`
	CacheWrite         int64    `json:"cacheWrite"`
	TotalTokens        int64    `json:"totalTokens"`
	EstimatedCostUSD   *float64 `json:"estimatedCostUsd"`
	MissingCostEntries *int64   `json:"missingCostEntries"`
}

type GatewayModelUsage struct {
	Provider string             `json:"provider"`
	Model    string             `json:"model"`
	Count    int64              `json:"count"`
	Totals   GatewayUsageTotals `json:"totals"`
}

// Identity stays in the private receipt. The persisted JSON contains only the
// bounded, source-reported snapshot and never a transcript or mutable key.
type GatewaySessionUsageSnapshot struct {
	ExecutionReference string              `json:"-"`
	SessionKey         string              `json:"-"`
	SessionID          string              `json:"-"`
	Source             string              `json:"source"`
	Scope              string              `json:"scope"`
	StartDate          string              `json:"startDate"`
	EndDate            string              `json:"endDate"`
	ObservedAt         time.Time           `json:"observedAt"`
	Totals             GatewayUsageTotals  `json:"totals"`
	ModelsReported     bool                `json:"modelsReported"`
	Models             []GatewayModelUsage `json:"models,omitempty"`
}

func (s *GatewaySessionUsageSnapshot) ValidateFor(r OpenClawGatewayReceipt, finished, now time.Time) error {
	invalid := fmt.Errorf("invalid OpenClaw usage snapshot")
	if s == nil || s.Source != "openclaw.sessions.usage" || s.Scope != "session-instance" || r.SessionID == "" || s.SessionID != r.SessionID || s.SessionKey != r.SessionKey || s.ExecutionReference != r.ExecutionReference || r.CreatedAt.IsZero() || finished.IsZero() || finished.Before(r.CreatedAt) || s.StartDate != r.CreatedAt.UTC().Format("2006-01-02") || s.EndDate != finished.UTC().Format("2006-01-02") || s.ObservedAt.UnixMilli() < finished.UnixMilli() || s.ObservedAt.After(now.Add(5*time.Minute)) || !validGatewayUsageTotals(s.Totals) || len(s.Models) > 32 || (!s.ModelsReported && len(s.Models) > 0) {
		return invalid
	}
	seen := map[string]bool{}
	for _, m := range s.Models {
		if len(m.Provider) > 120 || len(m.Model) > 255 || m.Count < 0 || m.Count > 9007199254740991 || !validGatewayUsageTotals(m.Totals) {
			return invalid
		}
		// Unknown provider/model identities remain empty, not the requested model.
		provider, model := m.Provider, m.Model
		if provider == "" {
			provider = "unknown"
		}
		if model == "" {
			model = "unknown"
		}
		if safety.ValidateRuntimeModel("openclaw", provider+"/"+model) != nil || safety.RedactSecrets(m.Provider) != m.Provider || safety.RedactSecrets(m.Model) != m.Model {
			return invalid
		}
		key := m.Provider + "\x00" + m.Model
		if seen[key] {
			return invalid
		}
		seen[key] = true
	}
	return nil
}

func validGatewayUsageTotals(t GatewayUsageTotals) bool {
	for _, n := range []int64{t.Input, t.Output, t.CacheRead, t.CacheWrite, t.TotalTokens} {
		if n < 0 || n > 9007199254740991 {
			return false
		}
	}
	if t.MissingCostEntries != nil && (*t.MissingCostEntries < 0 || *t.MissingCostEntries > 9007199254740991) {
		return false
	}
	return t.EstimatedCostUSD == nil || (!math.IsNaN(*t.EstimatedCostUSD) && !math.IsInf(*t.EstimatedCostUSD, 0) && *t.EstimatedCostUSD >= 0 && t.MissingCostEntries != nil && *t.MissingCostEntries == 0)
}

func (s *GatewaySessionUsageSnapshot) AuditSummary() string {
	t := s.Totals
	cost := "estimated cost unavailable"
	if t.EstimatedCostUSD != nil {
		cost = fmt.Sprintf("estimated USD=%.8g (not provider billing)", *t.EstimatedCostUSD)
	}
	return fmt.Sprintf("OpenClaw sessions.usage session-instance snapshot; UTC %s through %s; observed %s; tokens input=%d output=%d cache-read=%d cache-write=%d total=%d; %s; not per-run usage or a budget debit", s.StartDate, s.EndDate, s.ObservedAt.UTC().Format(time.RFC3339Nano), t.Input, t.Output, t.CacheRead, t.CacheWrite, t.TotalTokens, cost)
}

type gatewayWireUsageTotals struct {
	Input              *int64   `json:"input"`
	Output             *int64   `json:"output"`
	CacheRead          *int64   `json:"cacheRead"`
	CacheWrite         *int64   `json:"cacheWrite"`
	TotalTokens        *int64   `json:"totalTokens"`
	TotalCost          *float64 `json:"totalCost"`
	MissingCostEntries *int64   `json:"missingCostEntries"`
}

func (w gatewayWireUsageTotals) normalized() (GatewayUsageTotals, error) {
	invalid := fmt.Errorf("missing or invalid OpenClaw usage totals")
	if w.Input == nil || w.Output == nil || w.CacheRead == nil || w.CacheWrite == nil || w.TotalTokens == nil {
		return GatewayUsageTotals{}, invalid
	}
	t := GatewayUsageTotals{Input: *w.Input, Output: *w.Output, CacheRead: *w.CacheRead, CacheWrite: *w.CacheWrite, TotalTokens: *w.TotalTokens, MissingCostEntries: w.MissingCostEntries}
	if w.TotalCost != nil && *w.TotalCost < 0 {
		return GatewayUsageTotals{}, invalid
	}
	if w.MissingCostEntries != nil && *w.MissingCostEntries == 0 {
		t.EstimatedCostUSD = w.TotalCost
	}
	if !validGatewayUsageTotals(t) {
		return GatewayUsageTotals{}, invalid
	}
	return t, nil
}

func openClawSessionUsageSnapshot(payload json.RawMessage, r OpenClawGatewayReceipt, finished, now time.Time) (*GatewaySessionUsageSnapshot, error) {
	if len(payload) > 65536 {
		return nil, fmt.Errorf("OpenClaw usage exceeds import bound")
	}
	var wire struct {
		UpdatedAt int64  `json:"updatedAt"`
		StartDate string `json:"startDate"`
		EndDate   string `json:"endDate"`
		Sessions  []struct {
			Key                string   `json:"key"`
			SessionID          string   `json:"sessionId"`
			Scope              string   `json:"scope"`
			IncludedSessionIDs []string `json:"includedSessionIds"`
			Usage              struct {
				SessionID string `json:"sessionId"`
				gatewayWireUsageTotals
				ModelUsage *[]struct {
					Provider string                 `json:"provider"`
					Model    string                 `json:"model"`
					Count    *int64                 `json:"count"`
					Totals   gatewayWireUsageTotals `json:"totals"`
				} `json:"modelUsage"`
			} `json:"usage"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(payload, &wire); err != nil {
		return nil, err
	}
	if len(wire.Sessions) != 1 {
		return nil, fmt.Errorf("OpenClaw usage requires one session instance")
	}
	session := wire.Sessions[0]
	if session.Scope != "instance" || session.SessionID != session.Usage.SessionID || len(session.IncludedSessionIDs) > 1 || (len(session.IncludedSessionIDs) == 1 && session.IncludedSessionIDs[0] != session.SessionID) {
		return nil, fmt.Errorf("OpenClaw usage has ambiguous instance scope")
	}
	u := session.Usage
	totals, err := u.gatewayWireUsageTotals.normalized()
	if err != nil {
		return nil, err
	}
	s := &GatewaySessionUsageSnapshot{ExecutionReference: r.ExecutionReference, SessionKey: session.Key, SessionID: session.SessionID, Source: "openclaw.sessions.usage", Scope: "session-instance", StartDate: wire.StartDate, EndDate: wire.EndDate, ObservedAt: time.UnixMilli(wire.UpdatedAt).UTC(), Totals: totals, ModelsReported: u.ModelUsage != nil}
	if u.ModelUsage != nil {
		if len(*u.ModelUsage) > 32 {
			return nil, fmt.Errorf("OpenClaw model usage exceeds import bound")
		}
		for _, m := range *u.ModelUsage {
			if m.Count == nil {
				return nil, fmt.Errorf("OpenClaw model usage count unavailable")
			}
			t, err := m.Totals.normalized()
			if err != nil {
				return nil, err
			}
			s.Models = append(s.Models, GatewayModelUsage{Provider: m.Provider, Model: m.Model, Count: *m.Count, Totals: t})
		}
	}
	if err := s.ValidateFor(r, finished, now); err != nil {
		return nil, err
	}
	return s, nil
}

func openClawSessionUsageSummary(payload json.RawMessage, r OpenClawGatewayReceipt, finished, now time.Time) (string, error) {
	s, err := openClawSessionUsageSnapshot(payload, r, finished, now)
	if err != nil {
		return "", err
	}
	return s.AuditSummary(), nil
}
