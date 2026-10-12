package frameworkregistry

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/agentcoordination"

	"github.com/google/uuid"
)

func TestAgentTeamLifecycleUsesCanonicalCoordinationAndNeverGrantsAuthority(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)
	repo := NewMemoryAgentTeamRepository()
	service := newAgentTeamService(repo, func() time.Time { return now }, deterministicTeamIDs("lifecycle"))
	team := createActiveTestTeam(t, service, "robert", now)

	correlationID := deterministicUUID("consensus-correlation")
	first := decisionMessage(t, team, now, correlationID, team.Members[0], team.Members[1], TeamVoteSupport, "Use the bounded plan.", "decision-one")
	second := decisionMessage(t, team, now, correlationID, team.Members[1], team.Members[0], TeamVoteSupport, "Use the bounded plan.", "decision-two")
	if _, created, err := service.StoreCoordinationMessage("robert", team.ID, team.Version, first); err != nil || !created {
		t.Fatalf("StoreCoordinationMessage(first) = created %v, err %v", created, err)
	}
	if _, created, err := service.StoreCoordinationMessage("robert", team.ID, team.Version, second); err != nil || !created {
		t.Fatalf("StoreCoordinationMessage(second) = created %v, err %v", created, err)
	}
	storedAgain, created, err := service.StoreCoordinationMessage("robert", team.ID, team.Version, first)
	if err != nil || created || storedAgain.ID != first.ID {
		t.Fatalf("idempotent message retry = %#v, created %v, err %v", storedAgain, created, err)
	}
	conflictingRetry := first
	conflictingRetry.Payload.Data = json.RawMessage(`{"position":"oppose","recommendation":"Use another plan."}`)
	conflictingRetry.PayloadDigest, err = agentcoordination.ComputeMessageDigest(conflictingRetry)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.StoreCoordinationMessage("robert", team.ID, team.Version, conflictingRetry); !errors.Is(err, ErrAgentTeamIdempotencyConflict) {
		t.Fatalf("expected message idempotency conflict, got %v", err)
	}

	outcomeKey := deterministicUUID("consensus-outcome-key")
	outcome, created, err := service.RecordConsensus("robert", team.ID, team.Version, correlationID, outcomeKey, "Select the bounded plan")
	if err != nil || !created {
		t.Fatalf("RecordConsensus = created %v, err %v", created, err)
	}
	if outcome.Status != TeamOutcomeReached || outcome.SupportCount != 2 || outcome.Recommendation != "Use the bounded plan." {
		t.Fatalf("unexpected consensus outcome: %#v", outcome)
	}
	if !outcome.AdvisoryOnly || outcome.GrantsExecutionAuthority || !outcome.ExecutionAuthorizationRequired {
		t.Fatalf("consensus outcome crossed authority boundary: %#v", outcome)
	}
	retried, created, err := service.RecordConsensus("robert", team.ID, team.Version, correlationID, outcomeKey, "Select the bounded plan")
	if err != nil || created || retried.ID != outcome.ID || retried.OutcomeDigest != outcome.OutcomeDigest {
		t.Fatalf("idempotent consensus retry = %#v, created %v, err %v", retried, created, err)
	}

	current, err := service.GetTeam("robert", team.ID, team.Version)
	if err != nil {
		t.Fatal(err)
	}
	delegation := canonicalDelegation(now, current.Members[0], current.Members[1])
	assessment, err := service.AssessDelegation("robert", current.ID, current.Version, TeamRiskLow, delegation)
	if err != nil {
		t.Fatalf("AssessDelegation: %v", err)
	}
	if !assessment.ContractValid || !assessment.AdvisoryOnly || assessment.GrantsExecutionAuthority || !assessment.ExecutionAuthorizationRequired {
		t.Fatalf("delegation assessment crossed authority boundary: %#v", assessment)
	}
	delegation.RequiredAuthority = current.MaximumDelegatedAuthority + 1
	if _, err := service.AssessDelegation("robert", current.ID, current.Version, TeamRiskLow, delegation); err == nil {
		t.Fatal("delegation above team ceiling was accepted")
	}

	revoked, err := service.RevokeTeam("robert", current.ID, current.Version, TeamTransitionRequest{
		ExpectedRevision: current.Revision,
		Actor:            "owner",
		Reason:           "Team contract is no longer trusted.",
		EvidenceRefs:     []string{"audit://team/revocation"},
	})
	if err != nil {
		t.Fatalf("RevokeTeam: %v", err)
	}
	if revoked.Status != AgentTeamRevoked || revoked.RevokedAt == nil {
		t.Fatalf("team not revoked: %#v", revoked)
	}
	for _, member := range revoked.Members {
		if member.Status != TeamMemberRevoked || member.RevokedAt == nil {
			t.Fatalf("member not revoked with team: %#v", member)
		}
	}
	if _, _, err := service.StoreCoordinationMessage("robert", revoked.ID, revoked.Version, first); err == nil {
		t.Fatal("revoked team accepted a coordination message")
	}

	events, err := service.Events("robert", revoked.ID, revoked.Version)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != int(revoked.Revision) {
		t.Fatalf("event count %d does not match revision %d", len(events), revoked.Revision)
	}
	for index, event := range events {
		expectedPrevious := ""
		if index > 0 {
			expectedPrevious = events[index-1].EventDigest
		}
		if event.Sequence != uint64(index+1) || event.PreviousEventDigest != expectedPrevious {
			t.Fatalf("broken event chain at %d: %#v", index, event)
		}
		digest, err := teamLifecycleEventDigest(event)
		if err != nil || digest != event.EventDigest {
			t.Fatalf("invalid event digest at %d: %v", index, err)
		}
	}
}

