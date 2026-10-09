// hai-dsh-bridge is a deliberately narrow Windows host worker for the
// DeepSeek Harness adapter. It has no listener: it only polls HAI's separate
// loopback-only gateway and executes leased, already-approved jobs.
package main

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
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxOutputBytes                = 16 * 1024
	pollInterval                  = 2 * time.Second
	executionConfirmationInterval = 2 * time.Second
	executionConfirmationTimeout  = 2 * time.Second
)

var (
	errEmergencyStop                 = errors.New("host runtime execution is blocked by emergency stop")
	errStaleLease                    = errors.New("host runtime lease is no longer valid")
	errCancellationRequested         = errors.New("host runtime cancellation was requested")
	errFatalSupervisorState          = errors.New("bridge stopped because process-tree termination could not be verified")
	errExecutionIsolationUnavailable = errors.New("host runtime execution is blocked because operating-system isolation is unavailable")
	errDSHIsolationUnavailable       = errors.New("HAI DSH bridge execution is disabled: TLS peer pinning, a verified OS-enforced least-privilege sandbox, and an acknowledged server-to-Windows process-start protocol that orders Resume against emergency stop are not implemented")
)

type config struct {
	baseURL      *url.URL
	token        string
	executable   string
	version      string
	workspace    string
	stateDir     string
	workspaceKey string
	timeout      time.Duration
	envAllow     []string
}

type lease struct {
	Job struct {
		ID           string `json:"id"`
		RuntimeID    string `json:"runtimeId"`
		Prompt       string `json:"prompt"`
		WorkspaceKey string `json:"workspaceKey"`
	} `json:"job"`
	Token          string `json:"leaseToken"`
	WorkerID       string `json:"workerId"`
	ApprovalDigest string `json:"approvalDigest"`
	StopRevision   uint64 `json:"stopRevision"`
}

type completion struct {
	LeaseToken            string `json:"leaseToken"`
	ExitCode              int    `json:"exitCode"`
	Output                string `json:"output"`
	Error                 string `json:"error"`
	CancellationRequested bool   `json:"cancellationRequested,omitempty"`
	TerminationVerified   bool   `json:"terminationVerified,omitempty"`
	processTreeTerminated bool
	fatal                 bool
}

type confirmRequest struct {
	LeaseToken string `json:"leaseToken"`
}

func main() {
	configuration, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "hai-dsh-bridge:", err)
		os.Exit(2)
	}
	if err := verifyVersion(context.Background(), configuration); err != nil {
		fmt.Fprintln(os.Stderr, "hai-dsh-bridge:", err)
		os.Exit(2)
	}
	if err := run(context.Background(), configuration); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "hai-dsh-bridge:", err)
		os.Exit(1)
	}
}

func loadConfig() (config, error) {
	return config{}, errDSHIsolationUnavailable
}

func run(ctx context.Context, configuration config) error {
	return errDSHIsolationUnavailable
}

type leaseExecutor func(context.Context, *http.Client, config, lease) completion

func runWithExecutor(ctx context.Context, client *http.Client, configuration config, execute leaseExecutor) error {
	return errDSHIsolationUnavailable
}

