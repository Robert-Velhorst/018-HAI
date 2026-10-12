package openclawmaintenance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"automation-hub-backend/internal/lifecycle"
	"github.com/google/uuid"
)

// PullClient speaks only to the local HAI maintenance API; it never follows a
// redirect or sends internal credentials through an environment-configured proxy.
type PullClient struct {
	base, token, key string
	http             *http.Client
	permitInterval   time.Duration
	cancelWait       time.Duration
}

const (
	defaultPermitInterval = 2 * time.Second
	permitRequestTimeout  = 5 * time.Second
	defaultCancelWait     = 10 * time.Second
)

var ErrExecutionCancellationUnconfirmed = errors.New("maintenance execution did not confirm cancellation")
var ErrWorkerStopAfterCancellation = errors.New("maintenance worker must stop after interrupted installation")

func NewPullClient(base, token, backendKey string) (*PullClient, error) {
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("maintenance requires a loopback HTTP backend URL without credentials, path, query or fragment")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("maintenance backend must use a loopback IP address")
	}
	if !validCredential(token) || !validCredential(backendKey) || token == backendKey {
		return nil, fmt.Errorf("separate maintenance and backend credentials of at least 32 characters are required")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &PullClient{base: strings.TrimRight(base, "/"), token: token, key: backendKey, http: &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, permitInterval: defaultPermitInterval, cancelWait: defaultCancelWait}, nil
}

func validCredential(value string) bool {
	if len(value) < 32 {
		return false
	}
	for _, c := range value {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}

func (c *PullClient) call(ctx context.Context, path string, body any, capabilityHeader string) (int, []byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return 0, nil, fmt.Errorf("invalid maintenance request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v1/openclaw-maintenance-worker"+path, bytes.NewReader(data))
	if err != nil {
		return 0, nil, fmt.Errorf("invalid maintenance request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-HAI-Backend-Key", c.key)
	req.Header.Set("X-HAI-Worker-Capability", capabilityHeader)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("maintenance backend request failed")
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 16385))
	if err != nil || len(b) > 16384 {
		return res.StatusCode, nil, fmt.Errorf("invalid maintenance response")
	}
	return res.StatusCode, b, nil
}

// PollOnce confirms a single lease and reports one execution. Only delivery of
// the exact receipt is retried; execution and authorization are never replayed.
func (c *PullClient) PollOnce(ctx context.Context, execute func(context.Context, Job, func() error) (Report, error)) error {
	return c.PollOnceWithCapabilities(ctx, unsupportedWorkerCapability(), execute)
}

// PollOnceWithCapabilities renews the authenticated capability observation on
// every worker request, including live permit checks during an installation.
func (c *PullClient) PollOnceWithCapabilities(ctx context.Context, capability WorkerCapabilityHandshake, execute func(context.Context, Job, func() error) (Report, error)) error {
	if execute == nil {
		return fmt.Errorf("maintenance executor is required")
	}
	capabilityJSON, err := json.Marshal(capability)
	if err != nil || len(capabilityJSON) > 1024 {
		return fmt.Errorf("invalid worker capability report")
	}
	capabilityHeader := string(capabilityJSON)
	status, data, err := c.call(ctx, "/leases", nil, capabilityHeader)
	if err != nil {
		return err
	}
	if status == 204 {
		return nil
	}
	if status != 200 {
		return fmt.Errorf("maintenance lease unavailable (HTTP %d)", status)
	}
	var lease Lease
	if json.Unmarshal(data, &lease) != nil || !validPulledLease(lease) {
		return fmt.Errorf("invalid maintenance lease")
	}
	path := "/leases/" + lease.Job.ID
	auth := map[string]string{"leaseToken": lease.Token}
	status, _, err = c.call(ctx, path+"/confirm", auth, capabilityHeader)
	if err != nil || status != 204 {
		return fmt.Errorf("maintenance lease confirmation failed; execution was not started")
	}
	bounded, done := context.WithTimeout(ctx, 15*time.Minute)
	permitWithContext := func(permitContext context.Context) error {
		code, _, err := c.call(permitContext, path+"/permit", auth, capabilityHeader)
		if err != nil || code != 204 {
			return fmt.Errorf("update permission expired or was revoked")
		}
		return nil
	}
	permit := func() error { return permitWithContext(bounded) }
	var report Report
	var runErr error
	if lease.Job.Kind == "apply" {
		report, runErr = executeWithPermitMonitor(bounded, lease.Job, c.permitInterval, c.cancelWait, permitWithContext, func(runContext context.Context) (Report, error) {
			runPermit := func() error { return permitWithContext(runContext) }
			return execute(runContext, lease.Job, runPermit)
		})
	} else {
		report, runErr = execute(bounded, lease.Job, permit)
	}
	done()
	cancellationUnconfirmed := errors.Is(runErr, ErrExecutionCancellationUnconfirmed)
	workerMustStop := errors.Is(runErr, ErrWorkerStopAfterCancellation)
	if runErr != nil {
		if lease.Job.Kind == "check" {
			report.Outcome = "unavailable"
		} else {
			report.Outcome = "needs_review"
		}
	}
	if report.Evidence == "" {
		report.Evidence = strings.Repeat("0", 64)
	}
	if ValidateReport(lease.Job, report) != nil {
		// A malformed executor result is not a successful install. Persist an honest
		// unknown outcome instead of forwarding raw output or leaving a silent gap.
		outcome := "needs_review"
		if lease.Job.Kind == "check" {
			outcome = "unavailable"
		}
		report = Report{Evidence: strings.Repeat("0", 64), Outcome: outcome}
		runErr = errors.Join(runErr, fmt.Errorf("invalid executor report"))
	}
	receipt := struct {
		Token  string `json:"leaseToken"`
		Report Report `json:"report"`
	}{lease.Token, report}
	// Execution may have been cancelled, but its final review receipt must still
	// reach HAI so the lease does not remain ambiguous until a later expiry sweep.
	receiptContext, cancelReceipt := context.WithTimeout(context.WithoutCancel(ctx), permitRequestTimeout)
	defer cancelReceipt()
	for attempt := 0; attempt < 3; attempt++ {
		status, data, err = c.call(receiptContext, path+"/complete", receipt, capabilityHeader)
		if err == nil && status == http.StatusOK {
			var acknowledgement ReceiptAcknowledgement
			if json.Unmarshal(data, &acknowledgement) != nil || (acknowledgement.Status != "completed" && acknowledgement.Status != "failed" && acknowledgement.Status != "needs_review" && acknowledgement.Status != "reviewed") {
				continue
			}
			if acknowledgement.Status != "completed" {
				if workerMustStop {
					return cancellationStopError(cancellationUnconfirmed, "HAI recorded the update as %s for owner review; inspect the recorded outcome in HAI", acknowledgement.Status)
				}
				return fmt.Errorf("maintenance receipt was recorded as %s; inspect the recorded outcome in HAI", acknowledgement.Status)
			}
			if workerMustStop {
				return cancellationStopError(cancellationUnconfirmed, "HAI recorded the update for owner review")
			}
			if runErr != nil || report.Outcome != "ok" {
				return fmt.Errorf("maintenance did not succeed; inspect the recorded outcome in HAI")
			}
			return nil
		}
		if err == nil && status >= 400 && status < 500 {
			break
		}
	}
	if workerMustStop {
		return cancellationStopError(cancellationUnconfirmed, "HAI could not confirm the review receipt; inspect the job before any further update")
	}
	return fmt.Errorf("maintenance receipt is unconfirmed; do not repeat installation, inspect the job in HAI")
}