func TestAgentTeamConsensusEscalatesCanonicalDecisionConflict(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 31, 11, 0, 0, 0, time.UTC)
	service := newAgentTeamService(NewMemoryAgentTeamRepository(), func() time.Time { return now }, deterministicTeamIDs("conflict"))
	team := createActiveTestTeam(t, service, "robert", now)
	correlationID := deterministicUUID("conflict-correlation")
	messages := []agentcoordination.Message{
		decisionMessage(t, team, now, correlationID, team.Members[0], team.Members[1], TeamVoteSupport, "Use plan A.", "conflict-one"),
		decisionMessage(t, team, now, correlationID, team.Members[1], team.Members[0], TeamVoteOppose, "Use plan B.", "conflict-two"),
	}
	for _, message := range messages {
		if _, _, err := service.StoreCoordinationMessage("robert", team.ID, team.Version, message); err != nil {
			t.Fatal(err)
		}
	}
	outcome, _, err := service.RecordConsensus("robert", team.ID, team.Version, correlationID, deterministicUUID("conflict-outcome"), "Choose a plan")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != TeamOutcomeEscalated || len(outcome.Conflicts) == 0 {
		t.Fatalf("canonical conflict was not escalated: %#v", outcome)
	}
	if outcome.Conflicts[0].Type != agentcoordination.ConflictContradictoryDecision {
		t.Fatalf("unexpected conflict type: %#v", outcome.Conflicts[0])
	}
}

func TestAgentTeamMessageAcknowledgmentsAreDurableAdvisoryAttentionState(t *testing.T) {
	t.Parallel()

	current := time.Date(2026, time.August, 8, 10, 0, 0, 0, time.UTC)
	repo := NewMemoryAgentTeamRepository()
	service := newAgentTeamService(repo, func() time.Time { return current }, deterministicTeamIDs("acknowledgments"))
	team := createActiveTestTeam(t, service, "robert", current)
	message := decisionMessage(
		t,
		team,
		current,
		deterministicUUID("ack-correlation"),
		team.Members[0],
		team.Members[1],
		TeamVoteSupport,
		"Review the bounded plan.",
		"ack-message",
	)
	message.RequiresAck = true
	var err error
	message.PayloadDigest, err = agentcoordination.ComputeMessageDigest(message)
	if err != nil {
		t.Fatal(err)
	}
	if _, created, err := service.StoreCoordinationMessage("robert", team.ID, team.Version, message); err != nil || !created {
		t.Fatalf("store acknowledgment-required message: created=%v err=%v", created, err)
	}

	attention, err := service.MessageAttention("robert", team.ID, team.Version)
	if err != nil || len(attention.Messages) != 1 || attention.Messages[0].State != TeamMessageAttentionWaiting {
		t.Fatalf("initial message attention = %#v, err %v", attention, err)
	}
	if !attention.Messages[0].AdvisoryOnly || attention.Messages[0].GrantsExecutionAuthority || !attention.Messages[0].ExecutionAuthorizationRequired {
		t.Fatalf("message attention crossed authority boundary: %#v", attention.Messages[0])
	}

	current = current.Add(16 * time.Minute)
	attention, err = service.MessageAttention("robert", team.ID, team.Version)
	if err != nil || attention.Messages[0].State != TeamMessageAttentionOverdue || !attention.Messages[0].HumanReviewRequired {
		t.Fatalf("overdue message attention = %#v, err %v", attention, err)
	}

	retryAfter := current.Add(10 * time.Minute)
	deferred := agentcoordination.Acknowledgment{
		ID:             deterministicUUID("ack-deferred-id"),
		MessageID:      message.ID,
		CorrelationID:  message.CorrelationID,
		RecipientID:    message.Recipient.ID,
		Status:         agentcoordination.AcknowledgmentDeferred,
		Reason:         "The reviewer needs the cited source.",
		CreatedAt:      current,
		RetryAfter:     &retryAfter,
		IdempotencyKey: deterministicUUID("ack-deferred-key"),
	}
	stored, created, err := service.AcknowledgeCoordinationMessage("robert", team.ID, team.Version, message.ID, deferred)
	if err != nil || !created || stored.ID != deferred.ID {
		t.Fatalf("defer acknowledgment = %#v, created=%v err=%v", stored, created, err)
	}
	replayed, created, err := service.AcknowledgeCoordinationMessage("robert", team.ID, team.Version, message.ID, deferred)
	if err != nil || created || replayed.ID != deferred.ID {
		t.Fatalf("replay acknowledgment = %#v, created=%v err=%v", replayed, created, err)
	}
	conflict := deferred
	conflict.Reason = "Conflicting replay content."
	if _, _, err := service.AcknowledgeCoordinationMessage("robert", team.ID, team.Version, message.ID, conflict); !errors.Is(err, ErrAgentTeamIdempotencyConflict) {
		t.Fatalf("acknowledgment idempotency conflict = %v", err)
	}
	attention, err = service.MessageAttention("robert", team.ID, team.Version)
	if err != nil || attention.Messages[0].State != TeamMessageAttentionDeferred || attention.Messages[0].HumanReviewRequired {
		t.Fatalf("deferred message attention = %#v, err %v", attention, err)
	}

	current = current.Add(time.Minute)
	accepted := agentcoordination.Acknowledgment{
		ID:             deterministicUUID("ack-accepted-id"),
		MessageID:      message.ID,
		CorrelationID:  message.CorrelationID,
		RecipientID:    message.Recipient.ID,
		Status:         agentcoordination.AcknowledgmentAccepted,
		CreatedAt:      current,
		IdempotencyKey: deterministicUUID("ack-accepted-key"),
	}
	if _, created, err := service.AcknowledgeCoordinationMessage("robert", team.ID, team.Version, message.ID, accepted); err != nil || !created {
		t.Fatalf("accept acknowledgment: created=%v err=%v", created, err)
	}
	attention, err = service.MessageAttention("robert", team.ID, team.Version)
	if err != nil || attention.Messages[0].State != TeamMessageAttentionAcknowledged || attention.Messages[0].HumanReviewRequired {
		t.Fatalf("acknowledged message attention = %#v, err %v", attention, err)
	}
	current = message.ExpiresAt.Add(time.Minute)
	attention, err = service.MessageAttention("robert", team.ID, team.Version)
	if err != nil || attention.Messages[0].State != TeamMessageAttentionAcknowledged || attention.Messages[0].Reason != "message acknowledged" {
		t.Fatalf("historical acknowledgment was lost after envelope expiry: %#v, err %v", attention, err)
	}
	rejected := accepted
	rejected.ID = deterministicUUID("ack-rejected-id")
	rejected.IdempotencyKey = deterministicUUID("ack-rejected-key")
	rejected.Status = agentcoordination.AcknowledgmentRejected
	rejected.Reason = "No longer accepted."
	rejected.CreatedAt = accepted.CreatedAt.Add(time.Minute)
	if _, _, err := service.AcknowledgeCoordinationMessage("robert", team.ID, team.Version, message.ID, rejected); !errors.Is(err, ErrAgentTeamAcknowledgmentTerminal) {
		t.Fatalf("terminal acknowledgment mutation = %v", err)
	}
	if _, err := service.MessageAcknowledgments("another-owner", team.ID, team.Version, message.ID); !errors.Is(err, ErrAgentTeamMessageNotFound) {
		t.Fatalf("cross-owner acknowledgment read = %v", err)
	}
}