func confirmLease(ctx context.Context, client *http.Client, configuration config, leased lease) error {
	payload, err := json.Marshal(confirmRequest{LeaseToken: leased.Token})
	if err != nil {
		return err
	}
	request, err := newRequest(ctx, configuration, http.MethodPost, "/api/v1/host-runtime/leases/"+url.PathEscape(leased.Job.ID)+"/confirm", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := doBridgeRequest(client, request)
	if err != nil {
		return fmt.Errorf("confirm host runtime lease: %w", err)
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusNoContent:
		return nil
	case http.StatusLocked:
		code, reason := gatewayResponseDetails(response)
		if code == "execution_isolation_unavailable" {
			if reason == "" {
				reason = errExecutionIsolationUnavailable.Error()
			}
			return fmt.Errorf("%w (HTTP %d, code %s): %s", errExecutionIsolationUnavailable, response.StatusCode, code, reason)
		}
		return errEmergencyStop
	case http.StatusConflict:
		if gatewayResponseCode(response) == "cancellation_requested" {
			return errCancellationRequested
		}
		return errStaleLease
	default:
		return fmt.Errorf("confirm host runtime lease: gateway returned %s", response.Status)
	}
}

// executeWithLeaseMonitor keeps the final execution gate live while DSH is
// running. A lease can become unsafe after the process starts when the owner
// activates emergency stop, so a launch-only check is insufficient. Any
// failed confirmation stops the local process rather than letting host work
// continue without HAI's current approval state.
func executeWithLeaseMonitor(parent context.Context, client *http.Client, configuration config, leased lease) completion {
	return completion{ExitCode: -1, Error: errDSHIsolationUnavailable.Error()}
}

func monitorExecution(parent context.Context, interval time.Duration, confirm func(context.Context) error, launch func(context.Context, func(context.Context) error, func()) completion) completion {
	return monitorExecutionWithConfirmTimeout(parent, interval, executionConfirmationTimeout, confirm, launch)
}

func monitorExecutionWithConfirmTimeout(parent context.Context, interval time.Duration, confirmationTimeout time.Duration, confirm func(context.Context) error, launch func(context.Context, func(context.Context) error, func()) completion) completion {
	if interval <= 0 {
		interval = executionConfirmationInterval
	}
	if confirmationTimeout <= 0 {
		confirmationTimeout = executionConfirmationTimeout
	}
	executionContext, cancel := context.WithCancel(parent)
	defer cancel()
	completed := make(chan completion, 1)
	started := make(chan struct{})
	var startedOnce sync.Once
	onStarted := func() { startedOnce.Do(func() { close(started) }) }
	reconfirm := func(ctx context.Context) error {
		confirmationContext, confirmationCancel := context.WithTimeout(ctx, confirmationTimeout)
		defer confirmationCancel()
		return confirm(confirmationContext)
	}
	go func() {
		completed <- launch(executionContext, reconfirm, onStarted)
	}()
	select {
	case result := <-completed:
		return result
	case <-parent.Done():
		cancel()
		return <-completed
	case <-started:
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case result := <-completed:
			return result
		case <-parent.Done():
			cancel()
			return <-completed
		case <-ticker.C:
			err := reconfirm(parent)
			if err == nil {
				continue
			}
			cancel()
			result := <-completed
			result.ExitCode = -1
			reason := executionStoppedReason(err)
			if errors.Is(err, errCancellationRequested) {
				result.CancellationRequested = true
				result.TerminationVerified = result.processTreeTerminated
			}
			if strings.TrimSpace(result.Error) != "" {
				result.Error = reason + "; " + result.Error
			} else {
				result.Error = reason
			}
			return result
		}
	}
}

func executionStoppedReason(err error) string {
	switch {
	case errors.Is(err, errEmergencyStop):
		return "DeepSeek Harness execution was stopped because HAI emergency stop is active"
	case errors.Is(err, errStaleLease):
		return "DeepSeek Harness execution was stopped because its HAI execution lease is no longer valid"
	case errors.Is(err, errCancellationRequested):
		return "DeepSeek Harness execution was stopped because an owner requested cancellation"
	case errors.Is(err, errExecutionIsolationUnavailable):
		return "DeepSeek Harness execution was blocked because operating-system isolation is unavailable: " + bridgeError(err)
	default:
		return "DeepSeek Harness execution was stopped because HAI could not reconfirm the execution lease: " + bridgeError(err)
	}
}

func requestLease(ctx context.Context, client *http.Client, configuration config) (lease, bool, error) {
	return lease{}, false, errDSHIsolationUnavailable
}

func submitCompletion(ctx context.Context, client *http.Client, configuration config, leased lease, result completion) error {
	status, code, err := submitCompletionOnce(ctx, client, configuration, leased, result)
	if err != nil {
		return err
	}
	if status == http.StatusOK {
		return nil
	}
	if status != http.StatusConflict || code != "cancellation_requested" || result.CancellationRequested {
		return fmt.Errorf("submit host runtime completion: gateway returned HTTP %d", status)
	}

	// A stop can race the worker's final completion request. Only the process
	// supervisor's explicit result, or a pre-launch path with no child process,
	// can authorize a positive termination acknowledgment.
	result.CancellationRequested = true
	if !result.processTreeTerminated {
		result.TerminationVerified = false
		result.ExitCode = -1
		result.Error = appendReason(result.Error, "HAI Stop is pending but process-tree termination could not be verified")
	} else {
		result.TerminationVerified = true
		result.ExitCode = -1
		result.Error = appendReason(result.Error, "HAI Stop was acknowledged after the contained process tree was confirmed empty; task effects were not rolled back")
	}
	status, _, err = submitCompletionOnce(ctx, client, configuration, leased, result)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("submit host runtime cancellation acknowledgment: gateway returned HTTP %d", status)
	}
	if !result.TerminationVerified {
		return fmt.Errorf("%w: HAI Stop remains indeterminate because the contained process tree was not confirmed empty", errFatalSupervisorState)
	}
	return nil
}

