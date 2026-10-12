package modelintelligence

import "time"

// BillingStatus is the evidence-backed billing classification of inference.
// Endpoint location is not evidence that a gateway's upstream model is local
// or unmetered.
type BillingStatus string

const (
	BillingUnknown   BillingStatus = "unknown"
	BillingUnmetered BillingStatus = "unmetered"
	BillingPaid      BillingStatus = "paid"
)

// ModelProfile is the architecture-aware metadata + observed telemetry for one
// (provider, model) pair (§16). Metadata is declared; observed metrics are only
// filled from real probes/benchmarks/telemetry — never fabricated.
type ModelProfile struct {
	ProviderID                     string             `json:"providerId"`
	ModelID                        string             `json:"modelId"`
	DisplayName                    string             `json:"displayName"`
	ArchitectureFamily             ArchitectureFamily `json:"architectureFamily"`
	Lanes                          []RoutingLane      `json:"lanes"`
	ContextWindow                  int                `json:"contextWindow"`
	Local                          bool               `json:"local"` // inference is enabled as local only after exact operator attestation
	LocalInferenceOperatorAttested bool               `json:"localInferenceOperatorAttested"`
	EndpointLocal                  bool               `json:"endpointLocal"`
	Deterministic                  bool               `json:"deterministic"`
	Paid                           *bool              `json:"paid"` // nil means billing is unknown
	BillingStatus                  BillingStatus      `json:"billingStatus"`
	Status                         ProviderStatus     `json:"status"`
	ClaimLevel                     ClaimLevel         `json:"claimLevel"`

	// Observed metrics (filled only from real runs; zero means not measured).
	ObservedTokensPerSecond float64    `json:"observedTokensPerSecond"`
	ObservedRuns            int        `json:"observedRuns"`
	ObservedFailures        int        `json:"observedFailures"`
	LastProbedAt            *time.Time `json:"lastProbedAt,omitempty"`
	LastBenchmarkedAt       *time.Time `json:"lastBenchmarkedAt,omitempty"`
}

// Key is the stable "providerId/modelId" identifier.
func (p ModelProfile) Key() string { return p.ProviderID + "/" + p.ModelID }

func paidValue(status BillingStatus) *bool {
	if status == BillingUnknown || status == "" {
		return nil
	}
	paid := status == BillingPaid
	return &paid
}

// ServesLane reports whether the model is declared to serve the given lane.
func (p ModelProfile) ServesLane(l RoutingLane) bool {
	for _, x := range p.Lanes {
		if x == l {
			return true
		}
	}
	return false
}

// Usable reports whether the model may currently be routed to.
func (p ModelProfile) Usable() bool { return p.Status.Usable() }