func TestAgentTeamDecisionOverviewReadsMessagesOnceAndKeepsAttentionAdvisory(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 11, 10, 0, 0, 0, time.UTC)
	base := NewMemoryAgentTeamRepository()
	repo := &batchAttentionRepository{AgentTeamRepository: base}
	service := newAgentTeamService(repo, func() time.Time { return now }, deterministicTeamIDs("decision-overview"))
	team := createActiveTestTeam(t, service, "robert", now)
	message := decisionMessage(t, team, now, deterministicUUID("decision-overview-correlation"), team.Members[0], team.Members[1], TeamVoteSupport, "Review the bounded plan.", "decision-overview-message")
	message.RequiresAck = true
	var err error
	message.PayloadDigest, err = agentcoordination.ComputeMessageDigest(message)
	if err != nil {
		t.Fatal(err)
	}
	if _, created, err := service.StoreCoordinationMessage("robert", team.ID, team.Version, message); err != nil || !created {
		t.Fatalf("store message: created=%v err=%v", created, err)
	}

	overview, err := service.DecisionOverview("robert", team.ID, team.Version)
	if err != nil {
		t.Fatalf("DecisionOverview: %v", err)
	}
	if len(overview.Messages) != 1 || overview.Messages[0].ID != message.ID || len(overview.Attention) != 1 || overview.Attention[0].MessageID != message.ID {
		t.Fatalf("decision overview = %#v", overview)
	}
	if !overview.Attention[0].AdvisoryOnly || overview.Attention[0].GrantsExecutionAuthority || !overview.Attention[0].ExecutionAuthorizationRequired {
		t.Fatalf("decision overview crossed authority boundary: %#v", overview.Attention[0])
	}
	if repo.individualMessageCalls != 1 || repo.batchCalls != 1 || repo.individualCalls != 0 {
		t.Fatalf("decision overview reads = messages:%d acknowledgment batch:%d individual acknowledgments:%d", repo.individualMessageCalls, repo.batchCalls, repo.individualCalls)
	}
}

func TestAgentTeamMessageAttentionBatchesAcknowledgmentReadsWhenSupported(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 9, 0, 0, 0, time.UTC)
	base := NewMemoryAgentTeamRepository()
	repo := &batchAttentionRepository{AgentTeamRepository: base}
	service := newAgentTeamService(repo, func() time.Time { return now }, deterministicTeamIDs("batch-attention"))
	team := createActiveTestTeam(t, service, "robert", now)
	for index := 0; index < 2; index++ {
		message := decisionMessage(
			t,
			team,
			now.Add(time.Duration(index)*time.Minute),
			deterministicUUID(fmt.Sprintf("batch-attention-correlation-%d", index)),
			team.Members[index],
			team.Members[1-index],
			TeamVoteSupport,
			"Review the bounded plan.",
			fmt.Sprintf("batch-attention-message-%d", index),
		)
		if _, created, err := service.StoreCoordinationMessage("robert", team.ID, team.Version, message); err != nil || !created {
			t.Fatalf("store message %d: created=%t err=%v", index, created, err)
		}
	}

	attention, err := service.MessageAttention("robert", team.ID, team.Version)
	if err != nil {
		t.Fatalf("MessageAttention: %v", err)
	}
	if len(attention.Messages) != 2 {
		t.Fatalf("attention messages = %#v", attention.Messages)
	}
	if repo.batchCalls != 1 || repo.individualCalls != 0 {
		t.Fatalf("attention reads must use one batch call: batch=%d individual=%d", repo.batchCalls, repo.individualCalls)
	}
}