func cancellationStopError(unconfirmed bool, format string, args ...any) error {
	message := fmt.Sprintf(format, args...)
	if unconfirmed {
		return errors.Join(ErrWorkerStopAfterCancellation, fmt.Errorf("%w; %s", ErrExecutionCancellationUnconfirmed, message))
	}
	return fmt.Errorf("%w; %s", ErrWorkerStopAfterCancellation, message)
}

type maintenanceExecutionResult struct {
	report Report
	err    error
}

// executeWithPermitMonitor keeps update authorization live after installation
// starts. A revoked or unavailable permit cancels the installer and records
// the outcome for owner review; it never retries the installation.
func executeWithPermitMonitor(
	parent context.Context,
	job Job,
	interval time.Duration,
	cancelWait time.Duration,
	permit func(context.Context) error,
	execute func(context.Context) (Report, error),
) (Report, error) {
	if parent == nil || job.Kind != "apply" || permit == nil || execute == nil {
		return Report{}, fmt.Errorf("live update authorization is unavailable")
	}
	if interval <= 0 {
		interval = defaultPermitInterval
	}
	if cancelWait <= 0 {
		cancelWait = defaultCancelWait
	}
	runContext, cancelRun := context.WithCancel(parent)
	defer cancelRun()
	completed := make(chan maintenanceExecutionResult, 1)
	if !lifecycle.Go(runContext, "openclaw-maintenance-installer", func() {
		report, err := execute(runContext)
		completed <- maintenanceExecutionResult{report: report, err: err}
	}) {
		return Report{}, errors.Join(ErrWorkerStopAfterCancellation, context.Canceled)
	}
	cancelAndCollect := func(cause error) (Report, error) {
		cancelRun()
		timer := time.NewTimer(cancelWait)
		defer timer.Stop()
		select {
		case result := <-completed:
			return result.report, errors.Join(ErrWorkerStopAfterCancellation, result.err, cause)
		case <-timer.C:
			return Report{Evidence: job.Evidence, Outcome: "needs_review"}, errors.Join(ErrWorkerStopAfterCancellation, ErrExecutionCancellationUnconfirmed, cause)
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case result := <-completed:
			if parent.Err() != nil || errors.Is(result.err, context.Canceled) || errors.Is(result.err, context.DeadlineExceeded) {
				return result.report, errors.Join(ErrWorkerStopAfterCancellation, result.err, parent.Err())
			}
			return result.report, result.err
		case <-parent.Done():
			return cancelAndCollect(parent.Err())
		case <-ticker.C:
			checkContext, cancelCheck := context.WithTimeout(parent, permitRequestTimeout)
			err := permit(checkContext)
			cancelCheck()
			if err == nil {
				continue
			}
			return cancelAndCollect(err)
		}
	}
}

func validPulledLease(l Lease) bool {
	id, err := uuid.Parse(l.Job.ID)
	if err != nil || id.String() != l.Job.ID || !ValidTarget(l.Job.Target) || l.Job.Status != "leased" || l.Job.LeaseUntil == nil || !l.Job.LeaseUntil.After(time.Now()) {
		return false
	}
	if len(l.Token) != 64 || strings.IndexFunc(l.Token, func(r rune) bool {
		return !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f'))
	}) >= 0 {
		return false
	}
	switch l.Job.Kind {
	case "check":
		return true
	case "apply":
		return ValidVersion(l.Job.Version) && len(l.Job.Evidence) == 64 && strings.Trim(l.Job.Evidence, "0123456789abcdef") == ""
	default:
		return false
	}
}
