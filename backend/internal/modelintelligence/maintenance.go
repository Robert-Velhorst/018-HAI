package modelintelligence

import (
	"context"
	"fmt"
	"strings"
)

// ModelMaintenanceGate is the legacy-compatible surface implemented by HAI's
// canonical LLM policy service. Local inference additionally requires the
// context-aware extension below so request cancellation can stop maintenance.
type ModelMaintenanceGate interface {
	EnsureConfiguredLocalModel(endpointURL, modelID string) error
}

// ContextModelMaintenanceGate is implemented by the canonical LLM service so
// request cancellation also stops waiting for or performing a local refresh.
type ContextModelMaintenanceGate interface {
	EnsureConfiguredLocalModelWithContext(ctx context.Context, endpointURL, modelID string) error
}

// MaintainedProvider identifies a real local model runtime that must pass the
// canonical daily maintenance check before Model Intelligence can call it.
// Deterministic in-process rules intentionally do not implement this contract:
// there is no downloaded model artifact to update or verify.
type MaintainedProvider interface {
	ModelMaintenanceIdentity() (endpointURL, modelID string, ok bool)
}

// deterministicLocalInference is intentionally package-private so only the
// built-in deterministic providers can opt out of model-artifact maintenance.
type deterministicLocalInference interface {
	isDeterministicLocalInference()
}

func isDeterministicProvider(provider Provider) bool {
	_, ok := provider.(deterministicLocalInference)
	return ok
}

// MaintainedLocalProvider preserves the original public interface name for
// integrations compiled against it. The broader name reflects that a
// provider-managed local bridge may expose the same maintenance identity.
type MaintainedLocalProvider = MaintainedProvider

// WithModelMaintenance binds Model Intelligence to the canonical policy gate.
// It is deliberately injected by the router instead of constructing another LLM
// service, keeping maintenance history, update controls, and execution blocks
// in one authoritative place.
func (s *Service) WithModelMaintenance(gate ModelMaintenanceGate) *Service {
	s.mu.Lock()
	s.maintenanceGate = gate
	s.mu.Unlock()
	return s
}

func (s *Service) ensureModelMaintenance(ctx context.Context, provider Provider, selected ModelProfile) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if nilModelDependency(provider) || strings.TrimSpace(provider.ID()) == "" || strings.TrimSpace(selected.ProviderID) != strings.TrimSpace(provider.ID()) || strings.TrimSpace(selected.ModelID) == "" {
		return fmt.Errorf("selected model maintenance metadata is unavailable")
	}
	// Provider-hosted models are checked and maintained by their provider. They
	// must never be sent to HAI's local artifact updater.
	if !selected.Local {
		return nil
	}
	if _, ok := provider.(deterministicLocalInference); ok {
		return nil
	}
	maintained, ok := provider.(MaintainedProvider)
	if !ok {
		return fmt.Errorf("daily model maintenance identity is unavailable for this local model runtime")
	}
	endpointURL, modelID, applicable := maintained.ModelMaintenanceIdentity()
	if !applicable || strings.TrimSpace(endpointURL) == "" || strings.TrimSpace(modelID) == "" {
		return fmt.Errorf("daily model maintenance identity is unavailable for this local model runtime")
	}
	if strings.TrimSpace(modelID) != strings.TrimSpace(selected.ModelID) {
		return fmt.Errorf("daily model maintenance identity does not match selected model %q", strings.TrimSpace(selected.ModelID))
	}
	s.mu.Lock()
	gate := s.maintenanceGate
	s.mu.Unlock()
	if nilModelDependency(gate) {
		return fmt.Errorf("daily model maintenance gate is unavailable for this local model runtime")
	}
	contextual, ok := gate.(ContextModelMaintenanceGate)
	if !ok {
		return fmt.Errorf("daily model maintenance gate does not support request cancellation")
	}
	err := contextual.EnsureConfiguredLocalModelWithContext(ctx, endpointURL, modelID)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("daily model maintenance blocked local model execution: %w", redactModelError(err))
	}
	return nil
}