func TestAgentTeamMessageAttentionIndexIsOwnerScopedAndUsesOneOverviewRead(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC)
	base := NewMemoryAgentTeamRepository()
	repo := &batchAttentionRepository{AgentTeamRepository: base}
	service := newAgentTeamService(repo, func() time.Time { return now }, deterministicTeamIDs("attention-index"))
	team := createActiveTestTeam(t, service, "robert", now)
	message := decisionMessage(t, team, now, deterministicUUID("attention-index-correlation"), team.Members[0], team.Members[1], TeamVoteSupport, "Keep the bounded plan.", "attention-index-message")
	if _, created, err := service.StoreCoordinationMessage("robert", team.ID, team.Version, message); err != nil || !created {
		t.Fatalf("StoreCoordinationMessage = created %v, err %v", created, err)
	}

	index, err := service.MessageAttentionIndex("robert")
	if err != nil {
		t.Fatalf("MessageAttentionIndex: %v", err)
	}
	if len(index.Contracts) != 1 || index.Contracts[0].ID != team.ID || len(index.Teams) != 1 || index.Teams[0].TeamID != team.ID || index.Teams[0].TeamVersion != team.Version || len(index.Teams[0].Messages) != 1 {
		t.Fatalf("unexpected owner-scoped attention index: %#v", index)
	}
	if repo.teamMessageBatchCalls != 1 || repo.individualMessageCalls != 0 {
		t.Fatalf("attention index must batch team message reads: batch=%d individual=%d", repo.teamMessageBatchCalls, repo.individualMessageCalls)
	}
	other, err := service.MessageAttentionIndex("someone-else")
	if err != nil {
		t.Fatalf("MessageAttentionIndex(other owner): %v", err)
	}
	if len(other.Contracts) != 0 || len(other.Teams) != 0 {
		t.Fatalf("owner-scoped attention leaked teams: %#v", other)
	}
}

type batchAttentionRepository struct {
	AgentTeamRepository
	batchCalls             int
	individualCalls        int
	teamMessageBatchCalls  int
	individualMessageCalls int
}

func (r *batchAttentionRepository) ListMessageAcknowledgments(owner, teamID, version, messageID string) ([]agentcoordination.Acknowledgment, error) {
	r.individualCalls++
	return nil, errors.New("per-message acknowledgment reads are not allowed in this attention test")
}

func (r *batchAttentionRepository) ListMessageAcknowledgmentsForMessages(owner, teamID, version string, messageIDs []string) (map[string][]agentcoordination.Acknowledgment, error) {
	r.batchCalls++
	result := make(map[string][]agentcoordination.Acknowledgment, len(messageIDs))
	for _, messageID := range messageIDs {
		acknowledgments, err := r.AgentTeamRepository.ListMessageAcknowledgments(owner, teamID, version, messageID)
		if err != nil {
			return nil, err
		}
		result[messageID] = acknowledgments
	}
	return result, nil
}

func (r *batchAttentionRepository) ListCoordinationMessages(owner, teamID, version, correlationID string) ([]agentcoordination.Message, error) {
	r.individualMessageCalls++
	return r.AgentTeamRepository.ListCoordinationMessages(owner, teamID, version, correlationID)
}

func (r *batchAttentionRepository) ListCoordinationMessagesForTeams(owner string, teams []AgentTeamContract) (map[string][]agentcoordination.Message, error) {
	r.teamMessageBatchCalls++
	return r.AgentTeamRepository.(*MemoryAgentTeamRepository).ListCoordinationMessagesForTeams(owner, teams)
}

func TestAgentTeamDeterministicValidationAndVersionProvenance(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)
	first := newAgentTeamService(NewMemoryAgentTeamRepository(), func() time.Time { return now }, deterministicTeamIDs("deterministic"))
	second := newAgentTeamService(NewMemoryAgentTeamRepository(), func() time.Time { return now }, deterministicTeamIDs("deterministic"))
	request := testTeamRequest(now)
	left, err := first.CreateTeam("robert", request)
	if err != nil {
		t.Fatal(err)
	}
	right, err := second.CreateTeam("robert", request)
	if err != nil {
		t.Fatal(err)
	}
	if left.ContractDigest != right.ContractDigest || left.ID != right.ID {
		t.Fatalf("same normalized input was not deterministic: %s / %s", left.ContractDigest, right.ContractDigest)
	}

	version, err := first.CreateTeamVersion("robert", left.ID, CreateAgentTeamVersionRequest{
		PreviousVersion:           left.Version,
		Version:                   "1.1.0",
		Name:                      left.Name,
		Purpose:                   "A revised advisory coordination contract.",
		AuthorityCeiling:          left.AuthorityCeiling,
		RiskCeiling:               left.RiskCeiling,
		MaximumDelegatedAuthority: left.MaximumDelegatedAuthority,
		MaximumDelegatedRisk:      left.MaximumDelegatedRisk,
		Roles:                     left.Roles,
		Capabilities:              left.Capabilities,
		CoordinationPolicy:        left.CoordinationPolicy,
		Consensus:                 left.Consensus,
		EvidenceRefs:              []string{"source://team/v1.1"},
		Provenance: TeamProvenance{
			Source:     "owner configuration",
			Reference:  "source://team/v1.1",
			AuthoredBy: "owner",
		},
		ExpectedPreviousDigest: left.ContractDigest,
		Actor:                  "owner",
	})
	if err != nil {
		t.Fatalf("CreateTeamVersion: %v", err)
	}
	if version.ID != left.ID || version.PreviousVersionDigest != left.ContractDigest || version.Status != AgentTeamDraft {
		t.Fatalf("version provenance is incomplete: %#v", version)
	}
	versions, err := first.ListTeamVersions("robert", left.ID)
	if err != nil || len(versions) != 2 || versions[0].Version != "1.1.0" {
		t.Fatalf("ListTeamVersions = %#v, err %v", versions, err)
	}
}