func submitCompletionOnce(ctx context.Context, client *http.Client, configuration config, leased lease, result completion) (int, string, error) {
	payload, err := json.Marshal(result)
	if err != nil {
		return 0, "", err
	}
	request, err := newRequest(ctx, configuration, http.MethodPost, "/api/v1/host-runtime/leases/"+url.PathEscape(leased.Job.ID)+"/complete", bytes.NewReader(payload))
	if err != nil {
		return 0, "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := doBridgeRequest(client, request)
	if err != nil {
		return 0, "", fmt.Errorf("submit host runtime completion: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusOK {
		return response.StatusCode, "", nil
	}
	return response.StatusCode, gatewayResponseCode(response), nil
}

func gatewayResponseCode(response *http.Response) string {
	code, _ := gatewayResponseDetails(response)
	return code
}

func gatewayResponseDetails(response *http.Response) (string, string) {
	if response == nil || response.Body == nil {
		return "", ""
	}
	var payload struct {
		Code   string `json:"code"`
		Error  string `json:"error"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&payload); err != nil {
		return "", ""
	}
	return payload.Code, firstNonEmpty(payload.Reason, payload.Error)
}

func appendReason(existing, reason string) string {
	if strings.TrimSpace(existing) == "" {
		return reason
	}
	return existing + "; " + reason
}

func bridgeError(err error) string {
	message := strings.TrimSpace(err.Error())
	if len(message) > 256 {
		message = strings.TrimSpace(message[:256])
	}
	if message == "" {
		return "confirmation failed"
	}
	return message
}

func newRequest(ctx context.Context, configuration config, method, requestPath string, body io.Reader) (*http.Request, error) {
	endpoint := configuration.baseURL.ResolveReference(&url.URL{Path: requestPath})
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+configuration.token)
	return request, nil
}

func doBridgeRequest(client *http.Client, request *http.Request) (*http.Response, error) {
	if client == nil {
		return nil, errors.New("bridge HTTP client is unavailable")
	}
	// The bridge bearer token is scoped to the configured gateway. Do not let a
	// gateway response redirect it to another local service or host. Build a
	// fresh client from exported settings rather than copying a possibly-used
	// http.Client, whose internal synchronization state is not copy-safe.
	safeClient := &http.Client{
		Transport: client.Transport,
		Jar:       client.Jar,
		Timeout:   client.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return safeClient.Do(request)
}

func execute(parent context.Context, configuration config, prompt string, revalidate func(context.Context) error, onStarted func()) completion {
	return completion{ExitCode: -1, Error: errDSHIsolationUnavailable.Error()}
}

func completionFromProcessResult(ctx context.Context, result processResult, err error) completion {
	// An untyped start/setup failure does not prove that a partially created
	// child was cleaned up. Only successful wait, bounded output-drain failure
	// after job-empty verification, or an explicit stop/lease result from the
	// supervisor may authorize a positive termination acknowledgment.
	processTreeTerminated := result.processTreeTerminated || err == nil || errors.Is(err, errOutputCaptureIncomplete) ||
		errors.Is(err, errCancellationRequested) || errors.Is(err, errEmergencyStop) || errors.Is(err, errStaleLease)
	if errors.Is(err, errProcessTreeTerminationUnverified) {
		processTreeTerminated = false
	}
	if err != nil {
		switch {
		case errors.Is(err, errProcessTreeTerminationUnverified):
			return completion{ExitCode: -1, Output: result.Stdout, Error: "DeepSeek Harness process supervisor failed: " + bridgeError(err), CancellationRequested: errors.Is(err, errCancellationRequested), processTreeTerminated: false, fatal: true}
		case errors.Is(err, errOutputCaptureIncomplete):
			message := "DeepSeek Harness output capture was incomplete: " + bridgeError(err)
			if diagnostic := strings.TrimSpace(result.Stderr); diagnostic != "" {
				message = appendReason(message, "Captured stderr (bounded): "+diagnostic)
			}
			return completion{ExitCode: -1, Output: result.Stdout, Error: message, processTreeTerminated: processTreeTerminated}
		case errors.Is(err, errCancellationRequested):
			return completion{ExitCode: -1, Output: result.Stdout, Error: executionStoppedReason(err), CancellationRequested: true, TerminationVerified: processTreeTerminated, processTreeTerminated: processTreeTerminated}
		case errors.Is(err, errEmergencyStop), errors.Is(err, errStaleLease):
			return completion{ExitCode: -1, Output: result.Stdout, Error: executionStoppedReason(err), processTreeTerminated: processTreeTerminated}
		case errors.Is(ctx.Err(), context.DeadlineExceeded) && errors.Is(err, context.DeadlineExceeded):
			return completion{ExitCode: -1, Output: result.Stdout, Error: "DeepSeek Harness execution exceeded the configured timeout and was stopped", processTreeTerminated: processTreeTerminated}
		default:
			return completion{ExitCode: -1, Output: result.Stdout, Error: "DeepSeek Harness process supervisor failed: " + bridgeError(err), processTreeTerminated: processTreeTerminated}
		}
	}
	if result.ExitCode == 0 {
		return completion{ExitCode: 0, Output: result.Stdout, processTreeTerminated: true}
	}
	diagnostic := strings.TrimSpace(result.Stderr)
	if diagnostic == "" {
		diagnostic = "DeepSeek Harness process failed without diagnostic output"
	}
	return completion{ExitCode: result.ExitCode, Output: result.Stdout, Error: diagnostic, processTreeTerminated: true}
}

func verifyVersion(ctx context.Context, configuration config) error {
	return errDSHIsolationUnavailable
}

var dshVersionTokenPattern = regexp.MustCompile(`\Av?((?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?)\z`)

func parseDshVersionToken(value string) (string, bool) {
	match := dshVersionTokenPattern.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) != 2 {
		return "", false
	}
	return match[1], true
}

func verifyVersionOutput(expected string, output []byte) error {
	expectedToken, ok := parseDshVersionToken(expected)
	if !ok {
		return errors.New("DeepSeek Harness pinned version is invalid")
	}
	actualToken, ok := parseDshVersionToken(string(output))
	if !ok {
		return errors.New("DeepSeek Harness version output is invalid")
	}
	if actualToken != expectedToken {
		return fmt.Errorf("DeepSeek Harness version mismatch: expected %q", expectedToken)
	}
	return nil
}

func validatedWorkspace(workspace, stateDir string) (string, string, error) {
	workspace, err := filepath.EvalSymlinks(filepath.Clean(workspace))
	if err != nil {
		return "", "", errors.New("DeepSeek Harness workspace is not an accessible directory")
	}
	info, err := os.Stat(workspace)
	if err != nil || !info.IsDir() {
		return "", "", errors.New("DeepSeek Harness workspace is not an accessible directory")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return "", "", errors.New("DeepSeek Harness state directory cannot be created")
	}
	stateDir, err = filepath.EvalSymlinks(filepath.Clean(stateDir))
	if err != nil {
		return "", "", errors.New("DeepSeek Harness state directory is not accessible")
	}
	relative, err := filepath.Rel(workspace, stateDir)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return "", "", errors.New("DeepSeek Harness state directory must stay inside the configured workspace")
	}
	return workspace, stateDir, nil
}

func invalidPrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || len(prompt) > 50*1024 || strings.HasPrefix(prompt, "-") || prompt == "web" || prompt == "plugin" || strings.ContainsRune(prompt, 0) {
		return "leased task prompt violates the DeepSeek Harness execution policy"
	}
	return ""
}

func isLoopbackURL(endpoint *url.URL) bool {
	if endpoint == nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return false
	}
	host := endpoint.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	return net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func boundedSeconds(value string, fallback int) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds < 1 || seconds > 900 {
		seconds = fallback
	}
	return time.Duration(seconds) * time.Second
}

func csvValues(value string) []string {
	result := []string{}
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" && validEnvKey(item) {
			result = append(result, item)
		}
	}
	return result
}

func safeEnvironment(allowlist []string, fixed map[string]string) []string {
	// The host worker has a dedicated DSH_HOME inside its approved workspace.
	// Do not pass Windows profile locations through to the child process: they
	// can contain browser sessions, SSH material, cloud credentials, and other
	// unrelated user state that the approved job was never authorized to use.
	allowed := map[string]bool{
		"COMSPEC": true, "PATH": true, "PATHEXT": true, "SYSTEMROOT": true,
		"TEMP": true, "TMP": true, "WINDIR": true,
	}
	for _, key := range allowlist {
		allowed[strings.ToUpper(key)] = true
	}
	fixedValues := make(map[string]string, len(fixed))
	for key := range fixed {
		normalized := strings.ToUpper(key)
		allowed[normalized] = true
		fixedValues[normalized] = fixed[key]
	}
	values := make(map[string]string, len(allowed))
	for _, pair := range os.Environ() {
		key, _, found := strings.Cut(pair, "=")
		normalized := strings.ToUpper(key)
		if found && allowed[normalized] {
			// Fixed runtime values must not be shadowed by an ambient value.
			if _, fixed := fixedValues[normalized]; !fixed {
				values[normalized] = pair
			}
		}
	}
	for key, value := range fixedValues {
		values[key] = key + "=" + value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, values[key])
	}
	return result
}

func validEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for index, character := range key {
		letter := character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z'
		digit := character >= '0' && character <= '9'
		if letter || index > 0 && (digit || character == '_') {
			continue
		}
		return false
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	remaining int
	truncated bool
}

func (b *limitedBuffer) Write(value []byte) (int, error) {
	count := len(value)
	if count > b.remaining {
		count = max(b.remaining, 0)
		b.truncated = true
	}
	if count > 0 {
		_, _ = b.buffer.Write(value[:count])
		b.remaining -= count
	}
	return len(value), nil
}

func (b *limitedBuffer) String() string { return strings.TrimSpace(b.buffer.String()) }
