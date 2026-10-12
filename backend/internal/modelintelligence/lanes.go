package modelintelligence

import (
	"sort"
	"strings"
	"time"
)

// LaneInput describes the work to be routed so lanes affect real behavior.
type LaneInput struct {
	OperationType string
	Title         string
	Content       string
	SourceBytes   int
	HighRisk      bool
	MessyInput    bool // OCR/messy/byte-level input
	SafeForCloud  bool // from the privacy filter
	Batch         bool
}

// ClassifyLanes maps work onto one or more behavioral lanes (§16). The privacy
// filter lane is always included (it runs before any model use).
func ClassifyLanes(in LaneInput) []RoutingLane {
	lanes := []RoutingLane{LanePrivacyFilter}
	t := strings.ToLower(in.OperationType + " " + in.Title + " " + in.Content)

	// Fast triage is the default lane for background classification/summarization.
	lanes = append(lanes, LaneFastTriage)

	if in.SourceBytes > 40_000 || containsAny(t, "dossier", "long context", "full history") {
		lanes = append(lanes, LaneLongContextDossier)
	}
	if containsAny(t, "draft", "reply", "email", "message", "compose") {
		lanes = append(lanes, LaneDrafting)
	}
	if in.MessyInput || containsAny(t, "ocr", "scan", "pdf", "handwritten") {
		lanes = append(lanes, LaneByteRobust)
	}
	if in.Batch {
		lanes = append(lanes, LaneParallelBatch)
	}
	if in.HighRisk {
		lanes = append(lanes, LaneRecursiveDeepReview)
	}
	// Every operation that can complete needs a verifier lane.
	lanes = append(lanes, LaneVerifier)
	return dedupeLanes(lanes)
}

// QueueForLane maps a lane onto a scheduling queue (§19) — lane affects
// scheduling.
func QueueForLane(lane RoutingLane, highRisk bool) Queue {
	switch lane {
	case LaneVerifier:
		return QueueVerifier
	case LaneParallelBatch, LaneLongContextDossier:
		return QueueBackgroundBatch
	case LaneRecursiveDeepReview:
		if highRisk {
			return QueueApproval
		}
		return QueueBackgroundBatch
	default:
		return QueueBackgroundFast
	}
}

// RouteDecision is the router's truthful selection for a lane.
type RouteDecision struct {
	Lane            RoutingLane `json:"lane"`
	ProviderID      string      `json:"providerId,omitempty"`
	ModelID         string      `json:"modelId,omitempty"`
	Local           bool        `json:"local"`
	Queue           Queue       `json:"queue"`
	Routable        bool        `json:"routable"`
	Reason          string      `json:"reason"`
	CloudRestricted bool        `json:"cloudRestricted"`
	Fallbacks       []string    `json:"fallbacks,omitempty"`
	DecidedAt       time.Time   `json:"decidedAt"`
}

// Router selects a provider/model for a lane from the registry, honoring the
// privacy lane: privacy-unsafe content is restricted to local providers.
type Router struct {
	reg *Registry
}

// NewRouter builds a router over a registry.
func NewRouter(reg *Registry) *Router { return &Router{reg: reg} }

// Route selects a model for the lane. It never fabricates a provider; when
// nothing is usable it returns a non-routable decision with the reason.
func (r *Router) Route(lane RoutingLane, in LaneInput, now time.Time) RouteDecision {
	dec := RouteDecision{Lane: lane, Queue: QueueForLane(lane, in.HighRisk), DecidedAt: now}
	candidates := r.reg.ProfilesForLane(lane)
	if len(candidates) == 0 {
		dec.Reason = "no active model serves this lane"
		return dec
	}
	// A configured, explicitly attested model takes precedence over the
	// deterministic rules fallback. The rule provider remains available when
	// no real local model is usable, but must not mask one that is ready.
	sort.SliceStable(candidates, func(i, j int) bool {
		return !candidates[i].Deterministic && candidates[j].Deterministic
	})
	cloudRestricted := !in.SafeForCloud
	dec.CloudRestricted = cloudRestricted
	for _, prof := range candidates {
		// This auxiliary lane is limited to deterministic and local model
		// intelligence. External provider routing, budget accounting, and the
		// daily maintenance gate belong to HAI's canonical LLM policy router.
		if !prof.Local {
			dec.Fallbacks = append(dec.Fallbacks, prof.Key()+" (skipped: external model intelligence execution is disabled)")
			continue
		}
		if !dec.Routable {
			dec.ProviderID = prof.ProviderID
			dec.ModelID = prof.ModelID
			dec.Local = prof.Local
			dec.Routable = true
			dec.Reason = "selected " + prof.Key() + " for lane " + string(lane)
		} else {
			dec.Fallbacks = append(dec.Fallbacks, prof.Key())
		}
	}
	if !dec.Routable {
		dec.Reason = "no active local model serves this lane; external providers must use HAI's canonical LLM policy router"
	}
	return dec
}

func dedupeLanes(lanes []RoutingLane) []RoutingLane {
	seen := map[RoutingLane]bool{}
	out := make([]RoutingLane, 0, len(lanes))
	for _, l := range lanes {
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	return out
}