func TestAgentTeamMembershipLifecycleIsExplicitAndQuorumSafe(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 31, 12, 30, 0, 0, time.UTC)
	service := newAgentTeamService(NewMemoryAgentTeamRepository(), func() time.Time { return now }, deterministicTeamIDs("membership"))
	team := createActiveTestTeam(t, service, "robert", now)
	team, err := service.AddMember("robert", team.ID, team.Version, AddTeamMemberRequest{
		ExpectedRevision: team.Revision,
		Actor:            "owner",
		Reason:           "Invite an additional verified reviewer.",
		Member: TeamMembership{
			AgentID: "second-reviewer", AgentVersion: "1.0.0", RoleIDs: []string{"reviewer"}, CapabilityIDs: []string{"review"}, Status: TeamMemberInvited, AuthorityCeiling: 1, RiskCeiling: TeamRiskLow, EvidenceRefs: []string{"agent://second-reviewer/manifest"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	membershipID := ""
	for _, member := range team.Members {
		if member.AgentID == normalizeIdentifier("second-reviewer") {
			membershipID = member.ID
		}
	}
	if membershipID == "" {
		t.Fatal("new membership was not persisted")
	}
	transitions := []string{TeamMemberActive, TeamMemberSuspended, TeamMemberActive, TeamMemberLeft}
	for _, status := range transitions {
		team, err = service.ChangeMembership("robert", team.ID, team.Version, membershipID, ChangeTeamMembershipRequest{
			ExpectedRevision: team.Revision,
			Actor:            "owner",
			Status:           status,
			Reason:           "Apply explicit membership lifecycle transition.",
			EvidenceRefs:     []string{"audit://membership/" + status},
		})
		if err != nil {
			t.Fatalf("ChangeMembership(%s): %v", status, err)
		}
	}
	leftStatus := ""
	for _, member := range team.Members {
		if member.ID == membershipID {
			leftStatus = member.Status
		}
	}
	if leftStatus != TeamMemberLeft {
		t.Fatalf("membership did not reach left state: %s", leftStatus)
	}
	if _, err := service.ChangeMembership("robert", team.ID, team.Version, membershipID, ChangeTeamMembershipRequest{
		ExpectedRevision: team.Revision,
		Actor:            "owner",
		Status:           TeamMemberActive,
		Reason:           "Invalid terminal-state transition.",
	}); err == nil {
		t.Fatal("terminal membership was reactivated")
	}
}

func TestAgentTeamRejectsAuthorityGrantAndSecretMaterial(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 31, 13, 0, 0, 0, time.UTC)
	service := newAgentTeamService(NewMemoryAgentTeamRepository(), func() time.Time { return now }, deterministicTeamIDs("invalid"))
	request := testTeamRequest(now)
	request.Capabilities[0].AdvisoryOnly = false
	if _, err := service.CreateTeam("robert", request); err == nil || !strings.Contains(err.Error(), "advisory") {
		t.Fatalf("authority-granting capability was not rejected: %v", err)
	}
	request = testTeamRequest(now)
	request.Purpose = "Use api_key=do-not-store for coordination."
	if _, err := service.CreateTeam("robert", request); err == nil || strings.Contains(err.Error(), "do-not-store") {
		t.Fatalf("secret handling error = %v", err)
	}
}

func TestGuidedAgentTeamCommandsOwnCanonicalEnvelopeAndRemainAdvisory(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC)
	service := newAgentTeamService(NewMemoryAgentTeamRepository(), func() time.Time { return now }, deterministicTeamIDs("guided"))
	team, err := service.CreateGuidedTeam("robert", CreateGuidedAgentTeamRequest{
		Key:                       "owner-review-council",
		Name:                      "Owner review council",
		Purpose:                   "Compare source-backed recommendations before Robert decides.",
		AuthorityCeiling:          4,
		MaximumDelegatedAuthority: 2,
		EvidenceRefs:              []string{"source://owner/team-charter"},
		Actor:                     "robert",
	})
	if err != nil {
		t.Fatalf("CreateGuidedTeam: %v", err)
	}
	if team.Status != AgentTeamDraft || team.RiskCeiling != TeamRiskLow || team.MaximumDelegatedRisk != TeamRiskLow {
		t.Fatalf("guided defaults are unsafe or incomplete: %#v", team)
	}
	if !team.AdvisoryOnly || team.GrantsExecutionAuthority || !team.ExecutionAuthorizationRequired {
		t.Fatalf("guided team grants authority: %#v", team)
	}
	if len(team.Roles) != 2 || len(team.Capabilities) != 2 || len(team.Members) != 0 {
		t.Fatalf("guided charter was not expanded deterministically: %#v", team)
	}

	for _, member := range []TeamMembership{
		{AgentID: "planner-agent", AgentVersion: "1.0.0", RoleIDs: []string{"coordinator"}, CapabilityIDs: []string{"analysis", "review"}, Status: TeamMemberActive, AuthorityCeiling: 3, RiskCeiling: TeamRiskLow, EvidenceRefs: []string{"agent://planner/manifest"}},
		{AgentID: "reviewer-agent", AgentVersion: "1.0.0", RoleIDs: []string{"reviewer"}, CapabilityIDs: []string{"review"}, Status: TeamMemberActive, AuthorityCeiling: 2, RiskCeiling: TeamRiskLow, EvidenceRefs: []string{"agent://reviewer/manifest"}},
	} {
		team, err = service.AddMember("robert", team.ID, team.Version, AddTeamMemberRequest{
			ExpectedRevision: team.Revision,
			Actor:            "robert",
			Reason:           "Register a verified advisory member.",
			Member:           member,
		})
		if err != nil {
			t.Fatalf("AddMember: %v", err)
		}
	}
	team, err = service.ActivateTeam("robert", team.ID, team.Version, TeamTransitionRequest{
		ExpectedRevision: team.Revision,
		Actor:            "robert",
		Reason:           "Two independent voting members satisfy quorum.",
		EvidenceRefs:     []string{"audit://team/activation"},
	})
	if err != nil {
		t.Fatalf("ActivateTeam: %v", err)
	}

	request := CreateTeamDecisionMessageRequest{
		SenderMembershipID:     team.Members[0].ID,
		RecipientMembershipID:  team.Members[1].ID,
		CorrelationID:          deterministicUUID("guided-correlation"),
		IdempotencyKey:         deterministicUUID("guided-decision-key"),
		Issue:                  "Select the bounded review approach",
		Position:               TeamVoteSupport,
		Recommendation:         "Use the source-backed plan and keep execution approval separate.",
		EvidenceRefs:           []string{"evidence://guided/decision"},
		RequiresAcknowledgment: true,
		ExpiresInMinutes:       60,
	}
	message, created, err := service.CreateDecisionMessage("robert", team.ID, team.Version, request)
	if err != nil || !created {
		t.Fatalf("CreateDecisionMessage = created %v, err %v", created, err)
	}
	if message.AuthorityLevel > 1 || message.PayloadDigest == "" || message.ID == "" || !message.RequiresAck {
		t.Fatalf("decision envelope is not canonical least-authority state: %#v", message)
	}
	replayed, created, err := service.CreateDecisionMessage("robert", team.ID, team.Version, request)
	if err != nil || created || replayed.ID != message.ID {
		t.Fatalf("decision replay = %#v, created %v, err %v", replayed, created, err)
	}

	ackRequest := CreateTeamAcknowledgmentRequest{
		Status:         string(agentcoordination.AcknowledgmentAccepted),
		Reason:         "The advisory recommendation was received for review.",
		IdempotencyKey: deterministicUUID("guided-ack-key"),
	}
	acknowledgment, created, err := service.CreateMessageAcknowledgment("robert", team.ID, team.Version, message.ID, ackRequest)
	if err != nil || !created {
		t.Fatalf("CreateMessageAcknowledgment = created %v, err %v", created, err)
	}
	if acknowledgment.MessageID != message.ID || acknowledgment.RecipientID != message.Recipient.ID {
		t.Fatalf("acknowledgment is not bound to the exact message: %#v", acknowledgment)
	}
	replayedAck, created, err := service.CreateMessageAcknowledgment("robert", team.ID, team.Version, message.ID, ackRequest)
	if err != nil || created || replayedAck.ID != acknowledgment.ID {
		t.Fatalf("acknowledgment replay = %#v, created %v, err %v", replayedAck, created, err)
	}
}

func TestAgentTeamDecisionRejectsNonVotingMember(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 11, 0, 0, 0, time.UTC)
	service := newAgentTeamService(NewMemoryAgentTeamRepository(), func() time.Time { return now }, deterministicTeamIDs("non-voter"))
	request := testTeamRequest(now)
	request.Roles = append(request.Roles, TeamRoleContract{
		ID: "observer", Name: "Observer", Purpose: "Observe advisory deliberation without voting.",
		CapabilityIDs: []string{"analysis"}, AllowedRecommendationTypes: []string{"status"},
		ProhibitedActions: []string{"vote", "execute work"}, EvidenceRequirements: []string{"source reference"},
		AuthorityCeiling: 1, RiskCeiling: TeamRiskLow, AdvisoryOnly: true,
	})
	team, err := service.CreateTeam("robert", request)
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	for _, member := range []TeamMembership{
		{AgentID: "planner", AgentVersion: "1.0.0", RoleIDs: []string{"coordinator"}, CapabilityIDs: []string{"analysis", "review"}, Status: TeamMemberActive, AuthorityCeiling: 2, RiskCeiling: TeamRiskLow, EvidenceRefs: []string{"agent://planner"}},
		{AgentID: "reviewer", AgentVersion: "1.0.0", RoleIDs: []string{"reviewer"}, CapabilityIDs: []string{"review"}, Status: TeamMemberActive, AuthorityCeiling: 2, RiskCeiling: TeamRiskLow, EvidenceRefs: []string{"agent://reviewer"}},
		{AgentID: "observer", AgentVersion: "1.0.0", RoleIDs: []string{"observer"}, CapabilityIDs: []string{"analysis"}, Status: TeamMemberActive, AuthorityCeiling: 1, RiskCeiling: TeamRiskLow, EvidenceRefs: []string{"agent://observer"}},
	} {
		team, err = service.AddMember("robert", team.ID, team.Version, AddTeamMemberRequest{ExpectedRevision: team.Revision, Actor: "robert", Reason: "Register advisory member.", Member: member})
		if err != nil {
			t.Fatalf("AddMember: %v", err)
		}
	}
	team, err = service.ActivateTeam("robert", team.ID, team.Version, TeamTransitionRequest{ExpectedRevision: team.Revision, Actor: "robert", Reason: "Voting quorum verified.", EvidenceRefs: []string{"audit://activation"}})
	if err != nil {
		t.Fatalf("ActivateTeam: %v", err)
	}
	observer, observerFound := activeMemberByAgentID(*team, "observer")
	reviewer, reviewerFound := activeMemberByAgentID(*team, "reviewer")
	if !observerFound || !reviewerFound {
		t.Fatalf("expected active observer and reviewer memberships: %#v", team.Members)
	}
	_, _, err = service.CreateDecisionMessage("robert", team.ID, team.Version, CreateTeamDecisionMessageRequest{
		SenderMembershipID: observer.ID, RecipientMembershipID: reviewer.ID,
		CorrelationID: deterministicUUID("observer-correlation"), IdempotencyKey: deterministicUUID("observer-key"),
		Issue: "Observer attempts to vote", Position: TeamVoteSupport, Recommendation: "Reject this vote.",
		EvidenceRefs: []string{"evidence://observer"}, ExpiresInMinutes: 60,
	})
	if err == nil || !strings.Contains(err.Error(), "voting role") {
		t.Fatalf("non-voting member decision was not rejected: %v", err)
	}
}

