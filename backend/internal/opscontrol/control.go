package opscontrol

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"automation-hub-backend/internal/autonomypolicy"
)

var ErrAutonomyModeStateChanged = errors.New("autonomy mode changed concurrently")

// Controller is the dynamic background control consulted by the worker each
// pass: current autonomy mode + emergency-stop state. Both survive restart.
type Controller struct {
	mu        sync.Mutex
	emergency *EmergencyStopStore
	modePath  string
	mode      autonomypolicy.Mode
	modeErr   error
}

// NewController builds a controller rooted at dir (persisting mode + stop).
func NewController(dir string) *Controller {
	c := &Controller{
		emergency: NewEmergencyStopStore(dir),
		modePath:  filepath.Join(dir, "background_mode.json"),
		mode:      autonomypolicy.ModeAutonomousSafe,
	}
	c.loadMode()
	return c
}

// SeedInitialState records the configured startup controls only when this is a
// fresh state directory. Later starts always retain the operator's persisted
// decision, even if an environment file has changed in the meantime.
func (c *Controller) SeedInitialState(
	mode autonomypolicy.Mode,
	emergencyStop bool,
	actor string,
	now time.Time,
) error {
	if _, err := autonomypolicy.ParseMode(string(mode)); err != nil {
		return err
	}
	if err := c.seedModeIfAbsent(mode); err != nil {
		return err
	}
	return c.emergency.SeedIfAbsent(emergencyStop, actor, now)
}

func (c *Controller) seedModeIfAbsent(mode autonomypolicy.Mode) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.modeErr != nil {
		return c.modeErr
	}
	if _, err := os.Stat(c.modePath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		c.modeErr = fmt.Errorf("inspect persisted autonomy mode: %w", err)
		return c.modeErr
	}
	if err := c.persistMode(mode); err != nil {
		c.modeErr = fmt.Errorf("persist initial autonomy mode: %w", err)
		return c.modeErr
	}
	c.mode = mode
	return nil
}

func (c *Controller) loadMode() {
	data, err := os.ReadFile(c.modePath)
	if err != nil {
		if !os.IsNotExist(err) {
			c.modeErr = fmt.Errorf("read persisted autonomy mode: %w", err)
		}
		return
	}
	var wrap struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(data, &wrap); err != nil {
		c.modeErr = fmt.Errorf("decode persisted autonomy mode: %w", err)
		return
	}
	mode, err := autonomypolicy.ParseMode(wrap.Mode)
	if err != nil {
		c.modeErr = fmt.Errorf("validate persisted autonomy mode: %w", err)
		return
	}
	c.mode = mode
	c.modeErr = nil
}

func (c *Controller) persistMode(mode autonomypolicy.Mode) error {
	data, err := json.MarshalIndent(map[string]string{"mode": string(mode)}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.modePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(c.modePath, data, 0o600)
}

// Mode returns the current autonomy mode. When the emergency stop is engaged the
// effective mode is emergency_stopped regardless of the stored mode.
func (c *Controller) Mode() autonomypolicy.Mode {
	if c.emergency.Engaged() {
		return autonomypolicy.ModeEmergencyStopped
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.modeErr != nil {
		return autonomypolicy.ModePaused
	}
	return c.mode
}

// StoredMode returns the configured mode ignoring emergency stop.
func (c *Controller) StoredMode() autonomypolicy.Mode {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.modeErr != nil {
		return autonomypolicy.ModePaused
	}
	return c.mode
}

// ModePersistenceStatus returns the fail-closed stored mode and any error that
// prevents the persisted mode from being trusted.
func (c *Controller) ModePersistenceStatus() (autonomypolicy.Mode, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.modeErr != nil {
		return autonomypolicy.ModePaused, c.modeErr
	}
	return c.mode, nil
}

// EmergencyStop reports whether the emergency stop is engaged (the worker's
// Control contract).
func (c *Controller) EmergencyStop() bool { return c.emergency.Engaged() }

// EmergencyStopStatus implements safety.EmergencyStopProvider without
// importing safety and creating a package cycle.
func (c *Controller) EmergencyStopStatus() (bool, string, error) {
	state, err := c.emergency.Status()
	if err != nil {
		return true, "", err
	}
	return state.Engaged, state.Reason, nil
}

// SetMode updates + persists the autonomy mode.
func (c *Controller) SetMode(m autonomypolicy.Mode) (autonomypolicy.Mode, error) {
	if _, err := autonomypolicy.ParseMode(string(m)); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	current := c.mode
	if c.modeErr != nil {
		current = autonomypolicy.ModePaused
	}
	if err := c.persistMode(m); err != nil {
		return current, err
	}
	c.mode = m
	c.modeErr = nil
	return c.mode, nil
}

// SetModeIfCurrent updates the mode only if the authorization was derived from
// the still-current source mode.
func (c *Controller) SetModeIfCurrent(
	expected autonomypolicy.Mode,
	target autonomypolicy.Mode,
) (autonomypolicy.Mode, error) {
	if _, err := autonomypolicy.ParseMode(string(target)); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	current := c.mode
	if c.modeErr != nil {
		current = autonomypolicy.ModePaused
	}
	if current != expected {
		return current, ErrAutonomyModeStateChanged
	}
	if err := c.persistMode(target); err != nil {
		return current, err
	}
	c.mode = target
	c.modeErr = nil
	return c.mode, nil
}

// Engage activates the emergency stop.
func (c *Controller) Engage(reason, actor string, now time.Time) (EmergencyStopState, error) {
	return c.emergency.Engage(reason, actor, now)
}

// Disengage clears the emergency stop.
func (c *Controller) Disengage(actor string, now time.Time) (EmergencyStopState, error) {
	return c.emergency.Disengage(actor, now)
}

func (c *Controller) DisengageIfRevision(
	revision uint64,
	actor string,
	now time.Time,
) (EmergencyStopState, error) {
	return c.emergency.DisengageIfRevision(revision, actor, now)
}

func (c *Controller) RestoreIfRevision(
	revision uint64,
	state EmergencyStopState,
	now time.Time,
) (EmergencyStopState, error) {
	return c.emergency.RestoreIfRevision(revision, state, now)
}

// EmergencyState returns the persisted emergency-stop state.
func (c *Controller) EmergencyState() EmergencyStopState { return c.emergency.State() }
