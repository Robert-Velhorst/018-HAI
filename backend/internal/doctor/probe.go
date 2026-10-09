package doctor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"automation-hub-backend/internal/lifecycle"
)

// ProbeFunc performs a live connectivity check against one dependency. It must
// respect ctx and return nil only when the dependency is genuinely usable.
type ProbeFunc func(ctx context.Context) error

// Probe is a live dependency check. Diagnose answers "is the configuration
// plausible"; a Probe answers "is the dependency actually there right now".
// Readiness needs both: a process with a perfect config and an unreachable
// database is not ready, and must not report that it is.
type Probe struct {
	// Name identifies the dependency, e.g. "database.connection".
	Name string
	// Critical marks a dependency the service cannot serve traffic without.
	// A failing critical probe is a SeverityFail, which drives /readyz to 503.
	// A failing non-critical probe is a SeverityWarn: degraded, still serving.
	Critical bool
	// Run performs the check.
	Run ProbeFunc
}

// RunProbes executes probes concurrently, bounding each one by timeout, and
// returns one Check per Probe in the original order. A probe that overruns the
// timeout is reported as unreachable rather than being allowed to hang the
// readiness response — a readiness endpoint that blocks is as useless as one
// that lies.
func RunProbes(ctx context.Context, timeout time.Duration, probes []Probe) []Check {
	checks := make([]Check, len(probes))
	var wg sync.WaitGroup
	for i, probe := range probes {
		wg.Add(1)
		go func(idx int, p Probe) {
			defer wg.Done()
			checks[idx] = runProbe(ctx, timeout, p)
		}(i, probe)
	}
	wg.Wait()
	return checks
}

func runProbe(ctx context.Context, timeout time.Duration, p Probe) Check {
	if p.Run == nil {
		return Check{Name: p.Name, Severity: SeverityWarn, Detail: "no probe configured for this dependency"}
	}

	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Buffer the result so a timed-out caller cannot block completion. API
	// ownership still tracks a probe that ignores cancellation through its return.
	done := make(chan error, 1)
	started := time.Now()
	if !lifecycle.Go(probeCtx, "readiness-probe", func() { done <- p.Run(probeCtx) }) {
		return Check{Name: p.Name, Severity: SeverityFail, Detail: "dependency probe canceled during runtime shutdown"}
	}

	var err error
	select {
	case err = <-done:
	case <-probeCtx.Done():
		err = fmt.Errorf("timed out after %s", timeout)
	}
	elapsed := time.Since(started).Round(time.Millisecond)

	if err != nil {
		severity := SeverityWarn
		if p.Critical {
			severity = SeverityFail
		}
		return Check{
			Name:     p.Name,
			Severity: severity,
			Detail:   fmt.Sprintf("unreachable after %s: %v", elapsed, err),
		}
	}

	return Check{
		Name:     p.Name,
		Severity: SeverityOK,
		Detail:   fmt.Sprintf("reachable in %s", elapsed),
	}
}

// Merge appends checks to the report, so a caller can combine the static
// configuration diagnosis with live dependency probes into one answer.
func (r Report) Merge(checks ...Check) Report {
	merged := make([]Check, 0, len(r.Checks)+len(checks))
	merged = append(merged, r.Checks...)
	merged = append(merged, checks...)
	return Report{Checks: merged}
}