func createActiveTestTeam(t *testing.T, service *AgentTeamService, owner string, now time.Time) *AgentTeamContract {
	t.Helper()
	team, err := service.CreateTeam(owner, testTeamRequest(now))
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	members := []TeamMembership{
		{
			AgentID:          "planner-agent",
			AgentVersion:     "1.0.0",
			RoleIDs:          []string{"coordinator"},
			CapabilityIDs:    []string{"analysis", "review"},
			Status:           TeamMemberActive,
			AuthorityCeiling: 3,
			RiskCeiling:      TeamRiskMedium,
			EvidenceRefs:     []string{"agent://planner/manifest"},
		},
		{
			AgentID:          "reviewer-agent",
			AgentVersion:     "1.0.0",
			RoleIDs:          []string{"reviewer"},
			CapabilityIDs:    []string{"review"},
			Status:           TeamMemberActive,
			AuthorityCeiling: 2,
			RiskCeiling:      TeamRiskLow,
			EvidenceRefs:     []string{"agent://reviewer/manifest"},
		},
	}
	for _, member := range members {
		team, err = service.AddMember(owner, team.ID, team.Version, AddTeamMemberRequest{
			ExpectedRevision: team.Revision,
			Actor:            "owner",
			Member:           member,
			Reason:           "Add verified advisory member.",
		})
		if err != nil {
			t.Fatalf("AddMember: %v", err)
		}
	}
	team, err = service.ActivateTeam(owner, team.ID, team.Version, TeamTransitionRequest{
		ExpectedRevision: team.Revision,
		Actor:            "owner",
		Reason:           "Verified members satisfy quorum.",
		EvidenceRefs:     []string{"audit://team/activation"},
	})
	if err != nil {
		t.Fatalf("ActivateTeam: %v", err)
	}
	if !team.AdvisoryOnly || team.GrantsExecutionAuthority || !team.ExecutionAuthorizationRequired {
		t.Fatalf("team authority invariant failed: %#v", team)
	}
	return team
}

