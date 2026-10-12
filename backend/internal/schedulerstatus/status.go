// Package schedulerstatus exposes whether this backend process attached its
// background workers. Durable job state remains persisted by durablejob.
package schedulerstatus

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"automation-hub-backend/internal/doctor"
)

type State struct {
	Name      string    `json:"name"`
	Enabled   bool      `json:"enabled"`
	Durable   bool      `json:"durable"`
	Running   bool      `json:"running"`
	Detail    string    `json:"detail"`
	UpdatedAt time.Time `json:"updatedAt"`
}

var registry = struct {
	sync.RWMutex
	items map[string]State
}{items: make(map[string]State)}

func Record(state State) {
	state.Name = strings.TrimSpace(state.Name)
	if state.Name == "" {
		return
	}
	state.Detail = strings.TrimSpace(state.Detail)
	state.UpdatedAt = time.Now().UTC()
	registry.Lock()
	registry.items[state.Name] = state
	registry.Unlock()
}

func Snapshot(name string) (State, bool) {
	registry.RLock()
	state, ok := registry.items[strings.TrimSpace(name)]
	registry.RUnlock()
	return state, ok
}

// Probe reports scheduler degradation as a warning, not a traffic-serving
// failure: the control plane remains available for inspection and recovery.
func Probe(name string) doctor.Probe {
	name = strings.TrimSpace(name)
	return doctor.Probe{
		Name:     "scheduler." + name,
		Critical: false,
		Run: func(context.Context) error {
			state, ok := Snapshot(name)
			if !ok {
				return fmt.Errorf("scheduler has not reported startup state")
			}
			if !state.Enabled {
				return fmt.Errorf("scheduler disabled%s", detailSuffix(state.Detail))
			}
			if !state.Running {
				return fmt.Errorf("scheduler is not running%s", detailSuffix(state.Detail))
			}
			if !state.Durable {
				return fmt.Errorf("scheduler is running without durable crash recovery%s", detailSuffix(state.Detail))
			}
			return nil
		},
	}
}

func detailSuffix(detail string) string {
	if detail == "" {
		return ""
	}
	return ": " + detail
}
