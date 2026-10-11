package openclawmaintenance

import (
	"encoding/json"
	"io"
	"runtime"
	"strings"
)

const (
	workerCapabilityProtocolVersion = 1
	companionJobContainmentID       = "windows-job-object-suspended-v1"
)

// WorkerCapabilityHandshake is carried on every authenticated worker request.
// The backend supplies the observation time; worker clocks are never trusted.
type WorkerCapabilityHandshake struct {
	ProtocolVersion         int    `json:"protocolVersion"`
	WorkerID                string `json:"workerId"`
	Platform                string `json:"platform"`
	Target                  string `json:"target"`
	Supported               bool   `json:"supported"`
	Containment             string `json:"containment"`
	CreateSuspended         bool   `json:"createSuspended"`
	AssignBeforeResume      bool   `json:"assignBeforeResume"`
	KillOnClose             bool   `json:"killOnClose"`
	WaitForJobEmpty         bool   `json:"waitForJobEmpty"`
	UnknownOnUnverifiedExit bool   `json:"unknownOnUnverifiedExit"`
}

func unsupportedWorkerCapability() WorkerCapabilityHandshake {
	return WorkerCapabilityHandshake{
		ProtocolVersion: workerCapabilityProtocolVersion,
		WorkerID:        "windows-openclaw-maintenance",
		Platform:        runtime.GOOS,
		Target:          "companion",
	}
}

func (capability WorkerCapabilityHandshake) supportsCompanionInstall() bool {
	return capability.ProtocolVersion == workerCapabilityProtocolVersion &&
		capability.WorkerID == "windows-openclaw-maintenance" &&
		capability.Platform == "windows" &&
		capability.Target == "companion" &&
		capability.Supported &&
		capability.Containment == companionJobContainmentID &&
		capability.CreateSuspended &&
		capability.AssignBeforeResume &&
		capability.KillOnClose &&
		capability.WaitForJobEmpty &&
		capability.UnknownOnUnverifiedExit
}

func capabilityFromHeader(value string) (WorkerCapabilityHandshake, bool) {
	if value == "" || len(value) > 1024 {
		return WorkerCapabilityHandshake{}, false
	}
	var capability WorkerCapabilityHandshake
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&capability); err != nil {
		return WorkerCapabilityHandshake{}, false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return WorkerCapabilityHandshake{}, false
	}
	return capability, capability.supportsCompanionInstall()
}