func testTeamRequest(now time.Time) CreateAgentTeamRequest {
	policy := agentcoordination.DefaultValidationPolicy()
	policy.MaximumAuthority = 4
	return CreateAgentTeamRequest{
		Key:                       "governed-review-team",
		Version:                   "1.0.0",
		Name:                      "Governed review team",
		Purpose:                   "Coordinate bounded planning and review recommendations.",
		AuthorityCeiling:          4,
		RiskCeiling:               TeamRiskMedium,
		MaximumDelegatedAuthority: 2,
		MaximumDelegatedRisk:      TeamRiskLow,
		Capabilities: []TeamCapabilityContract{
			{ID: "analysis", Name: "Analysis", Description: "Analyze source-backed inputs.", InputSchema: "schema://analysis/input/v1", OutputSchema: "schema://analysis/output/v1", EvidenceRequired: []string{"source reference"}, ProhibitedActions: []string{"execute tools"}, AuthorityCeiling: 3, RiskCeiling: TeamRiskMedium, AdvisoryOnly: true},
			{ID: "review", Name: "Review", Description: "Review an advisory proposal.", InputSchema: "schema://review/input/v1", OutputSchema: "schema://review/output/v1", EvidenceRequired: []string{"proposal digest"}, ProhibitedActions: []string{"approve execution"}, AuthorityCeiling: 3, RiskCeiling: TeamRiskMedium, AdvisoryOnly: true},
		},
		Roles: []TeamRoleContract{
			{ID: "coordinator", Name: "Coordinator", Purpose: "Coordinate advisory analysis.", CapabilityIDs: []string{"analysis", "review"}, AllowedRecommendationTypes: []string{"plan", "review"}, ProhibitedActions: []string{"grant authority"}, EvidenceRequirements: []string{"source references"}, AuthorityCeiling: 3, RiskCeiling: TeamRiskMedium, MayCoordinate: true, MayVote: true, AdvisoryOnly: true},
			{ID: "reviewer", Name: "Reviewer", Purpose: "Review proposals independently.", CapabilityIDs: []string{"review"}, AllowedRecommendationTypes: []string{"review"}, ProhibitedActions: []string{"execute work"}, EvidenceRequirements: []string{"proposal digest"}, AuthorityCeiling: 2, RiskCeiling: TeamRiskLow, MayVote: true, AdvisoryOnly: true},
		},
		CoordinationPolicy: policy,
		Consensus: TeamConsensusPolicy{
			Mode:                       TeamConsensusMajority,
			DecisionPayloadSchema:      "schema://hai/team-decision/v1",
			Quorum:                     2,
			MinimumSupport:             2,
			AllowAbstention:            true,
			RequireEvidence:            true,
			ConflictEscalationRequired: true,
			TieOutcome:                 TeamOutcomeEscalated,
		},
		EvidenceRefs: []string{"source://team/charter"},
		Provenance: TeamProvenance{
			Source:     "owner configuration",
			Reference:  "source://team/charter",
			AuthoredBy: "owner",
		},
		Actor: "owner",
	}
}

