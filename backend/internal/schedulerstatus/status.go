// Package schedulerstatus keeps the in-process startup state of background
// schedulers visible to the shared readiness endpoint. Durable jobs themselves
// remain persisted in Postgres; this package only answers whether this backend
// process successfully attached a worker to them.
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

var states = struct {
	sync.RWMutex
	items map[string]State
}{items: map[string]State{}}

func Record(state State) {
	state.Name = strings.TrimSpace(state.Name)
	if state.Name == "" {
		return
	}
	state.Detail = strings.TrimSpace(state.Detail)
	state.UpdatedAt = time.Now().UTC()
	states.Lock()
	states.items[state.Name] = state
	states.Unlock()
}

func Snapshot(name string) (State, bool) {
	states.RLock()
	state, ok := states.items[strings.TrimSpace(name)]
	states.RUnlock()
	return state, ok
}

// Probe makes a scheduler's startup status visible in /readyz. It is a warning
// rather than a traffic-serving failure: the control plane remains usable for
// inspection and recovery even when background work is intentionally paused.
func Probe(name string) doctor.Probe {
	name = strings.TrimSpace(name)
	return doctor.Probe{
		Name:     "scheduler." + name,
		Critical: false,
		Run: func(context.Context) error {
			state, ok := Snapshot(name)
			if !ok {
				return fmt.Errorf("scheduler has not reported a startup state")
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
