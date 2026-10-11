package agentruntime

import "context"

// SetOpenClawMaintenanceGate is configured once, before the registry is served.
func (r *Registry) SetOpenClawMaintenanceGate(gate func(context.Context) (func(), error)) {
	if adapter, ok := r.adapters["openclaw"].(*openClawAdapter); ok {
		adapter.maintenanceGate = gate
	}
}