func decisionMessage(t *testing.T, team *AgentTeamContract, now time.Time, correlationID string, sender, recipient TeamMembership, position, recommendation, seed string) agentcoordination.Message {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"position": position, "recommendation": recommendation})
	if err != nil {
		t.Fatal(err)
	}
	message := agentcoordination.Message{
		ID:             deterministicUUID(seed + "-id"),
		IdempotencyKey: deterministicUUID(seed + "-key"),
		CorrelationID:  correlationID,
		SchemaVersion:  team.CoordinationPolicy.SchemaVersion,
		Type:           agentcoordination.MessageTypeDecision,
		Sender: agentcoordination.AgentRef{
			ID:               sender.AgentID,
			Role:             sender.RoleIDs[0],
			AuthorityCeiling: sender.AuthorityCeiling,
		},
		Recipient: agentcoordination.AgentRef{
			ID:               recipient.AgentID,
			Role:             recipient.RoleIDs[0],
			AuthorityCeiling: recipient.AuthorityCeiling,
		},
		Confidentiality: agentcoordination.ConfidentialityInternal,
		AuthorityLevel:  1,
		Payload: agentcoordination.MessagePayload{
			Schema:  team.Consensus.DecisionPayloadSchema,
			Subject: "Select the bounded plan",
			Data:    payload,
		},
		EvidenceRefs:      []string{"evidence://" + seed},
		CreatedAt:         now,
		ExpiresAt:         now.Add(time.Hour),
		ProvenanceSummary: "Verified advisory team deliberation.",
	}
	message.PayloadDigest, err = agentcoordination.ComputeMessageDigest(message)
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func canonicalDelegation(now time.Time, principal, delegate TeamMembership) agentcoordination.DelegationEnvelope {
	return agentcoordination.DelegationEnvelope{
		ID:             deterministicUUID("delegation-id"),
		TaskID:         deterministicUUID("delegation-task"),
		IdempotencyKey: deterministicUUID("delegation-key"),
		CorrelationID:  deterministicUUID("delegation-correlation"),
		Principal: agentcoordination.AgentRef{
			ID: principal.AgentID, Role: principal.RoleIDs[0], AuthorityCeiling: principal.AuthorityCeiling,
		},
		Delegate: agentcoordination.AgentRef{
			ID: delegate.AgentID, Role: delegate.RoleIDs[0], AuthorityCeiling: delegate.AuthorityCeiling,
		},
		Objective:         "Prepare an advisory review.",
		SuccessCriteria:   []string{"review cites source evidence"},
		StopConditions:    []string{"missing source evidence"},
		ProhibitedActions: []string{"execute tools", "grant authority"},
		ExecutionMode:     agentcoordination.ExecutionModePlanOnly,
		ApprovalMode:      agentcoordination.ApprovalNotRequired,
		RequiredAuthority: 1,
		EvidenceRefs:      []string{"source://delegation/request"},
		Status:            agentcoordination.DelegationProposed,
		CreatedAt:         now,
		DueAt:             now.Add(time.Hour),
		UpdatedAt:         now,
	}
}

func deterministicTeamIDs(namespace string) func() string {
	sequence := 0
	return func() string {
		sequence++
		return deterministicUUID(fmt.Sprintf("%s-%d", namespace, sequence))
	}
}

func deterministicUUID(value string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(value)).String()
}

func TestAgentTeamRevisionConflictIsTyped(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 31, 14, 0, 0, 0, time.UTC)
	service := newAgentTeamService(NewMemoryAgentTeamRepository(), func() time.Time { return now }, deterministicTeamIDs("revision"))
	team, err := service.CreateTeam("robert", testTeamRequest(now))
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.AddMember("robert", team.ID, team.Version, AddTeamMemberRequest{
		ExpectedRevision: team.Revision + 1,
		Actor:            "owner",
		Reason:           "stale update",
		Member: TeamMembership{
			AgentID: "agent", AgentVersion: "1.0.0", RoleIDs: []string{"reviewer"}, CapabilityIDs: []string{"review"}, Status: TeamMemberInvited, AuthorityCeiling: 1, RiskCeiling: TeamRiskLow, EvidenceRefs: []string{"agent://manifest"},
		},
	})
	if !errors.Is(err, ErrAgentTeamRevisionConflict) {
		t.Fatalf("expected typed revision conflict, got %v", err)
	}
}
