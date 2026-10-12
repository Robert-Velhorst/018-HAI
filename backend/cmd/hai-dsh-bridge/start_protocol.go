package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

var (
	errStartOutcomeReview = errors.New("host runtime start outcome requires review; the job must not be retried")
	errStartIntentReplay  = errors.New("host runtime start intent was consumed by a different attempt")
)

type startProtocolRequest struct {
	LeaseToken     string    `json:"leaseToken"`
	IntentID       uuid.UUID `json:"intentId"`
	ApprovalDigest string    `json:"approvalDigest"`
	StopRevision   uint64    `json:"stopRevision"`
}

type startIntentReceipt struct {
	JobID          uuid.UUID `json:"jobId"`
	IntentID       uuid.UUID `json:"intentId"`
	WorkerID       string    `json:"workerId"`
	ApprovalDigest string    `json:"approvalDigest"`
	StopRevision   uint64    `json:"stopRevision"`
}

func newStartProtocolRequest(leased lease) (startProtocolRequest, error) {
	_, err := uuid.Parse(strings.TrimSpace(leased.Job.ID))
	if err != nil || strings.TrimSpace(leased.Token) == "" || strings.TrimSpace(leased.WorkerID) == "" || !isSHA256Digest(leased.ApprovalDigest) {
		return startProtocolRequest{}, errors.New("leased host job is missing its immutable start binding")
	}
	return startProtocolRequest{
		LeaseToken: leased.Token, IntentID: uuid.New(), ApprovalDigest: leased.ApprovalDigest,
		StopRevision: leased.StopRevision,
	}, nil
}

func isSHA256Digest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9') && !(character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func beginStartIntent(ctx context.Context, client *http.Client, configuration config, leased lease, binding startProtocolRequest) (*startIntentReceipt, error) {
	jobID, err := validateStartRequest(leased, binding)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(binding)
	if err != nil {
		return nil, err
	}
	request, err := newRequest(ctx, configuration, http.MethodPost, "/api/v1/host-runtime/leases/"+url.PathEscape(jobID.String())+"/start-intent", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := doBridgeRequest(client, request)
	if err != nil {
		return nil, fmt.Errorf("begin host runtime start intent: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, startProtocolResponseError(response)
	}
	var receipt startIntentReceipt
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&receipt); err != nil {
		return nil, errors.New("host runtime returned an invalid start-intent receipt")
	}
	if receipt.JobID != jobID || receipt.IntentID != binding.IntentID || receipt.WorkerID != leased.WorkerID ||
		receipt.ApprovalDigest != binding.ApprovalDigest || receipt.StopRevision != binding.StopRevision {
		return nil, errStartOutcomeReview
	}
	return &receipt, nil
}

func acknowledgeActualStart(ctx context.Context, client *http.Client, configuration config, leased lease, binding startProtocolRequest) error {
	if err := postStartProtocol(ctx, client, configuration, leased, binding, "start-ack"); err != nil {
		// Resume may already have returned. Reconcile the exact same binding; if
		// the ack did not commit, the server quarantines it instead of retrying.
		if reportStartOutcomeUnknown(ctx, client, configuration, leased, binding) == nil {
			return nil
		}
		return fmt.Errorf("%w: actual-start acknowledgment could not be confirmed", errStartOutcomeReview)
	}
	return nil
}

func reportStartOutcomeUnknown(ctx context.Context, client *http.Client, configuration config, leased lease, binding startProtocolRequest) error {
	return postStartProtocol(ctx, client, configuration, leased, binding, "start-unknown")
}

func postStartProtocol(ctx context.Context, client *http.Client, configuration config, leased lease, binding startProtocolRequest, endpoint string) error {
	jobID, err := validateStartRequest(leased, binding)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(binding)
	if err != nil {
		return err
	}
	request, err := newRequest(ctx, configuration, http.MethodPost, "/api/v1/host-runtime/leases/"+url.PathEscape(jobID.String())+"/"+endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := doBridgeRequest(client, request)
	if err != nil {
		return fmt.Errorf("report host runtime start protocol: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return nil
	}
	return startProtocolResponseError(response)
}

func validateStartRequest(leased lease, binding startProtocolRequest) (uuid.UUID, error) {
	jobID, err := uuid.Parse(strings.TrimSpace(leased.Job.ID))
	if err != nil || strings.TrimSpace(leased.Token) == "" || strings.TrimSpace(leased.WorkerID) == "" ||
		binding.LeaseToken != leased.Token || binding.IntentID == uuid.Nil || !isSHA256Digest(binding.ApprovalDigest) ||
		binding.ApprovalDigest != leased.ApprovalDigest || binding.StopRevision != leased.StopRevision {
		return uuid.Nil, errors.New("host runtime start request does not match the leased job")
	}
	return jobID, nil
}

func startProtocolResponseError(response *http.Response) error {
	code, reason := gatewayResponseDetails(response)
	switch code {
	case "start_outcome_review":
		return errStartOutcomeReview
	case "start_intent_replay":
		return errStartIntentReplay
	case "execution_isolation_unavailable":
		return fmt.Errorf("%w: %s", errExecutionIsolationUnavailable, firstNonEmpty(reason, "execution is disabled"))
	case "emergency_stopped":
		return errEmergencyStop
	case "stale_lease":
		return errStaleLease
	case "cancellation_requested":
		return errCancellationRequested
	default:
		return fmt.Errorf("host runtime start protocol returned HTTP %d", response.StatusCode)
	}
}
