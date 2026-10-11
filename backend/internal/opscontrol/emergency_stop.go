// Package opscontrol is HAI's always-on runtime control surface (§30/§31). It
// provides a verifiable emergency stop that actually halts background
// processing and survives restart, truthful Docker Desktop dependency handling,
// crash/reboot recovery of stuck operations, and a Windows-runtime readiness
// checklist. It never claims always-on/Windows capability that is not real:
// Windows-specific gates report pending on non-Windows hosts.
package opscontrol

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var ErrEmergencyStopStateChanged = errors.New("emergency-stop state changed concurrently")

// EmergencyStopState is the persisted emergency-stop state.
type EmergencyStopState struct {
	Engaged   bool       `json:"engaged"`
	Reason    string     `json:"reason,omitempty"`
	Actor     string     `json:"actor,omitempty"`
	EngagedAt *time.Time `json:"engagedAt,omitempty"`
	UpdatedAt time.Time  `json:"updatedAt"`
	Revision  uint64     `json:"revision"`
}

// EmergencyStopStore persists the emergency-stop state to a JSON file so the
// halt survives a crash/reboot (recovery-after-restart, §31). Reads/writes are
// serialized.
type EmergencyStopStore struct {
	mu    sync.Mutex
	path  string
	state EmergencyStopState
	err   error
}

// NewEmergencyStopStore loads (or initializes) the store rooted at dir.
func NewEmergencyStopStore(dir string) *EmergencyStopStore {
	s := &EmergencyStopStore{path: filepath.Join(dir, "emergency_stop.json")}
	s.load()
	return s
}

func (s *EmergencyStopStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			// Windows may report "not found" for child paths whose existing
			// parent is a regular file. That is persistence corruption, not a
			// clean first run, so execution must remain fail-closed.
			parent := filepath.Dir(s.path)
			info, parentErr := os.Stat(parent)
			switch {
			case parentErr == nil && !info.IsDir():
				s.err = fmt.Errorf(
					"read persisted emergency-stop state: state root %q is not a directory",
					parent,
				)
			case parentErr != nil && !os.IsNotExist(parentErr):
				s.err = fmt.Errorf(
					"inspect persisted emergency-stop state directory: %w",
					parentErr,
				)
			}
		} else {
			s.err = fmt.Errorf("read persisted emergency-stop state: %w", err)
		}
		return
	}
	var st EmergencyStopState
	if err := json.Unmarshal(data, &st); err != nil {
		s.err = fmt.Errorf("decode persisted emergency-stop state: %w", err)
		return
	}
	s.state = st
}

func (s *EmergencyStopStore) persist(state EmergencyStopState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode emergency-stop state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create emergency-stop state directory: %w", err)
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("write emergency-stop state: %w", err)
	}
	return nil
}

// Status returns the persisted state and any error that prevents the state from
// being trusted. Callers must treat an error as engaged.
func (s *EmergencyStopStore) Status() (EmergencyStopState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, s.err
}

// State returns the effective fail-closed state for operator-facing status.
func (s *EmergencyStopStore) State() EmergencyStopState {
	state, err := s.Status()
	if err == nil {
		return state
	}
	state.Engaged = true
	state.Reason = "persisted emergency-stop state is unavailable; execution remains blocked"
	state.Actor = "system"
	return state
}

// Engaged reports whether the emergency stop is active.
func (s *EmergencyStopStore) Engaged() bool {
	state, err := s.Status()
	return err != nil || state.Engaged
}

// SeedIfAbsent writes an initial state exactly once. Recording the false state
// matters too: otherwise a later configuration change could turn an already
// running installation into an emergency-stopped one on restart.
func (s *EmergencyStopStore) SeedIfAbsent(
	engaged bool,
	actor string,
	now time.Time,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.err != nil {
		return s.err
	}
	if _, err := os.Stat(s.path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		s.err = fmt.Errorf("inspect persisted emergency-stop state: %w", err)
		return s.err
	}
	if strings.TrimSpace(actor) == "" {
		actor = "system"
	}
	state := EmergencyStopState{
		Engaged:   engaged,
		Actor:     actor,
		UpdatedAt: now.UTC(),
		Revision:  1,
	}
	if engaged {
		state.Reason = "configured first-run emergency stop"
		engagedAt := state.UpdatedAt
		state.EngagedAt = &engagedAt
	}
	if err := s.persist(state); err != nil {
		s.err = err
		return err
	}
	s.state = state
	return nil
}

// Engage activates the emergency stop and persists it.
func (s *EmergencyStopStore) Engage(reason, actor string, now time.Time) (EmergencyStopState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := now.UTC()
	next := EmergencyStopState{
		Engaged:   true,
		Reason:    reason,
		Actor:     actor,
		EngagedAt: &t,
		UpdatedAt: t,
		Revision:  s.state.Revision + 1,
	}
	if err := s.persist(next); err != nil {
		// Engagement fails closed even when persistence is unavailable.
		s.state = next
		s.err = err
		return s.state, err
	}
	s.state = next
	s.err = nil
	return s.state, nil
}

// Disengage clears the emergency stop and persists it.
func (s *EmergencyStopStore) Disengage(actor string, now time.Time) (EmergencyStopState, error) {
	state, _ := s.Status()
	return s.DisengageIfRevision(state.Revision, actor, now)
}

// DisengageIfRevision clears the stop only when the exact authorized revision
// is still current. A concurrent stop decision therefore cannot be cleared.
func (s *EmergencyStopStore) DisengageIfRevision(
	expectedRevision uint64,
	actor string,
	now time.Time,
) (EmergencyStopState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Revision != expectedRevision {
		return s.state, ErrEmergencyStopStateChanged
	}
	next := EmergencyStopState{
		Engaged:   false,
		Actor:     actor,
		UpdatedAt: now.UTC(),
		Revision:  s.state.Revision + 1,
	}
	if err := s.persist(next); err != nil {
		s.err = err
		return s.state, err
	}
	s.state = next
	s.err = nil
	return s.state, nil
}

// RestoreIfRevision restores a verification snapshot only if no operator has
// changed the emergency stop since the verifier engaged it.
func (s *EmergencyStopStore) RestoreIfRevision(
	expectedRevision uint64,
	previous EmergencyStopState,
	now time.Time,
) (EmergencyStopState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Revision != expectedRevision {
		return s.state, ErrEmergencyStopStateChanged
	}
	next := previous
	next.Revision = s.state.Revision + 1
	next.UpdatedAt = now.UTC()
	if err := s.persist(next); err != nil {
		s.err = err
		return s.state, err
	}
	s.state = next
	s.err = nil
	return s.state, nil
}
