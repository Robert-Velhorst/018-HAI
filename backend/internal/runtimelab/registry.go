package runtimelab

import (
	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/executionbroker"
)

// Registry holds all runtime adapters (§15 required targets).
type Registry struct {
	adapters []Adapter
}

// NewRegistry assembles the required runtime targets:
//   - hai-local-safe-worker (real executor)
//   - Hermes, OpenClaw, Odysseus, OpenHands (external agent runtimes; env-driven, honest)
//   - browser runtime contract, local script runtime contract (contracts only)
func NewRegistry(broker *executionbroker.Broker) *Registry {
	return NewRegistryWithAgentRuntimeRegistry(broker, nil)
}

// NewRegistryWithAgentRuntimeRegistry shares a canonical agent-runtime
// registry when one is available. Runtime Lab remains inspection-only: it
// never gains task execution authority from the registry.
func NewRegistryWithAgentRuntimeRegistry(broker *executionbroker.Broker, agentRegistry *agentruntime.Registry) *Registry {
	openClaw := Adapter(newRemoteRuntime("openclaw", "OpenClaw", "OPENCLAW_BASE_URL"))
	if agentRegistry != nil {
		openClaw = newAgentRuntimeBridge(agentRegistry, "openclaw")
	}
	return &Registry{adapters: []Adapter{
		newSafeWorkerRuntime(broker),
		newRemoteRuntime("hermes", "Hermes", "HERMES_BASE_URL"),
		openClaw,
		newRemoteRuntime("odysseus", "Odysseus", "ODYSSEUS_BASE_URL"),
		newRemoteRuntime("openhands", "OpenHands", "OPENHANDS_BASE_URL"),
		newBrowserContract(),
		newLocalScriptContract(),
	}}
}

// Adapters returns all adapters.
func (r *Registry) Adapters() []Adapter { return r.adapters }

// Adapter returns an adapter by runtime id.
func (r *Registry) Adapter(id string) (Adapter, bool) {
	for _, a := range r.adapters {
		if a.Info().ID == id {
			return a, true
		}
	}
	return nil, false
}
