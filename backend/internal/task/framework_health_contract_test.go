package task

import (
	"reflect"
	"testing"
	"time"

	"automation-hub-backend/internal/agentregistry"
	"automation-hub-backend/internal/frameworkregistry"
)

func TestFrameworkHealthRejectsRegistryDeclarationsAsProbeEvidence(t *testing.T) {
	now := agentContextTestNow
	for _, name := range []string{"fresh declaration", "stale declaration", "disabled", "quarantined", "full capacity", "draining"} {
		t.Run(name, func(t *testing.T) {
			agent := validRegistryAgent("alice", now)
			agent.Health.CheckedAt = now.Add(-30 * time.Second)
			switch name {
			case "stale declaration":
				agent.Health.CheckedAt = now.Add(-2 * time.Hour)
			case "disabled":
				agent.State = agentregistry.StateDisabled
			case "quarantined":
				agent.State = agentregistry.StateQuarantined
			case "full capacity":
				agent.Availability.ActiveAssignments = agent.Availability.MaxConcurrent
			case "draining":
				agent.State = agentregistry.StateDraining
			}
			card := mapRegistryAgentCard(agent, now)
			plan := &CompletionPlan{OwnerIdentity: "alice", ExecutionPlan: ExecutionPlan{AgentCards: []frameworkregistry.AgentCard{card}}}
			evidence := (&service{}).preAuthorizationEvidence(plan, IntakeRequest{OwnerIdentity: "alice"}, FrameworkEvidenceContract{Validator: "live_health", MaxAgeSeconds: 60}, now)
			if len(evidence) != 0 {
				t.Fatalf("registry declaration became live health evidence: %#v", evidence)
			}
			if !reflect.DeepEqual(plan.ExecutionPlan.AgentCards[0], card) {
				t.Fatal("health observation changed the declaration or execution authority")
			}
		})
	}
}

func TestFrameworkHealthRejectsLegacyAndRelabeledRegistryVerificationFlags(t *testing.T) {
	now := agentContextTestNow
	for _, provenance := range []string{
		"agent_registry:research-agent revision=7",
		"agent_registry_declaration:research-agent revision=7",
		"  AGENT_REGISTRY:research-agent revision=7  ",
		"  AGENT_REGISTRY_DECLARATION:research-agent revision=7  ",
	} {
		for _, health := range []string{"available", "healthy"} {
			t.Run(provenance+"/"+health, func(t *testing.T) {
				card := mapRegistryAgentCard(validRegistryAgent("alice", now), now)
				checkedAt := now.Add(-30 * time.Second)
				card.Verified = true
				card.Status = "available"
				card.HealthStatus = health
				card.LastVerifiedAt = &checkedAt
				card.Provenance = provenance
				plan := &CompletionPlan{OwnerIdentity: "alice", ExecutionPlan: ExecutionPlan{AgentCards: []frameworkregistry.AgentCard{card}}}
				evidence := (&service{}).preAuthorizationEvidence(plan, IntakeRequest{OwnerIdentity: "alice"}, FrameworkEvidenceContract{Validator: "live_health", MaxAgeSeconds: 60}, now)
				if len(evidence) != 0 {
					t.Fatalf("legacy/relabeled registry card became probe evidence: %#v", evidence)
				}
				if !reflect.DeepEqual(plan.ExecutionPlan.AgentCards[0], card) {
					t.Fatal("health rejection rewrote historical declaration evidence")
				}
			})
		}
	}
}

func TestFrameworkHealthRetainsExistingTrustedInProcessContract(t *testing.T) {
	now := agentContextTestNow
	for _, name := range []string{"fresh", "unverified", "revoked", "inventory only", "no timestamp", "zero timestamp", "future timestamp", "expired contract"} {
		t.Run(name, func(t *testing.T) {
			checkedAt := now.Add(-30 * time.Second)
			// This unit-only card matches the existing in-process health fixture;
			// it is not a registry declaration or a production probe implementation.
			card := frameworkregistry.AgentCard{
				ID: "hai_task_engine", Verified: true, HealthStatus: "healthy",
				Provenance: "embedded_canonical_go_engine", LastVerifiedAt: &checkedAt,
			}
			maxAgeSeconds := int64(60)
			switch name {
			case "unverified":
				card.Verified = false
			case "revoked":
				card.Revoked = true
			case "inventory only":
				card.HealthStatus = "available"
			case "no timestamp":
				card.LastVerifiedAt = nil
			case "zero timestamp":
				checkedAt = time.Time{}
				maxAgeSeconds = 0
			case "future timestamp":
				checkedAt = now.Add(time.Hour)
			case "expired contract":
				checkedAt = now.Add(-5 * time.Minute)
			}
			plan := &CompletionPlan{OwnerIdentity: "alice", ExecutionPlan: ExecutionPlan{AgentCards: []frameworkregistry.AgentCard{card}}}
			evidence := (&service{}).preAuthorizationEvidence(plan, IntakeRequest{OwnerIdentity: "alice"}, FrameworkEvidenceContract{Validator: "live_health", MaxAgeSeconds: maxAgeSeconds}, now)
			if (len(evidence) > 0) != (name == "fresh") {
				t.Fatalf("in-process health evidence admitted=%t for %s", len(evidence) > 0, name)
			}
			if !reflect.DeepEqual(plan.ExecutionPlan.AgentCards[0], card) {
				t.Fatal("health observation changed in-process verification or authority")
			}
		})
	}
}
