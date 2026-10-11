import { ChangeDetectionStrategy, Component, OnDestroy, OnInit, ViewEncapsulation } from '@angular/core';
import { FormBuilder, FormGroup, Validators } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { NzModalService } from 'ng-zorro-antd/modal';
import { finalize, Observable, Subscription, take } from 'rxjs';
import {
  IAutomationLaunchEvent,
  IAutomationRuntimeRouteTrace,
} from '../../models/automation.model.interface';
import {
  IPursuit,
  IPursuitAction,
  IPursuitAutomation,
  IPursuitBlocker,
  IPursuitDashboard,
  IPursuitDashboardDecision,
  IPursuitDelegationPackage,
  IPursuitDecision,
  IPursuitDetail,
  IPursuitEvidenceResolution,
  IProjectDossier,
  IPursuitLink,
	IPursuitLifeDomainReconciliationResult,
  IPursuitListItem,
  IPursuitDependency,
  IPursuitPortfolioPlanningRequest,
  IPursuitPortfolioPlanningResult,
  IPursuitPortfolioEstimateCalibrationBinding,
  IPursuitPortfolioEstimateCalibrationRecommendation,
  IPursuitPortfolioAllocationAcceptanceRequest,
  IPursuitPortfolioAllocationAcceptanceResult,
  IPursuitPortfolioAllocationItem,
  IPursuitPortfolioExecutionProposalItem,
  IPursuitPortfolioExecutionProposalResult,
  IPursuitPortfolioExecutionProposalDecisionResult,
  IPursuitPortfolioCoordinationResult,
  IPursuitPortfolioCoordinationItem,
  IPursuitPortfolioDispatchResult,
  IPursuitPortfolioDispatchItemResult,
  IPursuitPortfolioWorkflowEffectAuthorizationResult,
  IPursuitPortfolioWorkflowEffectExecutionResult,
  IPursuitPortfolioWorkflowSettlementResult,
  IPursuitPortfolioPriorityFactors,
  IPursuitResourceLimits,
  IPursuitResourceEvent,
  IPursuitResourceEventRequest,
  IPursuitResourceUsage,
  IPursuitStopCondition,
  IPursuitSuccessCriterion,
  PursuitPortfolioExecutionProposalDecision,
  PursuitPortfolioExecutionProposalDecisionConfirmation,
} from '../../models/pursuit.model.interface';
import { IWorkflowRecord } from '../../models/workflow.model.interface';
import { AutomationsService } from '../../services/automations/automations.service';
import { PursuitService } from '../../services/pursuit.service';
import { WorkflowService } from '../../services/workflow/workflow.service';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';

type PortfolioFactorKey = keyof IPursuitPortfolioPriorityFactors;

interface PortfolioPursuitDraft {
  pursuit: IPursuit;
  selected: boolean;
  optional: boolean;
  optimisticMinutes: number | null;
  expectedMinutes: number | null;
  pessimisticMinutes: number | null;
  estimatedCostEur: number;
  inputTokens: number;
  outputTokens: number;
  toolCalls: number;
  estimateBasis: string;
  factors: IPursuitPortfolioPriorityFactors;
  factorsReviewed: boolean;
  calibration?: IPursuitPortfolioEstimateCalibrationBinding;
  calibrationExpected?: {
    optimisticMinutes: number;
    expectedMinutes: number;
    pessimisticMinutes: number;
    costMicros: number;
  };
}

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-pursuits',
    templateUrl: './pursuits.component.html',
    styleUrls: ['./pursuits.component.scss'],
    // This route owns generic operational selectors such as .page-header.
    // Keep them scoped so visiting Pursuits cannot restyle another lazy page.
    encapsulation: ViewEncapsulation.Emulated,
    standalone: false
})
export class PursuitsComponent implements OnInit, OnDestroy {
  readonly moduleId = 'pursuits';
  dashboard?: IPursuitDashboard;
  pursuits: IPursuit[] = [];
  selected?: IPursuitDetail;
  projectDossier?: IProjectDossier;
  projectDossierProjectKey = '';
  projectDossierLoading = false;
  projectDossierError = '';
  loading = false;
  dashboardError = '';
  pursuitsError = '';
  detailLoading = false;
  detailError = '';
  creating = false;
	lifeDomainReconciliationRunning = false;
	lifeDomainReconciliation?: IPursuitLifeDomainReconciliationResult;
  intakeRunning = false;
  routedIntakeRunning = false;
  reviewing = false;
  summaryLoading = false;
  contextSaving = false;
  linkSaving = false;
  detachingLinkId = '';
  linkConfirmationId = '';
  private linkConfirmationRevision = 0;
  private contextEditorRevision = 0;
  planning = false;
  delegationLoading = false;
  resolvingDecisionId = '';
  stoppingAutomationId = '';
  lifecycleRunningId = '';
  private runtimeStopRecovery?: { pursuitId: string; automationId: string; runtimeId: string; evidenceUri: string };
  resolvingEvidenceUri = '';
  inspectedEvidence?: IPursuitEvidenceResolution;
  inspectedRuntimeEvidence?: IAutomationLaunchEvent;
  inspectedAction?: IPursuitAction;
  delegationPackage?: IPursuitDelegationPackage;
  showCreate = false;
  showContextEditor = false;
  draftSuccessCriteria: IPursuitSuccessCriterion[] = [];
  draftStopConditions: IPursuitStopCondition[] = [];
  draftDependencies: IPursuitDependency[] = [];
  includeArchived = false;
  private requestedPursuitId = '';
  private requestedEvidenceUri = '';
  private createIntentApplied = false;
  highlightedDecisionId = '';
  private routeSub?: Subscription;
  private runtimeStopSub?: Subscription;
  private evidenceResolutionSub?: Subscription;
  private dashboardSub?: Subscription;
  private pursuitsSub?: Subscription;
  private pursuitDetailSub?: Subscription;
  private decisionDetailSub?: Subscription;
  private decisionMutationSub?: Subscription;
  private lifecycleSub?: Subscription;
  private delegationSub?: Subscription;
  private summarySub?: Subscription;
  private reviewSub?: Subscription;
  private planningSub?: Subscription;
  private contextSaveSub?: Subscription;
  private linkSaveSub?: Subscription;
  private linkDetachSub?: Subscription;
  private intakeSub?: Subscription;
  private routedIntakeSub?: Subscription;
  private resourceAppendSub?: Subscription;
  private reservationReleaseSub?: Subscription;
  private reservationConfirmationId = '';
  private reservationConfirmationRevision = 0;
  private resourceEventsSub?: Subscription;
  private projectDossierSub?: Subscription;
  private projectDossierRequestGeneration = 0;
  private portfolioAllocationHistorySub?: Subscription;
  private portfolioExecutionProposalHistoryReadSub?: Subscription;
  private portfolioExecutionProposalSub?: Subscription;
  private portfolioExecutionProposalDecisionSub?: Subscription;
  private portfolioDispatchCoordinationSub?: Subscription;
  private portfolioDispatchCoordinationBatchSub?: Subscription;
  private portfolioDispatchSub?: Subscription;
  private portfolioWorkflowAuthorizationSub?: Subscription;
  private portfolioWorkflowExecutionSub?: Subscription;
  private portfolioWorkflowSettlementSub?: Subscription;
  private portfolioWorkflowVerificationSubs = new Subscription();
  private portfolioExecutionProposalDecisionHistorySub = new Subscription();
  resourceEvents: IPursuitResourceEvent[] = [];
  resourceEventsLoading = false;
  resourceEventsError = '';
  resourceEventsLoadedFor = '';
  resourceEventSaving = false;
  releasingReservationId = '';
  reservationReleaseReasons: Record<string, string> = {};
  portfolioPlannerVisible = false;
  portfolioPlanningRunning = false;
  portfolioPlanningError = '';
  portfolioResult?: IPursuitPortfolioPlanningResult;
  portfolioPlanningRequest?: IPursuitPortfolioPlanningRequest;
  portfolioAllocationResult?: IPursuitPortfolioAllocationAcceptanceResult;
  portfolioAcceptancePending = false;
  portfolioAcceptanceRunning = false;
  portfolioAcceptanceError = '';
  portfolioAllocationHistory: IPursuitPortfolioAllocationAcceptanceResult[] = [];
  portfolioAllocationHistoryLoading = false;
  portfolioAllocationHistoryLoaded = false;
  portfolioAllocationHistoryError = '';
  portfolioExecutionProposals: Record<string, IPursuitPortfolioExecutionProposalResult> = {};
  portfolioExecutionProposalErrors: Record<string, string> = {};
  portfolioExecutionProposalPendingId = '';
  portfolioExecutionProposalRunningId = '';
  portfolioExecutionProposalDecisionReasons: Record<string, string> = {};
  portfolioExecutionProposalDecisionHistory: Record<string, IPursuitPortfolioExecutionProposalDecisionResult[]> = {};
  portfolioExecutionProposalDecisionErrors: Record<string, string> = {};
  portfolioExecutionProposalDecisionPendingId = '';
  portfolioExecutionProposalDecisionRunningId = '';
  portfolioDispatchCoordination: Record<string, IPursuitPortfolioCoordinationResult> = {};
  portfolioDispatchCoordinationErrors: Record<string, string> = {};
  portfolioDispatchCoordinationLoadingId = '';
  portfolioDispatchCoordinationBatchLoading = false;
  portfolioDispatchSelections: Record<string, Record<string, boolean>> = {};
  portfolioDispatchConfirmations: Record<string, string> = {};
  portfolioDispatchResults: Record<string, IPursuitPortfolioDispatchResult> = {};
  portfolioDispatchErrors: Record<string, string> = {};
  portfolioDispatchPendingId = '';
  portfolioDispatchRunningId = '';
  portfolioWorkflowAuthorizationConfirmations: Record<string, string> = {};
  portfolioWorkflowAuthorizationResults: Record<string, IPursuitPortfolioWorkflowEffectAuthorizationResult> = {};
  portfolioWorkflowAuthorizationErrors: Record<string, string> = {};
  portfolioWorkflowAuthorizationPendingId = '';
  portfolioWorkflowAuthorizationRunningId = '';
  portfolioWorkflowExecutionConfirmations: Record<string, string> = {};
  portfolioWorkflowExecutionResults: Record<string, IPursuitPortfolioWorkflowEffectExecutionResult> = {};
  portfolioWorkflowExecutionErrors: Record<string, string> = {};
  portfolioWorkflowExecutionPendingId = '';
  portfolioWorkflowExecutionRunningId = '';
  portfolioWorkflowRecords: Record<string, IWorkflowRecord> = {};
  portfolioWorkflowVerificationErrors: Record<string, string> = {};
  portfolioWorkflowVerificationLoading: Record<string, boolean> = {};
  portfolioWorkflowSettlementEffortMinutes: Record<string, number | null> = {};
  portfolioWorkflowSettlementCostMicros: Record<string, number | null> = {};
  portfolioWorkflowSettlementConfirmations: Record<string, string> = {};
  portfolioWorkflowSettlementResults: Record<string, IPursuitPortfolioWorkflowSettlementResult> = {};
  portfolioWorkflowSettlementErrors: Record<string, string> = {};
  portfolioWorkflowSettlementPendingId = '';
  portfolioWorkflowSettlementRunningId = '';
  portfolioAsOf = '';
  portfolioHorizonStart = '';
  portfolioHorizonEnd = '';
  portfolioDurationMode: 'expected' | 'conservative' = 'conservative';
  portfolioMaxCostEur = 0;
  portfolioDrafts: PortfolioPursuitDraft[] = [];

  readonly portfolioFactorFields: Array<{ key: PortfolioFactorKey; label: string; cost: boolean; hint: string }> = [
    { key: 'importance', label: 'Importance', cost: false, hint: 'How important is the outcome?' },
    { key: 'urgency', label: 'Urgency', cost: false, hint: 'How quickly must this move?' },
    { key: 'humanNeedAffected', label: 'Human need', cost: false, hint: 'How strongly does it affect a core life need?' },
    { key: 'deadlinePressure', label: 'Deadline pressure', cost: false, hint: 'How much pressure does the actual deadline create?' },
    { key: 'costOfDelay', label: 'Cost of delay', cost: false, hint: 'What is lost by waiting?' },
    { key: 'expectedValue', label: 'Expected value', cost: false, hint: 'How valuable is successful completion?' },
    { key: 'harmAvoided', label: 'Harm avoided', cost: false, hint: 'How much harm can timely work prevent?' },
    { key: 'probabilityOfSuccess', label: 'Success chance', cost: false, hint: 'How likely is this plan to succeed?' },
    { key: 'effort', label: 'Effort cost', cost: true, hint: 'How much effort will this consume?' },
    { key: 'duration', label: 'Duration cost', cost: true, hint: 'How long will completion take?' },
    { key: 'dependencies', label: 'Dependency cost', cost: true, hint: 'How constrained is this by dependencies?' },
    { key: 'reversibility', label: 'Reversibility', cost: false, hint: 'How easy is it to reverse course safely?' },
    { key: 'risk', label: 'Risk significance', cost: false, hint: 'How important is it to manage this risk?' },
    { key: 'legalObligation', label: 'Legal obligation', cost: false, hint: 'How strong is the legal or formal obligation?' },
    { key: 'relationshipConsequences', label: 'Relationship impact', cost: false, hint: 'How much can delay affect important relationships?' },
    { key: 'availableCapacity', label: 'Available capacity', cost: false, hint: 'How well does current capacity fit this work?' },
    { key: 'energyFit', label: 'Energy fit', cost: false, hint: 'How well does this fit current energy and attention?' },
    { key: 'opportunityCost', label: 'Opportunity cost', cost: true, hint: 'How much better work would this displace?' },
    { key: 'strategicAlignment', label: 'Strategic alignment', cost: false, hint: 'How directly does this support the desired direction?' },
    { key: 'learningValue', label: 'Learning value', cost: false, hint: 'How much reusable learning will it create?' },
    { key: 'compoundingValue', label: 'Compounding value', cost: false, hint: 'How much future value can this unlock?' },
    { key: 'staleness', label: 'Staleness', cost: false, hint: 'How long has this remained unattended?' },
    { key: 'commitmentAge', label: 'Commitment age', cost: false, hint: 'How long ago was this commitment made?' },
    { key: 'peopleBlocked', label: 'People blocked', cost: false, hint: 'How many people depend on this moving?' },
    { key: 'delegability', label: 'Delegability', cost: false, hint: 'How readily can safe parts be delegated?' },
  ];

  readonly domains = [
    ['legal_government', 'Legal and government'], ['emergency_continuity', 'Emergency and continuity'],
    ['health_wellbeing', 'Health and wellbeing'], ['financial', 'Financial'],
    ['work_venture', 'Work and ventures'], ['home_assets', 'Home and assets'],
    ['relationships_care', 'Relationships and care'], ['learning_growth', 'Learning and growth'],
    ['travel_mobility', 'Travel and mobility'], ['personal_productivity', 'Personal productivity'],
    ['identity_roles', 'Identity and roles'], ['family_household', 'Family and household'],
    ['food_nutrition', 'Food and nutrition'], ['communication_correspondence', 'Communication and correspondence'],
    ['digital_accounts', 'Digital accounts'], ['possessions_inventory', 'Possessions and inventory'],
    ['animals_dependants', 'Animals and dependants'], ['community_civic', 'Community and civic life'],
    ['leisure_recreation', 'Leisure and recreation'], ['creativity_expression', 'Creativity and expression'],
    ['meaning_values', 'Meaning and values'], ['environment_sustainability', 'Environment and sustainability'],
    ['legacy_long_term', 'Legacy and long term'], ['safety_security', 'Safety and security'],
  ];

  readonly linkTypes = [
    'pursuit',
    'workflow',
    'memory',
    'ai_conversation',
    'ambient_opportunity',
    'source_item',
    'source_extraction',
    'verification',
    'automation',
    'agent_runtime',
  ];

  createForm: FormGroup = this.fb.group({
    title: ['', [Validators.required]],
    description: [''],
    projectKey: [''],
    domain: ['personal_productivity'],
    whyItMatters: [''],
    desiredOutcome: [''],
    currentStateSummary: [''],
    completionDefinition: [''],
    successCriteriaText: [''],
    stopConditionsText: [''],
    dependenciesText: [''],
    targetAt: [''],
    reviewCadenceDays: [7],
    maxEffortHours: [0],
    maxSpendEur: [0],
    maxParallelWorkflows: [0],
    resourceNotes: [''],
  });

  contextForm: FormGroup = this.fb.group({
    description: [''],
    whyItMatters: [''],
    desiredOutcome: [''],
    currentStateSummary: [''],
    nextRecommendedAction: [''],
    completionDefinition: [''],
    targetAt: [''],
    reviewCadenceDays: [0],
    maxEffortHours: [0],
    maxSpendEur: [0],
    maxParallelWorkflows: [0],
    resourceNotes: [''],
  });

  intakeForm: FormGroup = this.fb.group({
    input: ['', [Validators.required]],
    sourceType: ['manual'],
    sourceId: [''],
    sourceUri: [''],
    sourceLabel: [''],
    sender: [''],
  });

  routedIntakeForm: FormGroup = this.fb.group({
    input: ['', [Validators.required]],
    projectKey: [''],
    sourceType: ['manual'],
    sourceLabel: [''],
    sourceUri: [''],
  });

  linkForm: FormGroup = this.fb.group({
    linkType: ['workflow', [Validators.required]],
    linkId: ['', [Validators.required]],
    relationship: ['related'],
    sourceUri: [''],
    sourceLabel: [''],
    confidence: [0.7],
  });

  resourceEventForm: FormGroup = this.fb.group({
    kind: ['effort_recorded', [Validators.required]],
    effortHours: [0.5],
    spendEur: [0],
    note: [''],
    evidenceUri: [''],
    occurredAt: [''],
    idempotencyKey: [this.newResourceIdempotencyKey(), [Validators.required]],
  });

  constructor(
    private fb: FormBuilder,
    private pursuitsService: PursuitService,
    private automationsService: AutomationsService,
    private workflowService: WorkflowService,
    private notification: NzNotificationService,
    private modal: NzModalService,
    private router: Router,
    private route: ActivatedRoute,
    private viewPreferences: ModuleViewPreferencesService,
  ) {}

  get isAdvancedView(): boolean {
    return this.viewPreferences.get(this.moduleId).mode === 'advanced';
  }

  get loadError(): string {
    return this.dashboardError || this.pursuitsError;
  }

  get hasLastKnownData(): boolean {
    return !!this.dashboard || this.pursuits.length > 0;
  }

  get showEmptyState(): boolean {
    return this.pursuits.length === 0 && !this.loading && !this.loadError;
  }

  ngOnInit(): void {
    this.routeSub = this.route.queryParamMap.subscribe((params) => {
      const selectedId = params.get('selected') || '';
      const evidenceUri = params.get('evidence') || '';
      const decisionId = params.get('decision') || '';
      if (params.get('create') === 'true' && !this.createIntentApplied) {
        this.createIntentApplied = true;
        this.showCreate = true;
        void this.router.navigate([], {
          relativeTo: this.route,
          queryParams: { create: null },
          queryParamsHandling: 'merge',
          replaceUrl: true,
        });
      }
      if (selectedId === this.requestedPursuitId && evidenceUri === this.requestedEvidenceUri && decisionId === this.highlightedDecisionId) {
        return;
      }
      this.requestedPursuitId = selectedId;
      this.requestedEvidenceUri = evidenceUri;
      this.highlightedDecisionId = decisionId;
      if (selectedId && this.pursuits.length) {
        this.selectPursuitById(selectedId, false);
        return;
      }
      if (evidenceUri && this.selected?.pursuit.id === selectedId) {
        this.openRequestedEvidence();
      }
    });
    this.load();
  }

  ngOnDestroy(): void {
    this.reservationConfirmationRevision++;
    this.reservationConfirmationId = '';
    this.reservationReleaseSub?.unsubscribe();
    this.resourceAppendSub?.unsubscribe();
    this.routedIntakeSub?.unsubscribe();
    this.intakeSub?.unsubscribe();
    this.contextSaveSub?.unsubscribe();
    this.linkSaveSub?.unsubscribe();
    this.linkConfirmationRevision++;
    this.linkConfirmationId = '';
    this.linkDetachSub?.unsubscribe();
    this.planningSub?.unsubscribe();
    this.reviewSub?.unsubscribe();
    this.summarySub?.unsubscribe();
    this.delegationSub?.unsubscribe();
    this.lifecycleSub?.unsubscribe();
    this.decisionMutationSub?.unsubscribe();
    this.evidenceResolutionSub?.unsubscribe();
    this.runtimeStopSub?.unsubscribe();
    this.routeSub?.unsubscribe();
    this.dashboardSub?.unsubscribe();
    this.pursuitsSub?.unsubscribe();
    this.pursuitDetailSub?.unsubscribe();
    this.decisionDetailSub?.unsubscribe();
    this.resourceEventsSub?.unsubscribe();
    this.projectDossierSub?.unsubscribe();
    this.portfolioAllocationHistorySub?.unsubscribe();
    this.portfolioExecutionProposalHistoryReadSub?.unsubscribe();
    this.portfolioExecutionProposalSub?.unsubscribe();
    this.portfolioExecutionProposalDecisionSub?.unsubscribe();
    this.portfolioDispatchCoordinationSub?.unsubscribe();
    this.portfolioDispatchCoordinationBatchSub?.unsubscribe();
    this.portfolioDispatchSub?.unsubscribe();
    this.portfolioWorkflowAuthorizationSub?.unsubscribe();
    this.portfolioWorkflowExecutionSub?.unsubscribe();
    this.portfolioWorkflowSettlementSub?.unsubscribe();
    this.portfolioWorkflowVerificationSubs.unsubscribe();
    this.portfolioExecutionProposalDecisionHistorySub.unsubscribe();
  }

  load(): void {
    this.dashboardSub?.unsubscribe();
    this.loading = true;
    this.dashboardError = '';
    this.pursuitsError = '';
    this.dashboardSub = this.pursuitsService.dashboard(!this.includeArchived).subscribe({
      next: (dashboard) => {
        this.dashboard = dashboard;
        if (!this.includeArchived && Array.isArray(dashboard.pursuits)) {
          this.applyPursuits(dashboard.pursuits);
          return;
        }
        this.loadPursuits();
      },
      error: (error) => {
        this.loading = false;
        this.dashboardError = error?.error?.error || 'Failed to load pursuit dashboard.';
        this.notification.error('Pursuits unavailable', this.dashboardError);
      },
    });
  }

  loadPursuits(): void {
    this.pursuitsSub?.unsubscribe();
    this.loading = true;
    this.pursuitsError = '';
    this.pursuitsSub = this.pursuitsService.list(this.includeArchived).subscribe({
      next: (pursuits) => {
        this.applyPursuits(pursuits || []);
      },
      error: (error) => {
        this.loading = false;
        this.pursuitsError = error?.error?.error || 'Failed to load pursuits.';
        this.notification.error('Pursuits unavailable', this.pursuitsError);
      },
    });
  }

  reconcileLifeDomains(): void {
    if (this.lifeDomainReconciliationRunning) {
      return;
    }
    this.lifeDomainReconciliationRunning = true;
    this.pursuitsService.reconcileLifeDomains().subscribe({
      next: (result) => {
        this.lifeDomainReconciliationRunning = false;
        this.lifeDomainReconciliation = result;
        const detail = result.failed
          ? `${result.projected} indexed, ${result.failed} need review.`
          : `${result.projected} indexed; ${result.skipped} unchanged.`;
        this.notification.success('Life domains updated', detail);
        this.load();
      },
      error: (error) => {
        this.lifeDomainReconciliationRunning = false;
        this.notification.error(
          'Life-domain update unavailable',
          error?.error?.error || 'HAI could not update the life-domain index.',
        );
      },
    });
  }

  private applyPursuits(pursuits: IPursuit[]): void {
    this.pursuits = pursuits;
    this.pursuitsError = '';
    this.loading = false;
    if (this.requestedPursuitId) {
      this.selectPursuitById(this.requestedPursuitId, false);
      return;
    }
    if (!this.selected && this.pursuits.length) {
      this.selectPursuit(this.pursuits[0]);
      return;
    }
    if (this.selected) {
      const refreshed = this.pursuits.find((item) => item.id === this.selected?.pursuit.id);
      if (refreshed) {
        this.selectPursuit(refreshed);
      }
    }
  }

  openPortfolioPlanner(): void {
    const prior = new Map(this.portfolioDrafts.map((draft) => [draft.pursuit.id, draft]));
    this.portfolioDrafts = this.pursuits
      .filter((pursuit) => !pursuit.archived && pursuit.status !== 'completed')
      .map((pursuit) => prior.get(pursuit.id) || this.newPortfolioDraft(pursuit));
    this.portfolioPlanningError = '';
    this.portfolioAcceptanceError = '';
    this.portfolioPlannerVisible = true;
    this.loadPortfolioAllocationHistory();
  }

  loadPortfolioAllocationHistory(force: boolean = false): void {
    if (this.portfolioAllocationHistoryLoading || (!force && this.portfolioAllocationHistoryLoaded)) {
      return;
    }
    this.portfolioAllocationHistorySub?.unsubscribe();
    this.portfolioAllocationHistoryLoading = true;
    this.portfolioAllocationHistoryError = '';
    this.portfolioAllocationHistorySub = this.pursuitsService.portfolioAllocations(20).subscribe({
      next: (records) => {
        this.portfolioAllocationHistoryLoading = false;
        if (!Array.isArray(records) || records.some((record) => !this.validPortfolioAllocationRecord(record))) {
          this.portfolioAllocationHistoryLoaded = false;
          this.portfolioAllocationHistoryError = 'HAI rejected allocation history that violated its read-only authority or immutable record contract.';
          return;
        }
        this.portfolioAllocationHistory = this.reconciledPortfolioAllocationHistory(records);
        this.portfolioAllocationHistoryLoaded = true;
        this.loadPortfolioExecutionProposalHistory(this.portfolioAllocationHistory);
      },
      error: (error) => {
        this.portfolioAllocationHistoryLoading = false;
        this.portfolioAllocationHistoryLoaded = false;
        this.portfolioAllocationHistoryError = error?.error?.error || 'Recent portfolio allocations could not be loaded.';
      },
    });
  }

  private loadPortfolioExecutionProposalHistory(
    allocations: IPursuitPortfolioAllocationAcceptanceResult[],
  ): void {
    const allocationIds = allocations.map((record) => record.allocation.id);
    this.portfolioExecutionProposalHistoryReadSub?.unsubscribe();
    if (!allocationIds.length) {
      return;
    }
    this.portfolioExecutionProposalHistoryReadSub = this.pursuitsService.portfolioExecutionProposals(allocationIds).subscribe({
      next: (results) => {
        const allocationsById = new Map(allocations.map((record) => [record.allocation.id, record]));
        const seen = new Set<string>();
        if (!Array.isArray(results) || results.length > allocations.length || results.some((result) => {
          const allocationId = result?.proposal?.allocationId;
          const allocation = allocationsById.get(allocationId);
          if (!allocation || seen.has(allocationId) || !this.validPortfolioExecutionProposalResult(result, allocation)) {
            return true;
          }
          seen.add(allocationId);
          return false;
        })) {
          for (const allocationId of allocationIds) {
            this.portfolioExecutionProposalErrors[allocationId] =
              'HAI rejected recovered proposals that violated immutable owner or proposal-only boundaries.';
          }
          return;
        }
        for (const allocationId of allocationIds) {
          delete this.portfolioExecutionProposals[allocationId];
          delete this.portfolioExecutionProposalErrors[allocationId];
        }
        for (const result of results) {
          this.portfolioExecutionProposals[result.proposal.allocationId] = result;
        }
        this.loadPortfolioDispatchCoordinationBatch(results);
        this.loadPortfolioExecutionProposalDecisionHistories(results.flatMap((result) => result.items));
      },
      error: (error) => {
        const message = error?.error?.error || 'Prepared execution proposals could not be restored.';
        for (const allocationId of allocationIds) {
          this.portfolioExecutionProposalErrors[allocationId] = message;
        }
      },
    });
  }

  closePortfolioPlanner(): void {
    if (
      !this.portfolioPlanningRunning
      && !this.portfolioAcceptancePending
      && !this.portfolioAcceptanceRunning
      && !this.portfolioExecutionProposalPendingId
      && !this.portfolioExecutionProposalRunningId
      && !this.portfolioDispatchPendingId
      && !this.portfolioDispatchRunningId
    ) {
      this.portfolioPlannerVisible = false;
    }
  }

  resetPortfolioPlanner(): void {
    this.portfolioHorizonStart = '';
    this.portfolioHorizonEnd = '';
    this.portfolioDurationMode = 'conservative';
    this.portfolioMaxCostEur = 0;
    this.portfolioPlanningError = '';
    this.portfolioAcceptanceError = '';
    this.portfolioResult = undefined;
    this.portfolioPlanningRequest = undefined;
    this.portfolioAllocationResult = undefined;
    this.portfolioAcceptancePending = false;
    this.portfolioDrafts = this.pursuits
      .filter((pursuit) => !pursuit.archived && pursuit.status !== 'completed')
      .map((pursuit) => this.newPortfolioDraft(pursuit));
  }

  resetPortfolioFactors(draft: PortfolioPursuitDraft): void {
    draft.factors = this.neutralPortfolioFactors();
    draft.factorsReviewed = false;
  }

  portfolioSelectedCount(): number {
    return this.portfolioDrafts.filter((draft) => draft.selected).length;
  }

  portfolioValidationMessage(): string {
    const selected = this.portfolioDrafts.filter((draft) => draft.selected);
    if (!selected.length) {
      return 'Select at least one pursuit.';
    }
    const start = new Date(this.portfolioHorizonStart);
    const end = new Date(this.portfolioHorizonEnd);
    if (!this.portfolioHorizonStart || !this.portfolioHorizonEnd || Number.isNaN(start.getTime()) || Number.isNaN(end.getTime()) || end <= start) {
      return 'Enter one valid owner-capacity window with an end after its start.';
    }
    if (this.portfolioMaxCostEur < 0 || !Number.isFinite(Number(this.portfolioMaxCostEur))) {
      return 'The maximum planned cost must be zero or greater.';
    }
    for (const draft of selected) {
      const optimistic = Number(draft.optimisticMinutes);
      const expected = Number(draft.expectedMinutes);
      const pessimistic = Number(draft.pessimisticMinutes);
      if (!Number.isFinite(optimistic) || !Number.isFinite(expected) || !Number.isFinite(pessimistic) || optimistic <= 0 || optimistic > expected || expected > pessimistic) {
        return `${draft.pursuit.title}: enter positive optimistic, expected, and pessimistic minutes in ascending order.`;
      }
      if (!draft.factorsReviewed) {
        return `${draft.pursuit.title}: review and confirm all priority factors.`;
      }
      if (this.portfolioFactorFields.some((field) => {
        const value = Number(draft.factors[field.key]);
        return !Number.isFinite(value) || value < 0 || value > 100;
      })) {
        return `${draft.pursuit.title}: every priority factor must be between 0 and 100.`;
      }
      if (draft.estimatedCostEur < 0 || draft.inputTokens < 0 || draft.outputTokens < 0 || draft.toolCalls < 0) {
        return `${draft.pursuit.title}: usage estimates cannot be negative.`;
      }
      if (draft.calibration && !this.portfolioCalibrationDraftMatches(draft)) {
        return `${draft.pursuit.title}: the reviewed estimate was edited. Clear it or apply the reviewed suggestion again.`;
      }
    }
    return '';
  }

  planPortfolio(): void {
    if (this.portfolioPlanningRunning || this.portfolioAcceptanceRunning || this.portfolioAcceptancePending) {
      return;
    }
    const validation = this.portfolioValidationMessage();
    if (validation) {
      this.portfolioPlanningError = validation;
      return;
    }
    const asOf = new Date();
    const start = new Date(this.portfolioHorizonStart);
    const end = new Date(this.portfolioHorizonEnd);
    const selected = this.portfolioDrafts.filter((draft) => draft.selected);
    const request: IPursuitPortfolioPlanningRequest = {
      planId: this.newPortfolioPlanId(),
      asOf: asOf.toISOString(),
      horizonStart: start.toISOString(),
      horizonEnd: end.toISOString(),
      durationMode: this.portfolioDurationMode,
      availability: [{ start: start.toISOString(), end: end.toISOString() }],
      pursuits: selected.map((draft) => ({
        pursuitId: draft.pursuit.id,
        duration: {
          optimisticMinutes: Math.trunc(Number(draft.optimisticMinutes)),
          expectedMinutes: Math.trunc(Number(draft.expectedMinutes)),
          pessimisticMinutes: Math.trunc(Number(draft.pessimisticMinutes)),
          basis: draft.estimateBasis.trim(),
        },
        estimatedUsage: {
          costMicros: Math.round(Number(draft.estimatedCostEur || 0) * 1_000_000),
          inputTokens: Math.trunc(Number(draft.inputTokens || 0)),
          outputTokens: Math.trunc(Number(draft.outputTokens || 0)),
          toolCalls: Math.trunc(Number(draft.toolCalls || 0)),
        },
        factors: { ...draft.factors },
        calibration: draft.calibration,
        optional: draft.optional,
      })),
      budget: { maxCostMicros: Math.round(Number(this.portfolioMaxCostEur || 0) * 1_000_000) },
      approvalPolicy: {
        costThresholdMicros: 0,
        uncertaintyThresholdPct: 50,
        softDeadlineMiss: true,
      },
    };
    this.portfolioAsOf = request.asOf;
    this.portfolioPlanningRequest = request;
    this.portfolioPlanningRunning = true;
    this.portfolioPlanningError = '';
    this.portfolioAcceptanceError = '';
    this.portfolioResult = undefined;
    this.portfolioAllocationResult = undefined;
    this.pursuitsService.planPortfolio(request).subscribe({
      next: (result) => {
        this.portfolioPlanningRunning = false;
        if (
          result.authority !== 'advisory_only'
          || result.canExecute !== false
          || result.planId !== request.planId
          || (!!result.decision && result.decision.planId !== request.planId)
          || (!!result.decision && result.capacity?.status !== 'applied')
          || result.decision?.canExecute === true
          || result.decision?.grantsAuthority === true
          || !this.validPortfolioCalibrationResponse(result, request)
        ) {
          this.portfolioPlanningError = 'HAI rejected a portfolio response that claimed execution authority.';
          return;
        }
        this.portfolioResult = result;
        if (result.capacity && result.capacity.status !== 'applied') {
          this.notification.warning('Capacity check-in required', result.capacity.reason);
        } else {
          this.notification.success('Advisory portfolio ready', 'Review the schedule, exclusions, and approval flags before changing any work.');
        }
      },
      error: (error) => {
        this.portfolioPlanningRunning = false;
        this.portfolioPlanningError = error?.error?.error || 'The advisory portfolio could not be calculated.';
      },
    });
  }

  openCapacityWorkspace(): void {
    this.portfolioPlannerVisible = false;
    this.router.navigate(['/life-ops']);
  }

  applyPortfolioCalibration(calibration: IPursuitPortfolioEstimateCalibrationRecommendation): void {
    const draft = this.portfolioDrafts.find((candidate) => candidate.pursuit.id === calibration?.pursuitId);
    const sourceCostMicros = Number(calibration?.sourceCostMicros || 0);
    const suggestedCostMicros = Number(calibration?.suggestedCostMicros || 0);
    const sourceOptimistic = Number(calibration?.sourceOptimisticMinutes);
    const sourceExpected = Number(calibration?.sourceExpectedMinutes);
    const sourcePessimistic = Number(calibration?.sourcePessimisticMinutes);
    const suggestedOptimistic = Number(calibration?.suggestedOptimisticMinutes);
    const suggestedExpected = Number(calibration?.suggestedExpectedMinutes);
    const suggestedPessimistic = Number(calibration?.suggestedPessimisticMinutes);
    if (
      !draft
      || calibration.status !== 'available'
      || calibration.applied
      || !calibration.scopeKey
      || !calibration.proposalId
      || !calibration.proposalVersion
      || !calibration.applicationId
      || !/^sha256:[0-9a-f]{64}$/i.test(String(calibration.evidenceDigest || ''))
      || Math.trunc(Number(draft.optimisticMinutes)) !== sourceOptimistic
      || Math.trunc(Number(draft.expectedMinutes)) !== sourceExpected
      || Math.trunc(Number(draft.pessimisticMinutes)) !== sourcePessimistic
      || Math.round(Number(draft.estimatedCostEur || 0) * 1_000_000) !== sourceCostMicros
      || ![sourceOptimistic, sourceExpected, sourcePessimistic, suggestedOptimistic, suggestedExpected, suggestedPessimistic, sourceCostMicros, suggestedCostMicros]
        .every((value) => Number.isSafeInteger(value) && value >= 0)
      || suggestedOptimistic <= 0
      || suggestedOptimistic > suggestedExpected
      || suggestedExpected > suggestedPessimistic
    ) {
      this.notification.error('Suggestion not applied', 'The source estimate changed or the reviewed calibration evidence is incomplete. Recalculate the plan.');
      return;
    }
    const sourceDuration = {
      optimisticMinutes: sourceOptimistic,
      expectedMinutes: sourceExpected,
      pessimisticMinutes: sourcePessimistic,
      basis: draft.estimateBasis.trim(),
    };
    const sourceEstimatedUsage = {
      costMicros: sourceCostMicros,
      inputTokens: Math.trunc(Number(draft.inputTokens || 0)),
      outputTokens: Math.trunc(Number(draft.outputTokens || 0)),
      toolCalls: Math.trunc(Number(draft.toolCalls || 0)),
    };
    draft.calibration = {
      scopeKey: calibration.scopeKey,
      proposalId: calibration.proposalId,
      proposalVersion: calibration.proposalVersion,
      applicationId: calibration.applicationId,
      evidenceDigest: calibration.evidenceDigest as string,
      sourceDuration,
      sourceEstimatedUsage,
    };
    draft.calibrationExpected = {
      optimisticMinutes: suggestedOptimistic,
      expectedMinutes: suggestedExpected,
      pessimisticMinutes: suggestedPessimistic,
      costMicros: suggestedCostMicros,
    };
    draft.optimisticMinutes = suggestedOptimistic;
    draft.expectedMinutes = suggestedExpected;
    draft.pessimisticMinutes = suggestedPessimistic;
    draft.estimatedCostEur = suggestedCostMicros / 1_000_000;
    draft.estimateBasis = `Owner-approved calibration ${calibration.proposalVersion} from ${calibration.sampleCount || 0} verified settlements`;
    this.portfolioResult = undefined;
    this.portfolioPlanningRequest = undefined;
    this.portfolioAllocationResult = undefined;
    this.portfolioAcceptanceError = '';
    this.notification.success('Reviewed estimate copied', 'Recalculate the portfolio to bind this exact calibration revision. No work was approved or executed.');
  }

  clearPortfolioCalibration(draft: PortfolioPursuitDraft): void {
    draft.calibration = undefined;
    draft.calibrationExpected = undefined;
    this.portfolioResult = undefined;
    this.portfolioPlanningRequest = undefined;
    this.portfolioAllocationResult = undefined;
  }

  private portfolioCalibrationDraftMatches(draft: PortfolioPursuitDraft): boolean {
    const expected = draft.calibrationExpected;
    return !draft.calibration || !!expected
      && Math.trunc(Number(draft.optimisticMinutes)) === expected.optimisticMinutes
      && Math.trunc(Number(draft.expectedMinutes)) === expected.expectedMinutes
      && Math.trunc(Number(draft.pessimisticMinutes)) === expected.pessimisticMinutes
      && Math.round(Number(draft.estimatedCostEur || 0) * 1_000_000) === expected.costMicros;
  }

  private validPortfolioCalibrationResponse(
    result: IPursuitPortfolioPlanningResult,
    request: IPursuitPortfolioPlanningRequest,
  ): boolean {
    const calibrations = result.calibrations || [];
    if (!Array.isArray(calibrations)) {
      return false;
    }
    const inputs = new Map(request.pursuits.map((input) => [input.pursuitId, input]));
    const seen = new Set<string>();
    return calibrations.every((calibration) => {
      const input = inputs.get(calibration.pursuitId);
      if (!input || seen.has(calibration.pursuitId) || !['available', 'bound', 'unavailable'].includes(calibration.status)) {
        return false;
      }
      seen.add(calibration.pursuitId);
      if (calibration.status === 'unavailable') {
        return calibration.applied === false;
      }
      const source = [
        Number(calibration.sourceOptimisticMinutes), Number(calibration.sourceExpectedMinutes),
        Number(calibration.sourcePessimisticMinutes), Number(calibration.sourceCostMicros || 0),
      ];
      const suggested = [
        Number(calibration.suggestedOptimisticMinutes), Number(calibration.suggestedExpectedMinutes),
        Number(calibration.suggestedPessimisticMinutes), Number(calibration.suggestedCostMicros || 0),
      ];
      const evidenceValid = !!calibration.scopeKey && !!calibration.proposalId && !!calibration.proposalVersion
        && !!calibration.applicationId && /^sha256:[0-9a-f]{64}$/i.test(String(calibration.evidenceDigest || ''))
        && Number(calibration.sampleCount) >= 3 && Number(calibration.confidence) >= 0
        && Number(calibration.confidence) <= 1;
      if (!evidenceValid || ![...source, ...suggested].every((value) => Number.isSafeInteger(value) && value >= 0)) {
        return false;
      }
      if (calibration.status === 'available') {
        return calibration.applied === false && !input.calibration
          && source[0] === input.duration.optimisticMinutes
          && source[1] === input.duration.expectedMinutes
          && source[2] === input.duration.pessimisticMinutes
          && source[3] === input.estimatedUsage.costMicros;
      }
      return calibration.applied === true && !!input.calibration
        && input.calibration.scopeKey === calibration.scopeKey
        && input.calibration.proposalId === calibration.proposalId
        && input.calibration.proposalVersion === calibration.proposalVersion
        && input.calibration.applicationId === calibration.applicationId
        && input.calibration.evidenceDigest === calibration.evidenceDigest
        && suggested[0] === input.duration.optimisticMinutes
        && suggested[1] === input.duration.expectedMinutes
        && suggested[2] === input.duration.pessimisticMinutes
        && suggested[3] === input.estimatedUsage.costMicros;
    });
  }

  canAcceptPortfolioAllocation(): boolean {
    return !this.portfolioAcceptancePending
      && !this.portfolioAcceptanceRunning
      && !this.portfolioAllocationResult
      && !!this.portfolioAcceptanceContract();
  }

  portfolioAllocationApprovalCount(record: IPursuitPortfolioAllocationAcceptanceResult): number {
    return record.items.filter((item) => item.requiresApproval).length;
  }

  canPreparePortfolioExecutionProposals(record: IPursuitPortfolioAllocationAcceptanceResult): boolean {
    return !this.portfolioExecutionProposalPendingId
      && !this.portfolioExecutionProposalRunningId
      && this.validPortfolioAllocationRecord(record);
  }

  preparePortfolioExecutionProposals(record: IPursuitPortfolioAllocationAcceptanceResult): void {
    if (!this.canPreparePortfolioExecutionProposals(record)) {
      return;
    }
    const contract = {
      allocationId: record.allocation.id,
      allocationRecordDigest: record.allocation.recordDigest,
    };
    this.portfolioExecutionProposalPendingId = contract.allocationId;
    delete this.portfolioExecutionProposalErrors[contract.allocationId];
    this.modal.confirm({
      nzTitle: 'Prepare immutable execution proposals?',
      nzContent: 'HAI will snapshot proposed actions and their current safety gates. This does not approve, queue, or execute any work.',
      nzOkText: 'Prepare proposals',
      nzCancelText: 'Keep allocation only',
      nzOnCancel: () => {
        if (this.portfolioExecutionProposalPendingId === contract.allocationId) {
          this.portfolioExecutionProposalPendingId = '';
        }
      },
      nzOnOk: () => this.submitPortfolioExecutionProposals(contract),
    });
  }

  portfolioExecutionProposalStatusColor(status: IPursuitPortfolioExecutionProposalItem['status']): string {
    if (status === 'blocked') {
      return 'red';
    }
    return status === 'needs_approval' ? 'gold' : 'green';
  }

  loadPortfolioDispatchCoordination(proposal: IPursuitPortfolioExecutionProposalResult): void {
    const proposalId = proposal?.proposal?.id;
    if (!proposalId || this.portfolioDispatchCoordinationLoadingId) {
      return;
    }
    this.portfolioDispatchCoordinationLoadingId = proposalId;
    delete this.portfolioDispatchCoordinationErrors[proposalId];
    delete this.portfolioDispatchCoordination[proposalId];
    this.portfolioDispatchSelections[proposalId] = {};
    this.portfolioDispatchConfirmations[proposalId] = '';
    this.portfolioDispatchCoordinationSub?.unsubscribe();
    this.portfolioDispatchCoordinationSub = this.pursuitsService.portfolioDispatchCoordination(proposalId).subscribe({
      next: (result) => {
        this.portfolioDispatchCoordinationLoadingId = '';
        if (!this.validPortfolioDispatchCoordination(result, proposal)) {
          this.portfolioDispatchCoordinationErrors[proposalId] =
            'HAI rejected coordination data that did not match the immutable proposal and approval boundaries.';
          return;
        }
        this.portfolioDispatchCoordination[proposalId] = result;
        const prior = this.portfolioDispatchSelections[proposalId] || {};
        this.portfolioDispatchSelections[proposalId] = Object.fromEntries(
          result.items.map((entry) => [entry.item.id, entry.selectable && prior[entry.item.id] === true]),
        );
        for (const entry of result.items) {
          if (entry.latestDispatch?.workflowId) {
            this.refreshPortfolioWorkflowVerification(entry.item);
          }
        }
      },
      error: (error) => {
        this.portfolioDispatchCoordinationLoadingId = '';
        this.portfolioDispatchCoordinationErrors[proposalId] =
          error?.error?.error || 'Portfolio coordination could not be refreshed.';
      },
    });
  }

  private loadPortfolioDispatchCoordinationBatch(
    proposals: IPursuitPortfolioExecutionProposalResult[],
  ): void {
    this.portfolioDispatchCoordinationBatchSub?.unsubscribe();
    if (!proposals.length) {
      this.portfolioDispatchCoordinationBatchLoading = false;
      return;
    }
    const proposalIDs = proposals.map((proposal) => proposal.proposal.id);
    const proposalsByID = new Map(proposals.map((proposal) => [proposal.proposal.id, proposal]));
    this.portfolioDispatchCoordinationBatchLoading = true;
    for (const proposalID of proposalIDs) {
      delete this.portfolioDispatchCoordination[proposalID];
      delete this.portfolioDispatchCoordinationErrors[proposalID];
      this.portfolioDispatchSelections[proposalID] = {};
      this.portfolioDispatchConfirmations[proposalID] = '';
    }
    this.portfolioDispatchCoordinationBatchSub = this.pursuitsService.portfolioDispatchCoordinations(proposalIDs).subscribe({
      next: (results) => {
        this.portfolioDispatchCoordinationBatchLoading = false;
        const seen = new Set<string>();
        if (!Array.isArray(results) || results.length > proposals.length || results.some((result) => {
          const proposal = proposalsByID.get(result?.proposal?.id);
          if (!proposal || seen.has(result.proposal.id) || !this.validPortfolioDispatchCoordination(result, proposal)) {
            return true;
          }
          seen.add(result.proposal.id);
          return false;
        })) {
          for (const proposalID of proposalIDs) {
            this.portfolioDispatchCoordinationErrors[proposalID] =
              'HAI rejected recovered coordination that crossed immutable owner, approval, or proposal boundaries.';
          }
          return;
        }
        for (const result of results) {
          const proposalID = result.proposal.id;
          this.portfolioDispatchCoordination[proposalID] = result;
          this.portfolioDispatchSelections[proposalID] = Object.fromEntries(
            result.items.map((entry) => [entry.item.id, false]),
          );
          for (const entry of result.items) {
            if (entry.latestDispatch?.workflowId) {
              this.refreshPortfolioWorkflowVerification(entry.item);
            }
          }
        }
        for (const proposalID of proposalIDs) {
          if (!seen.has(proposalID)) {
            this.portfolioDispatchCoordinationErrors[proposalID] =
              'Current coordination is unavailable for this recovered proposal.';
          }
        }
      },
      error: (error) => {
        this.portfolioDispatchCoordinationBatchLoading = false;
        const message = error?.error?.error || 'Current portfolio coordination could not be restored.';
        for (const proposalID of proposalIDs) {
          this.portfolioDispatchCoordinationErrors[proposalID] = message;
        }
      },
    });
  }

  portfolioDispatchSelectedCount(proposalId: string): number {
    return Object.values(this.portfolioDispatchSelections[proposalId] || {}).filter(Boolean).length;
  }

  portfolioDispatchEligibilityColor(eligibility: string): string {
    if (eligibility === 'eligible' || eligibility === 'dispatched') {
      return 'green';
    }
    if (eligibility === 'needs_approval' || eligibility === 'stale') {
      return 'gold';
    }
    return eligibility === 'blocked' || eligibility === 'failed' ? 'red' : 'default';
  }

  portfolioDispatchItemTitle(coordination: IPursuitPortfolioCoordinationResult, itemId: string): string {
    const pursuitId = coordination.items.find((entry) => entry.item.id === itemId)?.item.pursuitId || '';
    return this.portfolioPursuitTitle(pursuitId);
  }

  canDispatchPortfolioWorkflows(proposal: IPursuitPortfolioExecutionProposalResult): boolean {
    const proposalId = proposal.proposal.id;
    const coordination = this.portfolioDispatchCoordination[proposalId];
    return !!coordination
      && coordination.proposal.recordDigest === proposal.proposal.recordDigest
      && this.portfolioDispatchSelectedCount(proposalId) > 0
      && !this.portfolioDispatchPendingId
      && !this.portfolioDispatchRunningId
      && String(this.portfolioDispatchConfirmations[proposalId] || '').trim()
        === 'DISPATCH APPROVED PORTFOLIO WORKFLOWS';
  }

  dispatchPortfolioWorkflows(proposal: IPursuitPortfolioExecutionProposalResult): void {
    if (!this.canDispatchPortfolioWorkflows(proposal)) {
      this.portfolioDispatchErrors[proposal.proposal.id] =
        'Select at least one currently eligible item and enter the exact dispatch phrase.';
      return;
    }
    const proposalId = proposal.proposal.id;
    const coordination = this.portfolioDispatchCoordination[proposalId];
    const selected = coordination.items.filter((entry) => (
      entry.selectable && this.portfolioDispatchSelections[proposalId]?.[entry.item.id] === true
    ));
    const contract = {
      proposalId,
      proposalDigest: proposal.proposal.recordDigest,
      items: selected.map((entry) => ({
        proposalItemId: entry.item.id,
        expectedItemDigest: entry.item.recordDigest,
        expectedDecisionDigest: entry.decision?.recordDigest || '',
      })),
      confirmation: 'DISPATCH APPROVED PORTFOLIO WORKFLOWS' as const,
    };
    if (contract.items.some((entry) => !this.validPortfolioDigest(entry.expectedDecisionDigest))) {
      this.portfolioDispatchErrors[proposalId] = 'Every selected item needs one current exact approval decision.';
      return;
    }
    this.portfolioDispatchPendingId = proposalId;
    delete this.portfolioDispatchErrors[proposalId];
    this.modal.confirm({
      nzTitle: `Create ${contract.items.length} review-gated workflow${contract.items.length === 1 ? '' : 's'}?`,
      nzContent: 'HAI will independently revalidate each selected owner approval, issue and consume one exact authorization receipt per eligible item, and create local workflows that still require downstream review. It will not run workflows, contact anyone, settle reservations, or perform external actions.',
      nzOkText: 'Create selected workflows',
      nzCancelText: 'Cancel',
      nzOnCancel: () => {
        if (this.portfolioDispatchPendingId === proposalId) {
          this.portfolioDispatchPendingId = '';
        }
      },
      nzOnOk: () => this.submitPortfolioDispatch(contract, proposal),
    });
  }

  private submitPortfolioDispatch(
    contract: {
      proposalId: string;
      proposalDigest: string;
      items: Array<{ proposalItemId: string; expectedItemDigest: string; expectedDecisionDigest: string }>;
      confirmation: 'DISPATCH APPROVED PORTFOLIO WORKFLOWS';
    },
    proposal: IPursuitPortfolioExecutionProposalResult,
  ): void {
    if (this.portfolioDispatchRunningId) {
      return;
    }
    const current = this.portfolioDispatchCoordination[contract.proposalId];
    if (!current || current.proposal.recordDigest !== contract.proposalDigest ||
      proposal.proposal.recordDigest !== contract.proposalDigest || contract.items.length < 1 || contract.items.length > 20 ||
      contract.items.some((selected) => {
        const entry = current.items.find((candidate) => candidate.item.id === selected.proposalItemId);
        return !entry?.selectable || entry.item.recordDigest !== selected.expectedItemDigest ||
          entry.decision?.recordDigest !== selected.expectedDecisionDigest;
      })) {
      this.portfolioDispatchPendingId = '';
      this.portfolioDispatchErrors[contract.proposalId] =
        'The proposal, selection, or approval changed. Refresh coordination before dispatching.';
      return;
    }
    this.portfolioDispatchPendingId = '';
    this.portfolioDispatchRunningId = contract.proposalId;
    delete this.portfolioDispatchErrors[contract.proposalId];
    this.portfolioDispatchSub?.unsubscribe();
    this.portfolioDispatchSub = this.pursuitsService.dispatchPortfolioWorkflows(contract.proposalId, {
      expectedProposalDigest: contract.proposalDigest,
      items: contract.items,
      confirmation: contract.confirmation,
    }).subscribe({
      next: (result) => {
        this.portfolioDispatchRunningId = '';
        if (!this.validPortfolioDispatchResult(result, contract, proposal)) {
          this.portfolioDispatchErrors[contract.proposalId] =
            'HAI rejected a dispatch response that did not match the exact selected items and non-execution authority.';
          return;
        }
        this.portfolioDispatchResults[contract.proposalId] = result;
        this.portfolioDispatchConfirmations[contract.proposalId] = '';
        this.portfolioDispatchSelections[contract.proposalId] = {};
        this.loadPortfolioDispatchCoordination(proposal);
        const message = `${result.created} created, ${result.replayed} recovered, ${result.needsReview} need review, ${result.failed} failed. No workflow was run.`;
        if (result.failed > 0) {
          this.notification.warning('Portfolio dispatch needs recovery', message);
        } else if (result.needsReview > 0) {
          this.notification.warning('Portfolio dispatch needs decisions', message);
        } else {
          this.notification.success(result.resumed ? 'Portfolio dispatch recovered' : 'Review-gated workflows created', message);
        }
      },
      error: (error) => {
        this.portfolioDispatchRunningId = '';
        this.portfolioDispatchErrors[contract.proposalId] =
          error?.error?.error || 'The selected portfolio workflows could not be coordinated.';
      },
    });
  }

  private validPortfolioDispatchCoordination(
    result: IPursuitPortfolioCoordinationResult,
    proposal: IPursuitPortfolioExecutionProposalResult,
  ): boolean {
    if (result?.authority !== 'coordination_preview_only' || result?.canExecute !== false ||
      result?.proposal?.id !== proposal.proposal.id || result.proposal.recordDigest !== proposal.proposal.recordDigest ||
      !Array.isArray(result.items) || result.items.length !== proposal.items.length ||
      !Array.isArray(result.dispatchRuns) || result.dispatchRuns.length > 10 ||
      result?.freshness?.status !== 'current_coordination_snapshot' ||
      result.freshness.revalidationRequired !== true || Number.isNaN(new Date(result.freshness.checkedAt || '').getTime()) ||
      !String(result.freshness.reason || '').trim() ||
      [result.eligible, result.needsApproval, result.blocked, result.stale, result.dispatched]
        .some((value) => !Number.isSafeInteger(value) || value < 0) ||
      result.eligible + result.needsApproval + result.blocked + result.stale + result.dispatched !== result.items.length) {
      return false;
    }
    const proposalItems = new Map(proposal.items.map((item) => [item.id, item]));
    const resultItemIDs = new Set(result.items.map((entry) => entry?.item?.id));
    if (resultItemIDs.size !== result.items.length || resultItemIDs.size !== proposalItems.size ||
      !result.dispatchRuns.every((run) => run?.proposalId === proposal.proposal.id &&
        run.proposalDigest === proposal.proposal.recordDigest && this.validPortfolioRecordId(run.id) &&
        this.validPortfolioDigest(run.recordDigest) && this.validPortfolioDigest(run.requestDigest) &&
        this.validPortfolioDigest(run.selectedItemsDigest) && Array.isArray(run.selectedItemIds) &&
        run.confirmation === 'DISPATCH APPROVED PORTFOLIO WORKFLOWS' &&
        !Number.isNaN(new Date(run.requestedAt || '').getTime()))) {
      return false;
    }
    const itemsValid = result.items.every((entry) => {
      const expected = proposalItems.get(entry?.item?.id);
      const eligibilityValid = ['eligible', 'dispatched', 'needs_approval', 'blocked', 'stale', 'failed', 'cancelled']
        .includes(String(entry?.eligibility || ''));
      return !!expected && entry.item.recordDigest === expected.recordDigest && entry.item.proposalId === proposal.proposal.id &&
        eligibilityValid && !!String(entry.reason || '').trim() && entry.selectable === (entry.eligibility === 'eligible') &&
        (!entry.decision || this.validPortfolioExecutionProposalDecisionRecord(entry.decision, expected)) &&
        (!entry.latestDispatch || this.validPortfolioDispatchItemResult(entry.latestDispatch, expected));
    });
    if (!itemsValid) {
      return false;
    }
    const actual = result.items.reduce((counts, entry) => {
      if (entry.eligibility === 'eligible') counts.eligible++;
      else if (entry.eligibility === 'dispatched') counts.dispatched++;
      else if (entry.eligibility === 'needs_approval') counts.needsApproval++;
      else if (entry.eligibility === 'blocked') counts.blocked++;
      else counts.stale++;
      return counts;
    }, { eligible: 0, needsApproval: 0, blocked: 0, stale: 0, dispatched: 0 });
    return actual.eligible === result.eligible && actual.needsApproval === result.needsApproval &&
      actual.blocked === result.blocked && actual.stale === result.stale && actual.dispatched === result.dispatched;
  }

  private validPortfolioDispatchResult(
    result: IPursuitPortfolioDispatchResult,
    contract: { proposalId: string; proposalDigest: string; items: Array<{ proposalItemId: string; expectedItemDigest: string }> },
    proposal: IPursuitPortfolioExecutionProposalResult,
  ): boolean {
    const selected = new Map(contract.items.map((item) => [item.proposalItemId, item.expectedItemDigest]));
    const runSelectedItemIds = new Set(
      Array.isArray(result?.run?.selectedItemIds) ? result.run.selectedItemIds : [],
    );
    const resultItemIds = new Set(
      Array.isArray(result?.items) ? result.items.map((item) => item?.proposalItemId) : [],
    );
    if (!(result?.authority === 'portfolio_dispatch_result' && result?.canExecute === false &&
      ['workflows_created', 'needs_review', 'partial_failure'].includes(result?.status) &&
      typeof result?.resumed === 'boolean' && result?.run?.proposalId === contract.proposalId &&
      result.run.proposalDigest === contract.proposalDigest && result.run.confirmation === 'DISPATCH APPROVED PORTFOLIO WORKFLOWS' &&
      this.validPortfolioRecordId(result.run.id) && this.validPortfolioDigest(result.run.recordDigest) &&
      this.validPortfolioDigest(result.run.requestDigest) && this.validPortfolioDigest(result.run.selectedItemsDigest) &&
      Array.isArray(result.run.selectedItemIds) && result.run.selectedItemIds.length === selected.size &&
      runSelectedItemIds.size === selected.size && [...selected.keys()].every((id) => runSelectedItemIds.has(id)) &&
      Array.isArray(result.items) && result.items.length === selected.size &&
      resultItemIds.size === selected.size && [...selected.keys()].every((id) => resultItemIds.has(id)) &&
      result.items.every((item) => {
        const expected = proposal.items.find((candidate) => candidate.id === item.proposalItemId);
        return !!expected && selected.get(item.proposalItemId) === item.proposalItemDigest &&
          this.validPortfolioDispatchItemResult(item, expected);
      }) &&
      [result.created, result.replayed, result.needsReview, result.failed]
        .every((value) => Number.isSafeInteger(value) && value >= 0) &&
      result.created + result.replayed + result.needsReview + result.failed === result.items.length)) {
      return false;
    }
    const actual = result.items.reduce((summary, item) => {
      if (item.outcome === 'workflow_created') {
        summary.created++;
      } else if (item.outcome === 'replayed') {
        summary.replayed++;
      } else if (['needs_approval', 'blocked', 'stale'].includes(item.outcome)) {
        summary.needsReview++;
      } else {
        summary.failed++;
      }
      return summary;
    }, { created: 0, replayed: 0, needsReview: 0, failed: 0 });
    const expectedStatus = actual.failed > 0
      ? 'partial_failure'
      : actual.needsReview > 0 ? 'needs_review' : 'workflows_created';
    return result.created === actual.created && result.replayed === actual.replayed &&
      result.needsReview === actual.needsReview && result.failed === actual.failed &&
      result.status === expectedStatus;
  }

  private validPortfolioDispatchItemResult(
    result: IPursuitPortfolioDispatchItemResult,
    item: IPursuitPortfolioExecutionProposalItem,
  ): boolean {
    const success = ['workflow_created', 'replayed'].includes(result?.outcome);
    return this.validPortfolioRecordId(result?.id) && this.validPortfolioRecordId(result?.dispatchRunId) &&
      result?.proposalId === item.proposalId && result?.proposalItemId === item.id &&
      result?.proposalItemDigest === item.recordDigest && Number.isSafeInteger(result?.attemptNumber) && result.attemptNumber > 0 &&
      ['workflow_created', 'replayed', 'needs_approval', 'blocked', 'stale', 'failed', 'cancelled']
        .includes(result?.outcome) && !!String(result?.message || '').trim() && typeof result?.replayed === 'boolean' &&
      this.validPortfolioDigest(result?.recordDigest) && !Number.isNaN(new Date(result?.attemptedAt || '').getTime()) &&
      (success
        ? this.validPortfolioRecordId(result.approvalDecisionId) && this.validPortfolioDigest(result.approvalDecisionDigest) &&
          this.validPortfolioRecordId(result.authorizationReceiptId) && this.validPortfolioRecordId(result.workflowId) &&
          !!String(result.workflowState || '').trim()
        : !result.workflowId && !result.workflowState);
  }

  canDecidePortfolioExecutionProposalItem(
    item: IPursuitPortfolioExecutionProposalItem,
    decision: PursuitPortfolioExecutionProposalDecision,
  ): boolean {
    if (
      item.status === 'blocked'
      || !!this.portfolioExecutionProposalDecisionPendingId
      || !!this.portfolioExecutionProposalDecisionRunningId
      || !String(this.portfolioExecutionProposalDecisionReasons[item.id] || '').trim()
    ) {
      return false;
    }
    return decision !== 'revoked' || this.latestPortfolioExecutionProposalDecision(item.id)?.decision.decision === 'approved';
  }

  decidePortfolioExecutionProposalItem(
    item: IPursuitPortfolioExecutionProposalItem,
    decision: PursuitPortfolioExecutionProposalDecision,
  ): void {
    const reason = String(this.portfolioExecutionProposalDecisionReasons[item.id] || '').trim();
    if (!reason) {
      this.portfolioExecutionProposalDecisionErrors[item.id] = 'A reason is required before recording an immutable decision.';
      return;
    }
    if (reason.length > 2_000) {
      this.portfolioExecutionProposalDecisionErrors[item.id] = 'The decision reason must be 2,000 characters or fewer.';
      return;
    }
    if (!this.canDecidePortfolioExecutionProposalItem(item, decision)) {
      return;
    }

    const confirmation = this.portfolioExecutionProposalDecisionConfirmation(decision);
    const contract = {
      itemId: item.id,
      proposalId: item.proposalId,
      itemDigest: item.recordDigest,
      stateDigest: item.stateDigest,
      decision,
      reason,
      confirmation,
    };
    this.portfolioExecutionProposalDecisionPendingId = item.id;
    delete this.portfolioExecutionProposalDecisionErrors[item.id];
    this.modal.confirm({
      nzTitle: `${this.portfolioExecutionProposalDecisionLabel(decision)} this proposal item?`,
      nzContent: `Record the immutable decision "${confirmation}" with the stated reason. This decision does not queue or execute work and grants no execution authority.`,
      nzOkText: this.portfolioExecutionProposalDecisionLabel(decision),
      nzOkDanger: decision === 'rejected' || decision === 'revoked',
      nzCancelText: 'Cancel',
      nzOnCancel: () => {
        if (this.portfolioExecutionProposalDecisionPendingId === item.id) {
          this.portfolioExecutionProposalDecisionPendingId = '';
        }
      },
      nzOnOk: () => this.submitPortfolioExecutionProposalDecision(contract),
    });
  }

  portfolioExecutionProposalDecisionLabel(decision: PursuitPortfolioExecutionProposalDecision): string {
    switch (decision) {
      case 'approved': return 'Approve';
      case 'rejected': return 'Reject';
      case 'needs_clarification': return 'Request clarification';
      case 'revoked': return 'Revoke approval';
    }
  }

  portfolioExecutionProposalDecisionColor(decision: PursuitPortfolioExecutionProposalDecision): string {
    switch (decision) {
      case 'approved': return 'green';
      case 'rejected': return 'red';
      case 'revoked': return 'orange';
      case 'needs_clarification': return 'gold';
    }
  }

  latestPortfolioExecutionProposalDecision(
    itemId: string,
  ): IPursuitPortfolioExecutionProposalDecisionResult | undefined {
    const history = this.portfolioExecutionProposalDecisionHistory[itemId] || [];
    return history[history.length - 1];
  }

  canAuthorizePortfolioWorkflowEffect(item: IPursuitPortfolioExecutionProposalItem): boolean {
    const latest = this.latestPortfolioExecutionProposalDecision(item.id)?.decision;
    const expiresAt = latest?.expiresAt ? new Date(latest.expiresAt) : undefined;
    return item.status !== 'blocked'
      && latest?.decision === 'approved'
      && !!expiresAt
      && !Number.isNaN(expiresAt.getTime())
      && expiresAt.getTime() > Date.now()
      && !this.portfolioWorkflowAuthorizationPendingId
      && !this.portfolioWorkflowAuthorizationRunningId
      && String(this.portfolioWorkflowAuthorizationConfirmations[item.id] || '').trim()
        === 'AUTHORIZE PORTFOLIO WORKFLOW EFFECT';
  }

  authorizePortfolioWorkflowEffect(item: IPursuitPortfolioExecutionProposalItem): void {
    const latest = this.latestPortfolioExecutionProposalDecision(item.id)?.decision;
    if (!latest || !this.canAuthorizePortfolioWorkflowEffect(item)) {
      this.portfolioWorkflowAuthorizationErrors[item.id] =
        'A current unexpired approval and the exact authorization phrase are required.';
      return;
    }
    const contract = {
      itemId: item.id,
      itemDigest: item.recordDigest,
      decisionId: latest.id,
      decisionDigest: latest.recordDigest,
      confirmation: 'AUTHORIZE PORTFOLIO WORKFLOW EFFECT' as const,
    };
    this.portfolioWorkflowAuthorizationPendingId = item.id;
    delete this.portfolioWorkflowAuthorizationErrors[item.id];
    this.modal.confirm({
      nzTitle: 'Evaluate this exact workflow-intake effect?',
      nzContent: 'HAI will evaluate one reversible workflow.intake effect through hai-workflow-engine at EUR 0. The result is an unconsumed policy receipt only; it will not queue, create, or execute a workflow.',
      nzOkText: 'Evaluate authorization',
      nzCancelText: 'Cancel',
      nzOnCancel: () => {
        if (this.portfolioWorkflowAuthorizationPendingId === item.id) {
          this.portfolioWorkflowAuthorizationPendingId = '';
        }
      },
      nzOnOk: () => this.submitPortfolioWorkflowEffectAuthorization(contract),
    });
  }

  portfolioWorkflowAuthorizationColor(outcome: string): string {
    if (outcome === 'authorized') {
      return 'green';
    }
    return outcome === 'requires_approval' ? 'gold' : 'red';
  }

  private submitPortfolioWorkflowEffectAuthorization(contract: {
    itemId: string;
    itemDigest: string;
    decisionId: string;
    decisionDigest: string;
    confirmation: 'AUTHORIZE PORTFOLIO WORKFLOW EFFECT';
  }): void {
    if (this.portfolioWorkflowAuthorizationRunningId) {
      return;
    }
    const item = this.portfolioExecutionProposalItem(contract.itemId);
    const latest = this.latestPortfolioExecutionProposalDecision(contract.itemId)?.decision;
    if (
      !item
      || item.recordDigest !== contract.itemDigest
      || latest?.id !== contract.decisionId
      || latest.recordDigest !== contract.decisionDigest
      || latest.decision !== 'approved'
    ) {
      this.portfolioWorkflowAuthorizationPendingId = '';
      this.portfolioWorkflowAuthorizationErrors[contract.itemId] =
        'The proposal or approval changed. Inspect the current immutable evidence before continuing.';
      return;
    }

    this.portfolioWorkflowAuthorizationPendingId = '';
    this.portfolioWorkflowAuthorizationRunningId = contract.itemId;
    delete this.portfolioWorkflowAuthorizationErrors[contract.itemId];
    this.portfolioWorkflowAuthorizationSub?.unsubscribe();
    this.portfolioWorkflowAuthorizationSub = this.pursuitsService.authorizePortfolioWorkflowEffect(
      contract.itemId,
      {
        expectedItemDigest: contract.itemDigest,
        expectedDecisionDigest: contract.decisionDigest,
        confirmation: contract.confirmation,
      },
    ).subscribe({
      next: (result) => {
        this.portfolioWorkflowAuthorizationRunningId = '';
        if (!this.validPortfolioWorkflowAuthorizationResult(result, item, contract.decisionId)) {
          this.portfolioWorkflowAuthorizationErrors[contract.itemId] =
            'HAI rejected an authorization response that did not match the exact reviewed effect.';
          return;
        }
        this.portfolioWorkflowAuthorizationResults[contract.itemId] = result;
        this.portfolioWorkflowAuthorizationConfirmations[contract.itemId] = '';
        const title = result.receipt.outcome === 'authorized'
          ? 'Exact effect authorized'
          : 'Effect not authorized';
        const message = `${result.receipt.reason} No workflow was queued, created, or executed.`;
        if (result.receipt.outcome === 'authorized') {
          this.notification.success(title, message);
        } else {
          this.notification.warning(title, message);
        }
      },
      error: (error) => {
        this.portfolioWorkflowAuthorizationRunningId = '';
        this.portfolioWorkflowAuthorizationErrors[contract.itemId] =
          error?.error?.error || 'The exact workflow effect could not be evaluated.';
      },
    });
  }

  private validPortfolioWorkflowAuthorizationResult(
    result: IPursuitPortfolioWorkflowEffectAuthorizationResult,
    item: IPursuitPortfolioExecutionProposalItem,
    decisionId: string,
  ): boolean {
    const effect = result?.effect;
    const receipt = result?.receipt;
    const digestPattern = /^[0-9a-f]{64}$/;
    const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
    return result?.authority === 'execution_authorization_only'
      && result?.canExecute === false
      && effect?.action === 'pursuit.portfolio.create-workflow'
      && effect?.stage === 'execution'
      && effect?.resourceType === 'workflow-intake'
      && effect?.resourceId === item.id
      && effect?.toolId === 'workflow.intake'
      && effect?.runtimeId === 'hai-workflow-engine'
      && effect?.reversible === true
      && effect?.estimatedCostMicros === 0
      && !!String(effect?.actionSummary || '').trim()
      && digestPattern.test(effect?.effectDigest || '')
      && effect?.approvalSourceId === `portfolio-decision:${decisionId}`
      && uuidPattern.test(receipt?.id || '')
      && receipt?.contractVersion === 1
      && !!String(receipt?.ownerIdentity || '').trim()
      && receipt?.actorIdentity === receipt?.ownerIdentity
      && receipt?.actorKind === 'human'
      && receipt?.taskId === `portfolio-item:${item.id}`
      && receipt?.action === effect?.action
      && receipt?.stage === effect?.stage
      && receipt?.resourceType === effect?.resourceType
      && receipt?.resourceId === effect?.resourceId
      && receipt?.runtimeId === effect?.runtimeId
      && receipt?.approvalSourceId === effect?.approvalSourceId
      && receipt?.effectDigest === effect?.effectDigest
      && ['authorized', 'requires_approval', 'denied'].includes(String(receipt?.outcome || ''))
      && !!String(receipt?.reason || '').trim()
      && digestPattern.test(receipt?.requestDigest || '')
      && digestPattern.test(receipt?.decisionDigest || '')
      && receipt?.requiredAuthority === 1
      && receipt?.requestedAutonomy === 6
      && receipt?.risk === effect?.risk
      && receipt?.reversible === true
      && receipt?.estimatedCostEur === 0
      && !Number.isNaN(new Date(receipt?.evaluatedAt || '').getTime());
  }

  canExecutePortfolioWorkflowEffect(item: IPursuitPortfolioExecutionProposalItem): boolean {
    const authorization = this.portfolioWorkflowAuthorizationResults[item.id];
    const latest = this.latestPortfolioExecutionProposalDecision(item.id)?.decision;
    return authorization?.receipt?.outcome === 'authorized'
      && authorization.effect.resourceId === item.id
      && authorization.effect.approvalSourceId === `portfolio-decision:${latest?.id || ''}`
      && !this.portfolioWorkflowExecutionPendingId
      && !this.portfolioWorkflowExecutionRunningId
      && String(this.portfolioWorkflowExecutionConfirmations[item.id] || '').trim()
        === 'CREATE APPROVED PORTFOLIO WORKFLOW';
  }

  executePortfolioWorkflowEffect(item: IPursuitPortfolioExecutionProposalItem): void {
    const authorization = this.portfolioWorkflowAuthorizationResults[item.id];
    const latest = this.latestPortfolioExecutionProposalDecision(item.id)?.decision;
    if (!authorization || !latest || !this.canExecutePortfolioWorkflowEffect(item)) {
      this.portfolioWorkflowExecutionErrors[item.id] =
        'An authorized unconsumed receipt and the exact workflow creation phrase are required.';
      return;
    }
    const contract = {
      itemId: item.id,
      itemDigest: item.recordDigest,
      decisionId: latest.id,
      decisionDigest: latest.recordDigest,
      receiptId: authorization.receipt.id,
      effectDigest: authorization.effect.effectDigest,
      confirmation: 'CREATE APPROVED PORTFOLIO WORKFLOW' as const,
    };
    this.portfolioWorkflowExecutionPendingId = item.id;
    delete this.portfolioWorkflowExecutionErrors[item.id];
    this.modal.confirm({
      nzTitle: 'Create this approved local workflow?',
      nzContent: 'HAI will consume this exact receipt once and create one review-gated local workflow. It will not run the workflow, contact anyone, settle the resource hold, or perform an external action.',
      nzOkText: 'Create workflow',
      nzCancelText: 'Cancel',
      nzOnCancel: () => {
        if (this.portfolioWorkflowExecutionPendingId === item.id) {
          this.portfolioWorkflowExecutionPendingId = '';
        }
      },
      nzOnOk: () => this.submitPortfolioWorkflowEffectExecution(contract),
    });
  }

  private submitPortfolioWorkflowEffectExecution(contract: {
    itemId: string;
    itemDigest: string;
    decisionId: string;
    decisionDigest: string;
    receiptId: string;
    effectDigest: string;
    confirmation: 'CREATE APPROVED PORTFOLIO WORKFLOW';
  }): void {
    if (this.portfolioWorkflowExecutionRunningId) {
      return;
    }
    const item = this.portfolioExecutionProposalItem(contract.itemId);
    const latest = this.latestPortfolioExecutionProposalDecision(contract.itemId)?.decision;
    const authorization = this.portfolioWorkflowAuthorizationResults[contract.itemId];
    if (!item || item.recordDigest !== contract.itemDigest || latest?.id !== contract.decisionId ||
      latest.recordDigest !== contract.decisionDigest || latest.decision !== 'approved' ||
      authorization?.receipt.id !== contract.receiptId ||
      authorization.effect.effectDigest !== contract.effectDigest ||
      authorization.receipt.outcome !== 'authorized') {
      this.portfolioWorkflowExecutionPendingId = '';
      this.portfolioWorkflowExecutionErrors[contract.itemId] =
        'The proposal, approval, effect, or receipt changed. Inspect the current evidence before continuing.';
      return;
    }

    this.portfolioWorkflowExecutionPendingId = '';
    this.portfolioWorkflowExecutionRunningId = contract.itemId;
    delete this.portfolioWorkflowExecutionErrors[contract.itemId];
    this.portfolioWorkflowExecutionSub?.unsubscribe();
    this.portfolioWorkflowExecutionSub = this.pursuitsService.executePortfolioWorkflowEffect(
      contract.itemId,
      {
        authorizationReceiptId: contract.receiptId,
        expectedItemDigest: contract.itemDigest,
        expectedDecisionDigest: contract.decisionDigest,
        confirmation: contract.confirmation,
      },
    ).subscribe({
      next: (result) => {
        this.portfolioWorkflowExecutionRunningId = '';
        if (!this.validPortfolioWorkflowExecutionResult(result, item, authorization)) {
          this.portfolioWorkflowExecutionErrors[contract.itemId] =
            'HAI rejected a workflow result that did not match the consumed exact effect.';
          return;
        }
        this.portfolioWorkflowExecutionResults[contract.itemId] = result;
        this.portfolioWorkflowExecutionConfirmations[contract.itemId] = '';
        this.refreshPortfolioWorkflowVerification(item);
        this.notification.success(
          result.replayed ? 'Workflow already created' : 'Workflow created',
          'The receipt is consumed and the local workflow is review-gated. No workflow execution or external action occurred.',
        );
      },
      error: (error) => {
        this.portfolioWorkflowExecutionRunningId = '';
        this.portfolioWorkflowExecutionErrors[contract.itemId] =
          error?.error?.error || 'The approved workflow could not be created.';
      },
    });
  }

  private validPortfolioWorkflowExecutionResult(
    result: IPursuitPortfolioWorkflowEffectExecutionResult,
    item: IPursuitPortfolioExecutionProposalItem,
    authorization: IPursuitPortfolioWorkflowEffectAuthorizationResult,
  ): boolean {
    const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
    const consumption = result?.consumption;
    return result?.authority === 'workflow_effect_executed'
      && result?.canExecute === false
      && result?.pursuitId === item.pursuitId
      && uuidPattern.test(result?.workflowId || '')
      && result?.workflowState === 'needs_approval'
      && result?.effect?.effectDigest === authorization.effect.effectDigest
      && result?.receipt?.id === authorization.receipt.id
      && consumption?.receiptId === authorization.receipt.id
      && consumption?.ownerIdentity === authorization.receipt.ownerIdentity
      && consumption?.consumer === 'pursuit-portfolio-workflow'
      && consumption?.executionTarget === `workflow-intake:${authorization.effect.effectDigest}`
      && consumption?.receiptDigest === authorization.receipt.decisionDigest
      && !Number.isNaN(new Date(consumption?.consumedAt || '').getTime());
  }

  refreshPortfolioWorkflowVerification(item: IPursuitPortfolioExecutionProposalItem): void {
    const workflowId = this.portfolioWorkflowId(item.id);
    if (!workflowId || this.portfolioWorkflowVerificationLoading[item.id]) {
      return;
    }
    this.portfolioWorkflowVerificationLoading[item.id] = true;
    delete this.portfolioWorkflowVerificationErrors[item.id];
    const subscription = this.workflowService.get(workflowId).subscribe({
      next: (record) => {
        delete this.portfolioWorkflowVerificationLoading[item.id];
        if (!this.validPortfolioWorkflowRecord(record, workflowId)) {
          delete this.portfolioWorkflowRecords[item.id];
          this.portfolioWorkflowVerificationErrors[item.id] =
            'HAI rejected workflow evidence that did not match the linked workflow.';
          return;
        }
        this.portfolioWorkflowRecords[item.id] = record;
      },
      error: (error) => {
        delete this.portfolioWorkflowVerificationLoading[item.id];
        delete this.portfolioWorkflowRecords[item.id];
        this.portfolioWorkflowVerificationErrors[item.id] =
          error?.error?.error || 'The linked workflow verification could not be refreshed.';
      },
    });
    this.portfolioWorkflowVerificationSubs.add(subscription);
  }

  portfolioVerifiedWorkflow(itemId: string): IWorkflowRecord | undefined {
    const record = this.portfolioWorkflowRecords[itemId];
    return this.isPortfolioWorkflowVerifiedCompleted(record) ? record : undefined;
  }

  portfolioWorkflowVerificationLabel(itemId: string): string {
    const record = this.portfolioWorkflowRecords[itemId];
    if (!record?.item) {
      return 'not checked';
    }
    return `${record.item.currentState || 'unknown'} / ${record.item.verificationStatus || 'unverified'}`;
  }

  canSettlePortfolioWorkflow(item: IPursuitPortfolioExecutionProposalItem): boolean {
    return item.status !== 'blocked'
      && !!this.portfolioVerifiedWorkflow(item.id)
      && !!this.portfolioWorkflowId(item.id)
      && !this.portfolioWorkflowSettlementResults[item.id]
      && !this.portfolioWorkflowSettlementPendingId
      && !this.portfolioWorkflowSettlementRunningId
      && this.validPortfolioSettlementUsage(this.portfolioWorkflowSettlementEffortMinutes[item.id])
      && this.validPortfolioSettlementUsage(this.portfolioWorkflowSettlementCostMicros[item.id])
      && String(this.portfolioWorkflowSettlementConfirmations[item.id] || '').trim()
        === 'SETTLE VERIFIED PORTFOLIO WORK';
  }

  settlePortfolioWorkflow(item: IPursuitPortfolioExecutionProposalItem): void {
    const workflowId = this.portfolioWorkflowId(item.id);
    const workflow = this.portfolioVerifiedWorkflow(item.id);
    const effortMinutes = Number(this.portfolioWorkflowSettlementEffortMinutes[item.id]);
    const costMicros = Number(this.portfolioWorkflowSettlementCostMicros[item.id]);
    if (!workflowId || !workflow || !this.canSettlePortfolioWorkflow(item)) {
      this.portfolioWorkflowSettlementErrors[item.id] =
        'Verified completion, whole-number usage, and the exact settlement phrase are required.';
      return;
    }
    const contract = {
      itemId: item.id,
      itemDigest: item.recordDigest,
      pursuitId: item.pursuitId,
      reservationId: item.reservationId,
      workflowId,
      workflowCompletedAt: workflow.item.completedAt || '',
      taskPlanId: workflow.item.lastTaskPlanId || '',
      verificationStatus: workflow.item.verificationStatus || '',
      actualEffortMinutes: effortMinutes,
      actualCostMicros: costMicros,
      confirmation: 'SETTLE VERIFIED PORTFOLIO WORK' as const,
    };
    this.portfolioWorkflowSettlementPendingId = item.id;
    delete this.portfolioWorkflowSettlementErrors[item.id];
    this.modal.confirm({
      nzTitle: 'Settle this verified workflow reservation?',
      nzContent: `Record ${effortMinutes} actual minute(s) and ${costMicros} cost micros against the completed workflow. This closes accounting only; it cannot rerun the workflow or execute another effect.`,
      nzOkText: 'Settle reservation',
      nzCancelText: 'Cancel',
      nzOnCancel: () => {
        if (this.portfolioWorkflowSettlementPendingId === item.id) {
          this.portfolioWorkflowSettlementPendingId = '';
        }
      },
      nzOnOk: () => this.submitPortfolioWorkflowSettlement(contract),
    });
  }

  private submitPortfolioWorkflowSettlement(contract: {
    itemId: string;
    itemDigest: string;
    pursuitId: string;
    reservationId: string;
    workflowId: string;
    workflowCompletedAt: string;
    taskPlanId: string;
    verificationStatus: string;
    actualEffortMinutes: number;
    actualCostMicros: number;
    confirmation: 'SETTLE VERIFIED PORTFOLIO WORK';
  }): void {
    if (this.portfolioWorkflowSettlementRunningId) {
      return;
    }
    const item = this.portfolioExecutionProposalItem(contract.itemId);
    const workflowId = this.portfolioWorkflowId(contract.itemId);
    const workflow = this.portfolioVerifiedWorkflow(contract.itemId);
    if (!item || item.recordDigest !== contract.itemDigest || item.pursuitId !== contract.pursuitId ||
      item.reservationId !== contract.reservationId || workflowId !== contract.workflowId ||
      workflow?.item.id !== contract.workflowId || workflow.item.completedAt !== contract.workflowCompletedAt ||
      workflow.item.lastTaskPlanId !== contract.taskPlanId ||
      workflow.item.verificationStatus !== contract.verificationStatus) {
      this.portfolioWorkflowSettlementPendingId = '';
      this.portfolioWorkflowSettlementErrors[contract.itemId] =
        'The item or verified workflow changed. Refresh verification before settling usage.';
      return;
    }

    this.portfolioWorkflowSettlementPendingId = '';
    this.portfolioWorkflowSettlementRunningId = contract.itemId;
    delete this.portfolioWorkflowSettlementErrors[contract.itemId];
    this.portfolioWorkflowSettlementSub?.unsubscribe();
    this.portfolioWorkflowSettlementSub = this.pursuitsService.settlePortfolioWorkflow(
      contract.itemId,
      {
        workflowId: contract.workflowId,
        expectedItemDigest: contract.itemDigest,
        actualEffortMinutes: contract.actualEffortMinutes,
        actualCostMicros: contract.actualCostMicros,
        confirmation: contract.confirmation,
      },
    ).subscribe({
      next: (result) => {
        this.portfolioWorkflowSettlementRunningId = '';
        if (!this.validPortfolioWorkflowSettlementResult(result, item, workflow, contract)) {
          this.portfolioWorkflowSettlementErrors[contract.itemId] =
            'HAI rejected a settlement response that did not match the verified workflow and measured usage.';
          return;
        }
        this.portfolioWorkflowSettlementResults[contract.itemId] = result;
        this.portfolioWorkflowSettlementConfirmations[contract.itemId] = '';
        if (this.selected?.pursuit.id === result.pursuitId) {
          this.selected.resourceUsage = result.resourceUsage;
        }
        this.notification.success(
          result.replayed ? 'Existing settlement recovered' : 'Reservation settled',
          result.learningStatus === 'evidence_recorded'
            ? 'Verified usage and review-only learning evidence are recorded. Neither can execute or repeat workflow work.'
            : 'Verified usage is recorded. Learning evidence needs recovery, but accounting cannot execute or repeat workflow work.',
        );
      },
      error: (error) => {
        this.portfolioWorkflowSettlementRunningId = '';
        this.portfolioWorkflowSettlementErrors[contract.itemId] =
          error?.error?.error || 'The verified workflow reservation could not be settled.';
      },
    });
  }

  private validPortfolioWorkflowRecord(record: IWorkflowRecord, workflowId: string): boolean {
    return record?.item?.id === workflowId
      && Array.isArray(record?.checklist)
      && Array.isArray(record?.events);
  }

  private portfolioWorkflowId(itemId: string): string {
    const direct = this.portfolioWorkflowExecutionResults[itemId]?.workflowId;
    if (direct) {
      return direct;
    }
    for (const result of Object.values(this.portfolioDispatchResults)) {
      const workflowId = result.items.find((item) => item.proposalItemId === itemId)?.workflowId;
      if (workflowId) {
        return workflowId;
      }
    }
    for (const coordination of Object.values(this.portfolioDispatchCoordination)) {
      const workflowId = coordination.items.find((entry) => entry.item.id === itemId)?.latestDispatch?.workflowId;
      if (workflowId) {
        return workflowId;
      }
    }
    return '';
  }

  private isPortfolioWorkflowVerifiedCompleted(record: IWorkflowRecord | undefined): boolean {
    const item = record?.item;
    return item?.currentState === 'completed'
      && !!item.completedAt
      && !Number.isNaN(new Date(item.completedAt).getTime())
      && !!String(item.lastTaskPlanId || '').trim()
      && this.portfolioSettlementAcceptsVerification(item.verificationStatus);
  }

  private portfolioSettlementAcceptsVerification(status?: string): boolean {
    return ['verified', 'test_passed']
      .includes(String(status || '').trim().toLowerCase());
  }

  private validPortfolioSettlementUsage(value: number | null | undefined): boolean {
    const numeric = Number(value);
    return value !== null && value !== undefined && Number.isSafeInteger(numeric) && numeric >= 0;
  }

  private validPortfolioWorkflowSettlementResult(
    result: IPursuitPortfolioWorkflowSettlementResult,
    item: IPursuitPortfolioExecutionProposalItem,
    workflow: IWorkflowRecord,
    contract: {
      pursuitId: string;
      reservationId: string;
      workflowId: string;
      taskPlanId: string;
      verificationStatus: string;
      actualEffortMinutes: number;
      actualCostMicros: number;
    },
  ): boolean {
    const expectedEvidenceUri = `hai://workflow-completion-attestations/${result?.completionAttestationId}`;
    const usage = result?.resourceUsage;
    const numericUsageFields: Array<keyof IPursuitResourceUsage> = [
      'effortRecordedHours', 'effortReservedHours', 'effortCommittedHours', 'effortLimitHours',
      'effortRemainingHours', 'spendIncurredEur', 'spendRefundedEur', 'spendNetEur',
      'spendReservedEur', 'spendCommittedEur', 'spendLimitEur', 'spendRemainingEur',
      'eventCount', 'activeReservations',
    ];
    return result?.authority === 'verified_accounting_only'
      && result?.canExecute === false
      && typeof result?.replayed === 'boolean'
      && result?.pursuitId === contract.pursuitId
      && result?.pursuitId === item.pursuitId
      && result?.proposalItemId === item.id
      && result?.reservationId === contract.reservationId
      && result?.workflowId === contract.workflowId
      && result?.disposition === 'consumed'
      && result?.actualEffortMinutes === contract.actualEffortMinutes
      && result?.actualCostMicros === contract.actualCostMicros
      && String(result?.verificationStatus || '').trim().toLowerCase()
        === String(contract.verificationStatus || '').trim().toLowerCase()
      && this.portfolioSettlementAcceptsVerification(result?.verificationStatus)
      && result?.evidenceUri === expectedEvidenceUri
      && this.validPortfolioRecordId(result?.completionAttestationId)
      && this.validPortfolioRecordId(result?.settlementProofId)
      && this.validPortfolioDigest(result?.completionAttestationDigest)
      && this.validPortfolioDigest(result?.settlementProofDigest)
      && ['evidence_recorded', 'unavailable', 'recording_failed'].includes(result?.learningStatus)
      && ['proposal_unavailable', 'proposal_failed', 'insufficient_evidence', 'monitoring', 'stable',
        'review_required', 'changes_requested', 'governance_required', 'approved', 'rejected']
        .includes(result?.learningProposalStatus)
      && Number.isSafeInteger(Number(result?.learningSampleCount))
      && Number(result?.learningSampleCount) >= 0
      && Number.isSafeInteger(Number(result?.learningNewEvidenceCount))
      && Number(result?.learningNewEvidenceCount) >= 0
      && typeof result?.learningDriftDetected === 'boolean'
      && typeof result?.learningReviewRequired === 'boolean'
      && (result?.learningStatus !== 'evidence_recorded' || !!String(result?.learningOutcomeId || '').trim())
      && result?.learningReviewRequired === ['review_required', 'changes_requested', 'governance_required']
        .includes(result?.learningProposalStatus)
      && (!['review_required', 'changes_requested', 'governance_required'].includes(result?.learningProposalStatus)
        || (!!String(result?.learningProposalId || '').trim() && Number(result?.learningSampleCount) >= 3))
      && workflow.item.id === result?.workflowId
      && !!usage
      && ['not_configured', 'within_limits', 'reserved', 'exhausted', 'exceeded', 'unavailable'].includes(usage.state)
      && typeof usage.available === 'boolean'
      && typeof usage.limitsConfigured === 'boolean'
      && Array.isArray(usage.reservations)
      && numericUsageFields.every((field) => Number.isFinite(Number(usage[field])));
  }

  private validPortfolioRecordId(value?: string): boolean {
    return /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i
      .test(String(value || '').trim());
  }

  private validPortfolioDigest(value?: string): boolean {
    return /^(?:sha256:)?[0-9a-f]{64}$/i.test(String(value || '').trim());
  }

  private submitPortfolioExecutionProposalDecision(
    contract: {
      itemId: string;
      proposalId: string;
      itemDigest: string;
      stateDigest: string;
      decision: PursuitPortfolioExecutionProposalDecision;
      reason: string;
      confirmation: PursuitPortfolioExecutionProposalDecisionConfirmation;
    },
  ): void {
    if (this.portfolioExecutionProposalDecisionRunningId) {
      return;
    }
    const item = this.portfolioExecutionProposalItem(contract.itemId);
    if (
      !item
      || item.status === 'blocked'
      || item.proposalId !== contract.proposalId
      || item.recordDigest !== contract.itemDigest
      || item.stateDigest !== contract.stateDigest
      || (contract.decision === 'revoked'
        && this.latestPortfolioExecutionProposalDecision(item.id)?.decision.decision !== 'approved')
    ) {
      this.portfolioExecutionProposalDecisionPendingId = '';
      this.portfolioExecutionProposalDecisionErrors[contract.itemId] = 'The proposal item changed or is no longer eligible. Prepare and inspect a fresh snapshot before deciding.';
      return;
    }

    this.portfolioExecutionProposalDecisionPendingId = '';
    this.portfolioExecutionProposalDecisionRunningId = contract.itemId;
    delete this.portfolioExecutionProposalDecisionErrors[contract.itemId];
    this.portfolioExecutionProposalDecisionSub?.unsubscribe();
    this.portfolioExecutionProposalDecisionSub = this.pursuitsService.decidePortfolioExecutionProposalItem(contract.itemId, {
      expectedItemDigest: contract.itemDigest,
      decision: contract.decision,
      reason: contract.reason,
      confirmation: contract.confirmation,
    }).subscribe({
      next: (result) => {
        this.portfolioExecutionProposalDecisionRunningId = '';
        if (!this.validPortfolioExecutionProposalDecisionResult(result, item, contract)) {
          this.portfolioExecutionProposalDecisionErrors[contract.itemId] = 'HAI rejected a decision response that violated immutable evidence or non-execution authority.';
          return;
        }
        const history = this.portfolioExecutionProposalDecisionHistory[contract.itemId] || [];
        if (!history.some((entry) => entry.decision.recordDigest === result.decision.recordDigest)) {
          this.portfolioExecutionProposalDecisionHistory[contract.itemId] = [...history, result];
        }
        this.portfolioExecutionProposalDecisionReasons[contract.itemId] = '';
        const proposal = this.portfolioExecutionProposalById(item.proposalId);
        if (proposal) {
          this.loadPortfolioDispatchCoordination(proposal);
        }
        this.notification.success(
          result.replayed ? 'Decision already recorded' : 'Immutable decision recorded',
          'The proposal decision was recorded for review only. No work was queued or executed.',
        );
      },
      error: (error) => {
        this.portfolioExecutionProposalDecisionRunningId = '';
        this.portfolioExecutionProposalDecisionErrors[contract.itemId] = error?.error?.error || 'The proposal decision could not be recorded.';
      },
    });
  }

  private portfolioExecutionProposalItem(itemId: string): IPursuitPortfolioExecutionProposalItem | undefined {
    for (const result of Object.values(this.portfolioExecutionProposals)) {
      const item = result.items.find((candidate) => candidate.id === itemId);
      if (item) {
        return item;
      }
    }
    return undefined;
  }

  private portfolioExecutionProposalById(proposalId: string): IPursuitPortfolioExecutionProposalResult | undefined {
    return Object.values(this.portfolioExecutionProposals)
      .find((result) => result.proposal.id === proposalId);
  }

  private validPortfolioExecutionProposalDecisionResult(
    result: IPursuitPortfolioExecutionProposalDecisionResult,
    item: IPursuitPortfolioExecutionProposalItem,
    contract: {
      decision: PursuitPortfolioExecutionProposalDecision;
      reason: string;
      confirmation: PursuitPortfolioExecutionProposalDecisionConfirmation;
    },
  ): boolean {
    const decision = result?.decision;
    return result?.authority === 'approval_decision_only'
      && result?.canExecute === false
      && typeof result?.replayed === 'boolean'
      && this.validPortfolioExecutionProposalDecisionRecord(decision, item)
      && decision.decision === contract.decision
      && decision.reason === contract.reason
      && decision.confirmation === contract.confirmation;
  }

  private validPortfolioExecutionProposalDecisionRecord(
    decision: IPursuitPortfolioExecutionProposalDecisionResult['decision'],
    item: IPursuitPortfolioExecutionProposalItem,
  ): boolean {
    if (!decision) {
      return false;
    }
    const digestPattern = /^[0-9a-f]{64}$/;
    const expectedConfirmation = this.portfolioExecutionProposalDecisionConfirmation(decision.decision);
    const decidedAt = new Date(decision.decidedAt || '');
    const expiresAt = decision?.expiresAt ? new Date(decision.expiresAt) : undefined;
    return !!decision?.id
      && decision.proposalItemId === item.id
      && decision.proposalId === item.proposalId
      && decision.pursuitId === item.pursuitId
      && !!String(decision.reason || '').trim()
      && decision.reason === String(decision.reason || '').trim()
      && decision.reason.length <= 2_000
      && !!String(decision.actor || '').trim()
      && decision.confirmation === expectedConfirmation
      && decision.proposalItemDigest === item.recordDigest
      && decision.stateDigest === item.stateDigest
      && decision.authority === 'approval_decision_only'
      && digestPattern.test(decision.requestDigest)
      && digestPattern.test(decision.recordDigest)
      && !Number.isNaN(decidedAt.getTime())
      && (decision.decision === 'approved'
        ? !!expiresAt && !Number.isNaN(expiresAt.getTime()) && expiresAt.getTime() > decidedAt.getTime()
        : !decision.expiresAt)
      && (!decision.previousDecisionId || !!String(decision.previousDecisionId).trim())
      && !Object.prototype.hasOwnProperty.call(decision, 'ownerIdentity');
  }

  private loadPortfolioExecutionProposalDecisionHistories(
    items: IPursuitPortfolioExecutionProposalItem[],
  ): void {
    this.portfolioExecutionProposalDecisionHistorySub.unsubscribe();
    this.portfolioExecutionProposalDecisionHistorySub = new Subscription();
    items.forEach((item) => this.loadPortfolioExecutionProposalDecisionHistory(item));
  }

  private loadPortfolioExecutionProposalDecisionHistory(
    item: IPursuitPortfolioExecutionProposalItem,
  ): void {
    const subscription = this.pursuitsService.portfolioExecutionProposalDecisionHistory(item.id, 100).subscribe({
      next: (result) => {
        if (
          result?.authority !== 'approval_decision_only'
          || result?.canExecute !== false
          || !Array.isArray(result?.decisions)
          || result.decisions.length > 100
          || !result.decisions.every((decision) => this.validPortfolioExecutionProposalDecisionRecord(decision, item))
          || !result.decisions.every((decision, index) => (
            index === result.decisions.length - 1
            || decision.previousDecisionId === result.decisions[index + 1].id
          ))
        ) {
          this.portfolioExecutionProposalDecisionErrors[item.id] = 'HAI rejected decision history that violated immutable evidence or non-execution authority.';
          return;
        }
        this.portfolioExecutionProposalDecisionHistory[item.id] = [...result.decisions].reverse().map((decision) => ({
          decision,
          replayed: false,
          authority: 'approval_decision_only',
          canExecute: false,
        }));
      },
      error: (error) => {
        this.portfolioExecutionProposalDecisionErrors[item.id] = error?.error?.error || 'Immutable proposal decision history could not be loaded.';
      },
    });
    this.portfolioExecutionProposalDecisionHistorySub.add(subscription);
  }

  private portfolioExecutionProposalDecisionConfirmation(
    decision: PursuitPortfolioExecutionProposalDecision,
  ): PursuitPortfolioExecutionProposalDecisionConfirmation {
    switch (decision) {
      case 'approved': return 'APPROVE EXECUTION PROPOSAL ITEM';
      case 'rejected': return 'REJECT EXECUTION PROPOSAL ITEM';
      case 'needs_clarification': return 'REQUEST CLARIFICATION FOR EXECUTION PROPOSAL ITEM';
      case 'revoked': return 'REVOKE EXECUTION PROPOSAL ITEM';
    }
  }

  private submitPortfolioExecutionProposals(
    contract: { allocationId: string; allocationRecordDigest: string },
  ): void {
    if (this.portfolioExecutionProposalRunningId) {
      return;
    }
    const allocation = this.portfolioAllocationRecord(contract.allocationId);
    if (
      !allocation
      || !this.validPortfolioAllocationRecord(allocation)
      || allocation.allocation.recordDigest !== contract.allocationRecordDigest
    ) {
      this.portfolioExecutionProposalPendingId = '';
      this.portfolioExecutionProposalErrors[contract.allocationId] = 'The immutable allocation changed before proposal preparation. Refresh and inspect the allocation again.';
      return;
    }
    this.portfolioExecutionProposalPendingId = '';
    this.portfolioExecutionProposalRunningId = contract.allocationId;
    delete this.portfolioExecutionProposalErrors[contract.allocationId];
    this.portfolioExecutionProposalSub?.unsubscribe();
    this.portfolioExecutionProposalSub = this.pursuitsService.preparePortfolioExecutionProposals(contract.allocationId, {
      expectedAllocationDigest: contract.allocationRecordDigest,
      confirmation: 'PREPARE EXECUTION PROPOSALS',
    }).subscribe({
      next: (result) => {
        this.portfolioExecutionProposalRunningId = '';
        if (!this.validPortfolioExecutionProposalResult(result, allocation)) {
          this.portfolioExecutionProposalErrors[contract.allocationId] = 'HAI rejected execution proposals that violated their proposal-only authority or immutable allocation evidence.';
          return;
        }
        this.portfolioExecutionProposals[contract.allocationId] = result;
        this.loadPortfolioExecutionProposalDecisionHistories(result.items);
        this.loadPortfolioDispatchCoordination(result);
        this.notification.success(
          result.replayed ? 'Execution proposals already prepared' : 'Execution proposals prepared',
          'Immutable proposal evidence is ready for inspection. No work was approved, queued, or executed.',
        );
      },
      error: (error) => {
        this.portfolioExecutionProposalRunningId = '';
        this.portfolioExecutionProposalErrors[contract.allocationId] = error?.error?.error || 'Execution proposals could not be prepared.';
      },
    });
  }

  private portfolioAllocationRecord(allocationId: string): IPursuitPortfolioAllocationAcceptanceResult | undefined {
    const fromHistory = this.portfolioAllocationHistory.find((record) => record.allocation.id === allocationId);
    if (fromHistory) {
      return fromHistory;
    }
    return this.portfolioAllocationResult?.allocation.id === allocationId ? this.portfolioAllocationResult : undefined;
  }

  private validPortfolioExecutionProposalResult(
    result: IPursuitPortfolioExecutionProposalResult,
    allocation: IPursuitPortfolioAllocationAcceptanceResult,
  ): boolean {
    const proposal = result?.proposal;
    const freshness = result?.freshness;
    const digestPattern = /^[0-9a-f]{64}$/;
    const proposalStatuses = ['prepared', 'prepared_needs_approval', 'prepared_blocked'];
    const freshnessStatuses = ['prepared_snapshot', 'recovered_snapshot'];
    if (
      result?.authority !== 'proposal_only'
      || result?.canExecute !== false
      || typeof result?.replayed !== 'boolean'
      || !freshnessStatuses.includes(freshness?.status)
      || freshness?.revalidationRequired !== true
      || Number.isNaN(new Date(freshness?.checkedAt).getTime())
      || !String(freshness?.reason || '').trim()
      || freshness.reason.length > 1000
      || (result.replayed && freshness.status !== 'recovered_snapshot')
      || (!result.replayed && freshness.status !== 'prepared_snapshot')
      || !proposal?.id
      || proposal.allocationId !== allocation.allocation.id
      || proposal.allocationRecordDigest !== allocation.allocation.recordDigest
      || !digestPattern.test(proposal.snapshotDigest)
      || !proposalStatuses.includes(proposal.status)
      || !String(proposal.actor || '').trim()
      || proposal.confirmation !== 'PREPARE EXECUTION PROPOSALS'
      || proposal.authority !== 'proposal_only'
      || !digestPattern.test(proposal.recordDigest)
      || Number.isNaN(new Date(proposal.preparedAt).getTime())
      || !Array.isArray(result.items)
      || result.items.length !== allocation.items.length
      || result.items.length < 1
      || result.items.length > 500
    ) {
      return false;
    }

    const allocationItems = new Map(allocation.items.map((item) => [item.id, item]));
    const seenItems = new Set<string>();
    const seenProposalItems = new Set<string>();
    let hasApproval = false;
    let hasBlocked = false;
    const itemsValid = result.items.every((item) => {
      const allocationItem = allocationItems.get(item.allocationItemId);
      if (!item?.id || seenProposalItems.has(item.id) || !allocationItem || seenItems.has(item.allocationItemId)) {
        return false;
      }
      seenProposalItems.add(item.id);
      seenItems.add(item.allocationItemId);
      hasApproval = hasApproval || item.requiresApproval;
      hasBlocked = hasBlocked || item.status === 'blocked';
      const reasonsValid = this.validProposalReasons(item.approvalReasons) && this.validProposalReasons(item.blockedReasons);
      const statusReasonsValid = item.requiresApproval === (item.approvalReasons.length > 0)
        && (item.status !== 'needs_approval' || item.requiresApproval)
        && (item.status !== 'proposed' || !item.requiresApproval)
        && (item.status === 'blocked' ? item.blockedReasons.length > 0 : item.blockedReasons.length === 0);
      return item.proposalId === proposal.id
        && item.pursuitId === allocationItem.pursuitId
        && item.reservationId === allocationItem.reservationId
        && item.allocationItemDigest === allocationItem.recordDigest
        && !!String(item.actionSummary || '').trim()
        && item.actionSummary.length <= 4_000
        && !!String(item.pursuitStatus || '').trim()
        && item.pursuitStatus.length <= 80
        && !!String(item.riskLevel || '').trim()
        && item.riskLevel.length <= 80
        && !!String(item.autonomyLevel || '').trim()
        && item.autonomyLevel.length <= 80
        && ['proposed', 'needs_approval', 'blocked'].includes(item.status)
        && typeof item.requiresApproval === 'boolean'
        && reasonsValid
        && statusReasonsValid
        && digestPattern.test(item.stateDigest)
        && digestPattern.test(item.recordDigest)
        && !Number.isNaN(new Date(item.preparedAt).getTime())
        && this.sameInstant(item.preparedAt, proposal.preparedAt);
    });
    const expectedStatus = hasBlocked ? 'prepared_blocked' : hasApproval ? 'prepared_needs_approval' : 'prepared';
    return itemsValid && proposal.status === expectedStatus;
  }

  portfolioExecutionFreshnessColor(status: string): string {
    return status === 'prepared_snapshot' ? 'green' : 'gold';
  }

  portfolioExecutionFreshnessLabel(status: string): string {
    return status === 'prepared_snapshot' ? 'new snapshot' : 'historical snapshot';
  }

  private validProposalReasons(reasons: string[]): boolean {
    return Array.isArray(reasons)
      && reasons.length <= 40
      && reasons.every((reason) => !!reason && reason === reason.trim() && reason.length <= 1000);
  }

  acceptPortfolioAllocation(): void {
    const contract = this.portfolioAcceptanceContract();
    if (!contract || !this.canAcceptPortfolioAllocation()) {
      return;
    }
    this.portfolioAcceptancePending = true;
    this.portfolioAcceptanceError = '';
    this.modal.confirm({
      nzTitle: 'Accept this portfolio allocation?',
      nzContent: 'This reserves only the bounded pursuit capacity shown in the schedule. It does not approve risky work, execute tasks, or grant an agent authority.',
      nzOkText: 'Accept allocation',
      nzCancelText: 'Keep advisory only',
      nzOnCancel: () => {
        this.portfolioAcceptancePending = false;
      },
      nzOnOk: () => {
        this.submitPortfolioAllocationAcceptance(contract);
      },
    });
  }

  private submitPortfolioAllocationAcceptance(
    contract: { planningRequest: IPursuitPortfolioPlanningRequest; decisionDigest: string },
  ): void {
    if (this.portfolioAcceptanceRunning || this.portfolioAllocationResult) {
      return;
    }
    const current = this.portfolioAcceptanceContract();
    if (
      !current
      || current.planningRequest !== contract.planningRequest
      || current.decisionDigest !== contract.decisionDigest
    ) {
      this.portfolioAcceptancePending = false;
      this.portfolioAcceptanceError = 'The advisory decision changed before acceptance. Calculate and review a fresh plan.';
      return;
    }
    const request: IPursuitPortfolioAllocationAcceptanceRequest = {
      planningRequest: contract.planningRequest,
      expectedDecisionDigest: contract.decisionDigest,
      confirmation: 'ACCEPT PORTFOLIO ALLOCATION',
    };
    this.portfolioAcceptanceRunning = true;
    this.portfolioAcceptanceError = '';
    this.pursuitsService.acceptPortfolioAllocation(request).subscribe({
      next: (result) => {
        this.portfolioAcceptanceRunning = false;
        this.portfolioAcceptancePending = false;
        if (!this.validPortfolioAllocationResult(result, contract)) {
          this.portfolioAcceptanceError = 'HAI rejected an allocation response with mismatched authority, plan, or decision evidence.';
          return;
        }
        this.portfolioAllocationResult = result;
        this.portfolioAllocationHistory = this.reconciledPortfolioAllocationHistory(this.portfolioAllocationHistory, result);
        this.portfolioAllocationHistoryLoaded = true;
        this.portfolioAllocationHistoryError = '';
        this.notification.success(
          result.replayed ? 'Allocation already accepted' : 'Portfolio allocation accepted',
          'Bounded pursuit capacity is reserved. No work was approved or executed.',
        );
      },
      error: (error) => {
        this.portfolioAcceptanceRunning = false;
        this.portfolioAcceptancePending = false;
        this.portfolioAcceptanceError = error?.error?.error || 'The bounded portfolio allocation could not be accepted.';
      },
    });
  }

  private validPortfolioAllocationResult(
    result: IPursuitPortfolioAllocationAcceptanceResult,
    contract: { planningRequest: IPursuitPortfolioPlanningRequest; decisionDigest: string },
  ): boolean {
    const allocation = result?.allocation;
    const scheduled = this.portfolioResult?.decision?.scheduled || [];
    const digestPattern = /^[0-9a-f]{64}$/;
    if (
      !this.validPortfolioAllocationRecord(result)
      || !allocation
      || allocation.planId !== contract.planningRequest.planId
      || allocation.decisionDigest !== contract.decisionDigest
      || !digestPattern.test(allocation.requestDigest)
      || !digestPattern.test(allocation.recordDigest)
      || !['accepted', 'accepted_needs_approval'].includes(allocation.status)
      || allocation.durationMode !== contract.planningRequest.durationMode
      || !this.sameInstant(allocation.horizonStart, contract.planningRequest.horizonStart)
      || !this.sameInstant(allocation.horizonEnd, contract.planningRequest.horizonEnd)
      || !String(allocation.actor || '').trim()
      || allocation.confirmation !== 'ACCEPT PORTFOLIO ALLOCATION'
      || Number.isNaN(new Date(allocation.acceptedAt).getTime())
      || !Array.isArray(result.items)
      || result.items.length !== scheduled.length
    ) {
      return false;
    }
    const scheduledByPursuit = new Map(scheduled.map((item) => [item.taskId, item]));
    const estimatesByPursuit = new Map(contract.planningRequest.pursuits.map((item) => [item.pursuitId, item]));
    const seen = new Set<string>();
    return result.items.every((item: IPursuitPortfolioAllocationItem) => {
      const planned = scheduledByPursuit.get(item.pursuitId);
      const estimate = estimatesByPursuit.get(item.pursuitId);
      if (!planned || !estimate || seen.has(item.pursuitId)) {
        return false;
      }
      seen.add(item.pursuitId);
      return !!item.id
        && item.allocationId === allocation.id
        && this.sameInstant(item.scheduledStart, planned.start)
        && this.sameInstant(item.scheduledEnd, planned.end)
        && item.durationMinutes === planned.plannedDurationMinutes
        && item.estimatedCostMicros === estimate.estimatedUsage.costMicros
        && typeof item.requiresApproval === 'boolean'
        && Array.isArray(item.approvalReasons)
        && item.requiresApproval === (item.approvalReasons.length > 0)
        && !!item.reservationId
        && digestPattern.test(item.recordDigest)
        && !Number.isNaN(new Date(item.createdAt).getTime());
    });
  }

  private validPortfolioAllocationRecord(result: IPursuitPortfolioAllocationAcceptanceResult): boolean {
    const allocation = result?.allocation;
    const digestPattern = /^[0-9a-f]{64}$/;
    if (
      result?.authority !== 'allocation_only'
      || result?.canExecute !== false
      || !allocation
      || !allocation.id
      || !allocation.planId
      || !digestPattern.test(allocation.requestDigest)
      || !digestPattern.test(allocation.decisionDigest)
      || !digestPattern.test(allocation.recordDigest)
      || !['accepted', 'accepted_needs_approval'].includes(allocation.status)
      || !['expected', 'conservative'].includes(allocation.durationMode)
      || !this.validTimeRange(allocation.horizonStart, allocation.horizonEnd)
      || !String(allocation.actor || '').trim()
      || allocation.confirmation !== 'ACCEPT PORTFOLIO ALLOCATION'
      || Number.isNaN(new Date(allocation.acceptedAt).getTime())
      || !Array.isArray(result.items)
      || result.items.length < 1
      || result.items.length > 500
    ) {
      return false;
    }
    const horizonStart = new Date(allocation.horizonStart).getTime();
    const horizonEnd = new Date(allocation.horizonEnd).getTime();
    const seenItems = new Set<string>();
    const seenPursuits = new Set<string>();
    let requiresApproval = false;
    const itemsValid = result.items.every((item) => {
      if (!item?.id || seenItems.has(item.id) || !item.pursuitId || seenPursuits.has(item.pursuitId)) {
        return false;
      }
      seenItems.add(item.id);
      seenPursuits.add(item.pursuitId);
      const scheduledStart = new Date(item.scheduledStart).getTime();
      const scheduledEnd = new Date(item.scheduledEnd).getTime();
      requiresApproval = requiresApproval || item.requiresApproval;
      return item.allocationId === allocation.id
        && this.validTimeRange(item.scheduledStart, item.scheduledEnd)
        && scheduledStart >= horizonStart
        && scheduledEnd <= horizonEnd
        && Number.isFinite(item.durationMinutes)
        && item.durationMinutes > 0
        && scheduledEnd - scheduledStart === item.durationMinutes * 60_000
        && Number.isFinite(item.estimatedCostMicros)
        && item.estimatedCostMicros >= 0
        && typeof item.requiresApproval === 'boolean'
        && Array.isArray(item.approvalReasons)
        && item.approvalReasons.length <= 20
        && item.approvalReasons.every((reason) => !!reason && reason === reason.trim() && reason.length <= 1000)
        && item.requiresApproval === (item.approvalReasons.length > 0)
        && !!item.reservationId
        && digestPattern.test(item.recordDigest)
        && !Number.isNaN(new Date(item.createdAt).getTime());
    });
    return itemsValid && allocation.status === (requiresApproval ? 'accepted_needs_approval' : 'accepted');
  }

  private reconciledPortfolioAllocationHistory(
    records: IPursuitPortfolioAllocationAcceptanceResult[],
    accepted: IPursuitPortfolioAllocationAcceptanceResult | undefined = this.portfolioAllocationResult,
  ): IPursuitPortfolioAllocationAcceptanceResult[] {
    const byAllocationId = new Map<string, IPursuitPortfolioAllocationAcceptanceResult>();
    for (const record of records) {
      byAllocationId.set(record.allocation.id, record);
    }
    if (accepted && this.validPortfolioAllocationRecord(accepted)) {
      byAllocationId.set(accepted.allocation.id, accepted);
    }
    return Array.from(byAllocationId.values())
      .sort((left, right) => new Date(right.allocation.acceptedAt).getTime() - new Date(left.allocation.acceptedAt).getTime())
      .slice(0, 20);
  }

  private validTimeRange(start: string, end: string): boolean {
    const startTime = new Date(start).getTime();
    const endTime = new Date(end).getTime();
    return !Number.isNaN(startTime) && !Number.isNaN(endTime) && endTime > startTime;
  }

  private sameInstant(left: string, right: string): boolean {
    const leftTime = new Date(left).getTime();
    const rightTime = new Date(right).getTime();
    return !Number.isNaN(leftTime) && !Number.isNaN(rightTime) && leftTime === rightTime;
  }

  private portfolioAcceptanceContract(): { planningRequest: IPursuitPortfolioPlanningRequest; decisionDigest: string } | undefined {
    const result = this.portfolioResult;
    const decision = result?.decision;
    const planningRequest = this.portfolioPlanningRequest;
    const decisionDigest = String(decision?.decisionDigest || '').trim();
    if (
      !result
      || !decision
      || !planningRequest
      || result.planId !== planningRequest.planId
      || decision.planId !== planningRequest.planId
      || !decisionDigest
      || !decision.scheduled?.length
      || decision.feasibility === 'infeasible'
      || !!decision.criticalBlockers?.length
      || result.authority !== 'advisory_only'
      || result.canExecute !== false
      || result.capacity?.status !== 'applied'
      || decision.canExecute
      || decision.grantsAuthority
    ) {
      return undefined;
    }
    return { planningRequest, decisionDigest };
  }

  portfolioPursuitTitle(id: string): string {
    return this.portfolioDrafts.find((draft) => draft.pursuit.id === id)?.pursuit.title || id;
  }

  private newPortfolioDraft(pursuit: IPursuit): PortfolioPursuitDraft {
    return {
      pursuit,
      selected: false,
      optional: false,
      optimisticMinutes: null,
      expectedMinutes: null,
      pessimisticMinutes: null,
      estimatedCostEur: 0,
      inputTokens: 0,
      outputTokens: 0,
      toolCalls: 0,
      estimateBasis: '',
      factors: this.neutralPortfolioFactors(),
      factorsReviewed: false,
    };
  }

  private neutralPortfolioFactors(): IPursuitPortfolioPriorityFactors {
    return this.portfolioFactorFields.reduce((factors, field) => {
      factors[field.key] = 50;
      return factors;
    }, {} as IPursuitPortfolioPriorityFactors);
  }

  private newPortfolioPlanId(): string {
    const cryptoApi = globalThis.crypto;
    if (cryptoApi?.randomUUID) {
      return `portfolio-${cryptoApi.randomUUID()}`;
    }
    return `portfolio-${Date.now()}-${Math.random().toString(36).slice(2, 12)}`;
  }

  selectPursuit(pursuit: IPursuit, updateRoute: boolean = true): void {
    if (this.selected?.pursuit.id === pursuit.id && !this.detailLoading) {
      if (updateRoute) {
        this.setSelectedQuery(pursuit.id);
      }
      return;
    }
    this.requestedPursuitId = pursuit.id;
    this.loadProjectDossier(pursuit.projectKey);
    this.loadPursuitDetail(pursuit.id, updateRoute);
  }

  selectFirstFromQueue(queue: IPursuitListItem[] = []): void {
    if (!queue.length) {
      return;
    }
    this.selectPursuit(queue[0].pursuit);
  }

  openDashboardDecision(card: IPursuitDashboardDecision): void {
    this.selectPursuit(card.pursuit);
  }

  openLinkedPursuit(link: IPursuitLink): void {
    if (link.linkType !== 'pursuit' || !link.linkId) {
      return;
    }
    this.router.navigate(['/pursuits'], { queryParams: { selected: link.linkId } });
  }

  resolveDashboardDecision(card: IPursuitDashboardDecision, approved: boolean): void {
    if (this.lifecycleRunningId || this.resolvingDecisionId || !this.canResolveDecision(card.decision)) {
      return;
    }
    this.pursuitDetailSub?.unsubscribe();
    if (this.selected?.pursuit.id !== card.pursuit.id) {
      this.delegationSub?.unsubscribe();
      this.delegationPackage = undefined;
      this.evidenceResolutionSub?.unsubscribe();
      this.selected = undefined;
      this.inspectedRuntimeEvidence = undefined;
      this.inspectedEvidence = undefined;
      this.inspectedAction = undefined;
    }
    this.requestedPursuitId = card.pursuit.id;
    this.detailLoading = true;
    this.loadProjectDossier(card.pursuit.projectKey);
    this.decisionDetailSub?.unsubscribe();
    let received = false;
    this.decisionDetailSub = this.pursuitsService.get(card.pursuit.id).pipe(take(1)).subscribe({
      next: (detail) => {
        received = true;
        this.detailLoading = false;
        if (detail?.pursuit?.id !== card.pursuit.id || !Array.isArray(detail.decisionQueue)) {
          this.notification.warning('Decision unavailable', 'The response does not match this pursuit. Refresh before reviewing the decision.');
          return;
        }
        this.selected = detail;
        this.loadProjectDossier(detail.pursuit.projectKey);
        this.detailLoading = false;
        this.setSelectedQuery(detail.pursuit.id);
        const freshDecision = detail.decisionQueue.find((item) => item.id === card.decision.id);
        if (!freshDecision) {
          this.notification.warning('Decision no longer available', 'This decision is not in the current queue. Review the refreshed pursuit before taking action.');
          return;
        }
        this.resolveDecision(freshDecision, approved);
      },
      error: () => {
        this.detailLoading = false;
        this.notification.error('Decision unavailable', 'HAI could not load the current decision. Refresh the pursuit before retrying.');
      },
      complete: () => {
        if (!received) {
          this.detailLoading = false;
          this.notification.warning('Decision unavailable', 'No current decision was received. Refresh the pursuit before retrying.');
        }
      },
    });
  }

  private selectPursuitById(id: string, updateRoute: boolean): void {
    if (!id) {
      return;
    }
    if (this.selected?.pursuit.id === id && !this.detailLoading) {
      this.openRequestedEvidence();
      return;
    }
    const listed = this.pursuits.find((item) => item.id === id);
    if (listed) {
      this.selectPursuit(listed, updateRoute);
      return;
    }
    this.loadProjectDossier(undefined);
    this.loadPursuitDetail(id, updateRoute);
  }

  private loadPursuitDetail(id: string, updateRoute: boolean): void {
    this.delegationSub?.unsubscribe();
    this.decisionDetailSub?.unsubscribe();
    this.pursuitDetailSub?.unsubscribe();
    if (this.selected?.pursuit.id !== id) {
      this.evidenceResolutionSub?.unsubscribe();
      this.selected = undefined;
      this.inspectedRuntimeEvidence = undefined;
      this.inspectedEvidence = undefined;
      this.inspectedAction = undefined;
    }
    this.requestedPursuitId = id;
    this.detailLoading = true;
    this.detailError = '';
    this.delegationPackage = undefined;
    this.showContextEditor = false;
    this.resetResourceLedger(id);
    let received = false;
    this.pursuitDetailSub = this.pursuitsService.get(id).pipe(take(1)).subscribe({
      next: (detail) => {
        received = true;
        this.detailLoading = false;
        if (detail?.pursuit?.id !== id) {
          this.detailError = 'The response does not match the requested pursuit. Retry loading its details.';
          this.notification.error('Pursuit unavailable', this.detailError);
          return;
        }
        this.selected = detail;
        this.loadProjectDossier(detail.pursuit.projectKey);
        this.detailLoading = false;
        this.detailError = '';
        if (updateRoute) {
          this.setSelectedQuery(detail.pursuit.id);
        }
        this.openRequestedEvidence();
      },
      error: () => {
        this.detailLoading = false;
        this.detailError = 'HAI could not load the pursuit details. Retry loading this pursuit.';
        this.notification.error('Pursuit unavailable', this.detailError);
      },
      complete: () => {
        if (!received) {
          this.detailLoading = false;
          this.detailError = 'No pursuit details were received. Retry loading this pursuit.';
          this.notification.warning('Pursuit unavailable', this.detailError);
        }
      },
    });
  }

  retrySelectedPursuit(): void {
    if (!this.requestedPursuitId || this.detailLoading) {
      return;
    }
    this.loadPursuitDetail(this.requestedPursuitId, false);
  }

  retryProjectDossier(): void {
    if (!this.projectDossierProjectKey || this.projectDossierLoading) {
      return;
    }
    this.loadProjectDossier(this.projectDossierProjectKey, true);
  }

  private loadProjectDossier(projectKey?: string, force = false): void {
    const key = typeof projectKey === 'string' ? projectKey : '';
    if (!force && key === this.projectDossierProjectKey && (
      this.projectDossierLoading || this.projectDossier || this.projectDossierError
    )) {
      return;
    }

    this.projectDossierSub?.unsubscribe();
    const generation = ++this.projectDossierRequestGeneration;
    this.projectDossierProjectKey = key;
    this.projectDossier = undefined;
    this.projectDossierLoading = false;
    this.projectDossierError = '';
    if (!key.trim()) {
      return;
    }

    this.projectDossierLoading = true;
    this.projectDossierSub = this.pursuitsService.projectDossier(key).subscribe({
      next: (dossier) => {
        if (generation !== this.projectDossierRequestGeneration || key !== this.projectDossierProjectKey) {
          return;
        }
        this.projectDossierLoading = false;
        if (!dossier || dossier.projectKey !== key) {
          this.projectDossierError = 'The returned project dossier did not match the selected project.';
          return;
        }
        this.projectDossier = dossier;
      },
      error: (error) => {
        if (generation !== this.projectDossierRequestGeneration || key !== this.projectDossierProjectKey) {
          return;
        }
        this.projectDossierLoading = false;
        this.projectDossierError = error?.error?.error || 'Project context could not be loaded.';
      },
    });
  }

  loadResourceEvents(force: boolean = false): void {
    const pursuitId = this.selected?.pursuit.id || '';
    if (!pursuitId || this.resourceEventsLoading || (!force && this.resourceEventsLoadedFor === pursuitId)) {
      return;
    }
    this.resourceEventsSub?.unsubscribe();
    this.resourceEventsLoading = true;
    this.resourceEventsError = '';
    let received = false;
    this.resourceEventsSub = this.pursuitsService.resourceEvents(pursuitId, 100).pipe(
      take(1), finalize(() => { this.resourceEventsLoading = false; }),
    ).subscribe({
      next: (events) => {
        received = true;
        if (this.selected?.pursuit.id !== pursuitId) return;
        const ids = new Set<string>();
        if (!Array.isArray(events) || !events.every(event => {
          if (!event || typeof event.id !== 'string' || !event.id.trim() || ids.has(event.id) ||
              event.pursuitId !== pursuitId || !['effort_recorded', 'spend_incurred', 'spend_refund'].includes(event.kind) ||
              !Number.isSafeInteger(event.effortMinutes) || event.effortMinutes < 0 ||
              !Number.isSafeInteger(event.amountMinor) || event.amountMinor < 0 ||
              (event.kind === 'effort_recorded' ? event.effortMinutes <= 0 || event.amountMinor !== 0
                : event.amountMinor <= 0 || event.effortMinutes !== 0 || event.currency !== 'EUR')) return false;
          ids.add(event.id);
          return true;
        })) {
          this.resourceEventsError = 'The returned ledger contains missing, duplicate or mismatched entries. Reload before relying on these records.';
          this.resourceEventsLoadedFor = '';
          return;
        }
        this.resourceEvents = events;
        this.resourceEventsLoadedFor = pursuitId;
      },
      error: () => {
        if (this.selected?.pursuit.id !== pursuitId) return;
        this.resourceEventsLoadedFor = '';
        this.resourceEventsError = 'The resource ledger could not be loaded. Reload before relying on these records.';
      },
      complete: () => {
        if (!received && this.selected?.pursuit.id === pursuitId) {
          this.resourceEventsLoadedFor = '';
          this.resourceEventsError = 'No resource ledger was returned. Reload before relying on these records.';
        }
      },
    });
  }

  onResourceLedgerOpen(open: boolean): void {
    if (open) this.loadResourceEvents();
  }

  recordResourceEvent(): void {
    const pursuitId = this.selected?.pursuit.id || '';
    if (!pursuitId || this.detailLoading || this.resourceEventSaving || this.resourceEventForm.invalid) {
      return;
    }
    const value = this.resourceEventForm.getRawValue();
    const kind = value.kind as IPursuitResourceEvent['kind'];
    const selection = this.selected;
    const draftSnapshot = JSON.stringify(value);
    const quantity = Number(kind === 'effort_recorded' ? value.effortHours : value.spendEur);
    const scaledQuantity = Math.round(quantity * (kind === 'effort_recorded' ? 60 : 100));
    const occurredAt = value.occurredAt ? new Date(value.occurredAt) : undefined;
    if (!['effort_recorded', 'spend_incurred', 'spend_refund'].includes(kind) ||
        !Number.isFinite(quantity) || quantity <= 0 || !Number.isSafeInteger(scaledQuantity) || scaledQuantity <= 0 ||
        !String(value.idempotencyKey || '').trim() || (occurredAt && !Number.isFinite(occurredAt.getTime()))) {
      this.notification.warning('Invalid resource entry', 'Use a positive amount, a valid date and a nonempty recording key.');
      return;
    }
    const request: IPursuitResourceEventRequest = {
      kind,
      idempotencyKey: String(value.idempotencyKey || '').trim(),
      note: String(value.note || '').trim() || undefined,
      evidenceUri: String(value.evidenceUri || '').trim() || undefined,
      occurredAt: occurredAt?.toISOString(),
    };
    if (kind === 'effort_recorded') {
      request.effortHours = Number(value.effortHours || 0);
    } else {
      request.spendEur = Number(value.spendEur || 0);
    }
    this.resourceEventSaving = true;
    let received = false;
    this.resourceAppendSub = this.pursuitsService.appendResourceEvent(pursuitId, request).pipe(
      take(1), finalize(() => { this.resourceEventSaving = false; }),
    ).subscribe({
      next: (event) => {
        received = true;
        if (!event || typeof event.id !== 'string' || !event.id.trim() || event.pursuitId !== pursuitId ||
            event.idempotencyKey !== request.idempotencyKey || event.kind !== kind ||
            event.effortMinutes !== (kind === 'effort_recorded' ? scaledQuantity : 0) ||
            event.amountMinor !== (kind === 'effort_recorded' ? 0 : scaledQuantity) ||
            (kind !== 'effort_recorded' && event.currency !== 'EUR')) {
          this.notification.warning('Resource record unconfirmed', 'The receipt does not match the submitted entry. Keep its recording key and inspect the ledger before repeating it.');
          return;
        }
        if (this.selected !== selection || this.detailLoading || JSON.stringify(this.resourceEventForm.getRawValue()) !== draftSnapshot) {
          this.load();
          return;
        }
        this.resourceEventForm.patchValue({
          effortHours: 0.5,
          spendEur: 0,
          note: '',
          evidenceUri: '',
          occurredAt: '',
          idempotencyKey: this.newResourceIdempotencyKey(),
        });
        this.resourceEvents = [event, ...this.resourceEvents.filter((item) => item.id !== event.id)];
        this.resourceEventsLoadedFor = pursuitId;
        this.loadPursuitDetail(pursuitId, false);
        this.notification.success('Resource receipt returned', 'The returned entry matches the recording key and quantity. Current budget totals will be reloaded.');
      },
      error: () => {
        this.notification.error('Resource record unconfirmed', 'HAI could not confirm the entry. Keep its recording key and inspect the ledger before repeating it.');
      },
      complete: () => {
        if (!received) this.notification.warning('Resource record unconfirmed', 'No receipt was returned. Keep its recording key and inspect the ledger before repeating it.');
      },
    });
  }

  resourceUsageLabel(usage: IPursuitResourceUsage): string {
    switch (usage.state) {
      case 'within_limits': return 'Within limits';
      case 'reserved': return `${usage.activeReservations} active resource ${usage.activeReservations === 1 ? 'hold' : 'holds'}`;
      case 'exhausted': return 'Ceiling reached';
      case 'exceeded': return 'Ceiling exceeded';
      case 'unavailable': return 'Usage unavailable';
      default: return 'No ceiling configured';
    }
  }

  releaseReservation(reservationId: string): void {
    const pursuitId = this.selected?.pursuit.id || '';
    const reason = String(this.reservationReleaseReasons[reservationId] || '').trim();
    if (!pursuitId || this.detailLoading || this.releasingReservationId || this.reservationConfirmationId) {
      return;
    }
    if (reason.length < 12 || reason.length > 1000) {
      this.notification.warning('Release reason required', 'Explain in 12 to 1000 characters why the operation is confirmed stopped.');
      return;
    }
    const selection = this.selected;
    const reservation = selection?.resourceUsage?.reservations?.find(item => item.id === reservationId);
    if (!reservation || !reservation.operationId) {
      this.notification.warning('Reservation unavailable', 'Reload the pursuit and inspect the current hold before releasing it.');
      return;
    }
    this.reservationConfirmationId = reservationId;
    const confirmationRevision = ++this.reservationConfirmationRevision;
    this.modal.confirm({
      nzTitle: 'Release this resource hold?',
      nzContent: 'Only continue after confirming the worker or runtime is no longer active. The original hold remains in the immutable audit ledger.',
      nzOkText: 'Confirmed stopped - release',
      nzOkDanger: true,
      nzCancelText: 'Keep hold',
      nzOnCancel: () => {
        if (this.reservationConfirmationRevision === confirmationRevision) this.reservationConfirmationId = '';
      },
      nzOnOk: () => {
        if (this.reservationConfirmationRevision !== confirmationRevision || this.reservationConfirmationId !== reservationId || this.releasingReservationId) return;
        this.reservationConfirmationId = '';
        if (this.selected !== selection || this.detailLoading ||
            !this.selected?.resourceUsage?.reservations?.some(item => item.id === reservationId && item.operationId === reservation.operationId) ||
            String(this.reservationReleaseReasons[reservationId] || '').trim() !== reason) {
          this.notification.warning('Release needs review', 'The selection, hold or explanation changed. Inspect the current reservation before confirming again.');
          return;
        }
        this.releasingReservationId = reservationId;
        let received = false;
        this.reservationReleaseSub = this.pursuitsService.releaseResourceReservation(pursuitId, reservationId, reason).pipe(
          take(1), finalize(() => { this.releasingReservationId = ''; }),
        ).subscribe({
          next: (resourceUsage) => {
            received = true;
            if (resourceUsage?.available !== true || !Array.isArray(resourceUsage.reservations) ||
                resourceUsage.activeReservations !== resourceUsage.reservations.length ||
                resourceUsage.reservations.some(item => !item || typeof item.id !== 'string' || !item.id.trim() || item.id === reservationId)) {
              this.notification.warning('Release unconfirmed', 'The budget response did not confirm that the hold is absent. Keep the explanation and inspect current state before repeating.');
              return;
            }
            if (this.selected === selection && !this.detailLoading) {
              if (String(this.reservationReleaseReasons[reservationId] || '').trim() === reason) delete this.reservationReleaseReasons[reservationId];
              // Usage lacks a pursuit or settlement identity; reload scoped detail instead of adopting it.
              this.loadPursuitDetail(pursuitId, false);
              this.notification.info('Budget response received', 'The returned active list omits this hold. HAI is reloading its project; the settlement itself is not independently verified.');
            }
            this.load();
          },
          error: () => {
            this.notification.error('Release unconfirmed', 'HAI could not confirm the release. Keep the explanation and inspect current state before repeating.');
          },
          complete: () => {
            if (!received) this.notification.warning('Release unconfirmed', 'No budget response was returned. Keep the explanation and inspect current state before repeating.');
          },
        });
      },
    });
  }

  resourceEventValue(event: IPursuitResourceEvent): string {
    if (event.kind === 'effort_recorded') {
      return `${(event.effortMinutes / 60).toFixed(2)} h`;
    }
    const prefix = event.kind === 'spend_refund' ? '-' : '';
    return `${prefix}EUR ${(event.amountMinor / 100).toFixed(2)}`;
  }

  private resetResourceLedger(nextPursuitId: string): void {
    if (this.resourceEventsLoadedFor === nextPursuitId) {
      return;
    }
    this.resourceEventsSub?.unsubscribe();
    this.resourceEvents = [];
    this.resourceEventsLoading = false;
    this.resourceEventsError = '';
    this.resourceEventsLoadedFor = '';
  }

  private newResourceIdempotencyKey(): string {
    const cryptoApi = globalThis.crypto as Crypto | undefined;
    if (cryptoApi?.randomUUID) {
      return `resource-${cryptoApi.randomUUID()}`;
    }
    return `resource-${Date.now()}-${Math.random().toString(36).slice(2, 12)}`;
  }

  private setSelectedQuery(id?: string): void {
    this.router.navigate([], {
      relativeTo: this.route,
      queryParams: { selected: id || null, evidence: null, decision: null },
      queryParamsHandling: 'merge',
      replaceUrl: true,
    });
  }

  private openRequestedEvidence(): void {
    const uri = this.requestedEvidenceUri;
    if (!uri || !this.selected?.pursuit?.id || this.detailLoading) {
      return;
    }
    this.requestedEvidenceUri = '';
    this.router.navigate([], {
      relativeTo: this.route,
      queryParams: { evidence: null },
      queryParamsHandling: 'merge',
      replaceUrl: true,
    });
    this.openSource(uri);
  }

  isHighlightedDecision(decision: IPursuitDecision): boolean {
    return !!this.highlightedDecisionId && decision.id === this.highlightedDecisionId;
  }

  highlightedDecision(): IPursuitDecision | undefined {
    if (!this.highlightedDecisionId || !this.selected?.decisionQueue?.length) {
      return undefined;
    }
    return this.selected.decisionQueue.find((decision) => decision.id === this.highlightedDecisionId);
  }

  createPursuit(): void {
    if (this.createForm.invalid) {
      this.createForm.markAllAsTouched();
      return;
    }
    this.creating = true;
    const value = this.createForm.value;
    this.pursuitsService.create({
      title: value.title,
      description: value.description,
      projectKey: value.projectKey,
      domain: value.domain,
      whyItMatters: value.whyItMatters,
      desiredOutcome: value.desiredOutcome,
      currentStateSummary: value.currentStateSummary,
      completionDefinition: value.completionDefinition,
      successCriteria: this.criteriaFromLines(value.successCriteriaText, value.completionDefinition || value.desiredOutcome || value.title),
      stopConditions: this.stopConditionsFromLines(value.stopConditionsText),
      dependencies: this.dependenciesFromLines(value.dependenciesText),
      targetAt: this.localDateToRFC3339(value.targetAt),
      reviewCadenceDays: Number(value.reviewCadenceDays || 0),
      resourceLimits: this.resourceLimitsFromForm(value),
      sourceOfCreation: 'dashboard',
    }).subscribe({
      next: (pursuit) => {
        // The create response is authoritative. Do not let an earlier dashboard
        // request overwrite it before the new pursuit is rendered.
        this.dashboardSub?.unsubscribe();
        this.pursuitsSub?.unsubscribe();
        this.pursuits = [pursuit, ...this.pursuits.filter((item) => item.id !== pursuit.id)];
        this.loading = false;
        this.creating = false;
        this.showCreate = false;
        this.createForm.reset({ domain: 'personal_productivity', reviewCadenceDays: 7, maxEffortHours: 0, maxSpendEur: 0, maxParallelWorkflows: 0 });
        this.notification.success('Pursuit created', 'HAI can now link workflows, memory, sources, and approvals to it.');
        this.selectPursuit(pursuit);
      },
      error: (error) => {
        this.creating = false;
        this.notification.error('Create failed', error?.error?.error || 'The pursuit could not be created.');
      },
    });
  }

  openContextEditor(): void {
    if (!this.selected) {
      return;
    }
    const pursuit = this.selected.pursuit;
    this.contextEditorRevision++;
    this.contextForm.reset({
      description: pursuit.description || '',
      whyItMatters: pursuit.whyItMatters || '',
      desiredOutcome: pursuit.desiredOutcome || '',
      currentStateSummary: pursuit.currentStateSummary || '',
      nextRecommendedAction: pursuit.nextRecommendedAction || '',
      completionDefinition: pursuit.completionDefinition || '',
      targetAt: this.rfc3339ToLocalDate(pursuit.targetAt),
      reviewCadenceDays: pursuit.reviewCadenceDays || 0,
      maxEffortHours: pursuit.resourceLimits?.maxEffortHours || 0,
      maxSpendEur: pursuit.resourceLimits?.maxSpendEur || 0,
      maxParallelWorkflows: pursuit.resourceLimits?.maxParallelWorkflows || 0,
      resourceNotes: pursuit.resourceLimits?.notes || '',
    });
    this.draftSuccessCriteria = (pursuit.successCriteria || []).map((item) => ({ ...item }));
    this.draftStopConditions = (pursuit.stopConditions || []).map((item) => ({ ...item }));
    this.draftDependencies = (pursuit.dependencies || []).map((item) => ({ ...item }));
    this.showContextEditor = true;
  }

  savePursuitContext(): void {
    if (!this.selected || this.detailLoading || this.contextSaving || this.lifecycleRunningId || this.resolvingDecisionId) {
      return;
    }
    const pursuitID = this.selected.pursuit.id;
    const selection = this.selected;
    const editorRevision = this.contextEditorRevision;
    const draftSnapshot = this.contextDraftSnapshot();
    const value = this.contextForm.value;
    if (!this.draftSuccessCriteria.length || !this.draftStopConditions.length) {
      this.notification.error('Outcome contract incomplete', 'Keep at least one success criterion and one stop condition.');
      return;
    }
    const request = {
      description: value.description,
      whyItMatters: value.whyItMatters,
      desiredOutcome: value.desiredOutcome,
      currentStateSummary: value.currentStateSummary,
      nextRecommendedAction: value.nextRecommendedAction,
      completionDefinition: value.completionDefinition,
      successCriteria: this.draftSuccessCriteria.map((item) => ({ ...item, description: item.description.trim() })),
      stopConditions: this.draftStopConditions.map((item) => ({ ...item, description: item.description.trim() })),
      dependencies: this.draftDependencies.map((item) => ({ ...item, label: item.label.trim() })),
      targetAt: this.localDateToRFC3339(value.targetAt),
      reviewCadenceDays: Number(value.reviewCadenceDays || 0),
      resourceLimits: this.resourceLimitsFromForm(value),
    };
    this.contextSaving = true;
    let received = false;
    this.contextSaveSub = this.pursuitsService.update(pursuitID, request).pipe(
      take(1), finalize(() => { this.contextSaving = false; }),
    ).subscribe({
      next: (pursuit) => {
        received = true;
        if (pursuit?.id !== pursuitID || !this.contextResponseMatches(pursuit, request)) {
          this.notification.warning('Context update unconfirmed', 'The returned context did not match your submitted goals, boundaries, or limits. Your draft is retained; inspect current state before repeating this action.');
          return;
        }
        if (this.selected === selection && !this.detailLoading && this.contextEditorRevision === editorRevision &&
            this.contextDraftSnapshot() === draftSnapshot) {
          this.selected = { ...this.selected, pursuit };
          this.loadProjectDossier(pursuit.projectKey);
          this.showContextEditor = false;
          this.notification.success('Context response received', 'The server returned the updated pursuit record.');
        }
        this.load();
      },
      error: () => {
        this.notification.error('Context update unconfirmed', 'HAI could not confirm the update. Your draft is retained; inspect current state before repeating this action.');
      },
      complete: () => {
        if (!received) this.notification.warning('Context update unconfirmed', 'No pursuit record was returned. Your draft is retained; inspect current state before repeating this action.');
      },
    });
  }

  private contextDraftSnapshot(): string {
    return JSON.stringify([this.contextForm.getRawValue(), this.draftSuccessCriteria, this.draftStopConditions, this.draftDependencies]);
  }

  private contextResponseMatches(pursuit: IPursuit, request: any): boolean {
    const text = (value: unknown): string => typeof value === 'string' ? value.trim() : '';
    const dateMatches = (actual: unknown, expected: unknown): boolean => {
      if (!expected) return actual === undefined || actual === null || actual === '';
      return typeof actual === 'string' && typeof expected === 'string' &&
        Number.isFinite(Date.parse(actual)) && Date.parse(actual) === Date.parse(expected);
    };
    for (const key of ['description', 'whyItMatters', 'desiredOutcome', 'currentStateSummary', 'nextRecommendedAction', 'completionDefinition'] as const) {
      if (text(pursuit[key]) !== text(request[key])) return false;
    }
    if (pursuit.reviewCadenceDays !== request.reviewCadenceDays || !dateMatches(pursuit.targetAt, request.targetAt)) return false;
    if (!pursuit.resourceLimits || typeof pursuit.resourceLimits !== 'object') return false;
    for (const key of ['maxEffortHours', 'maxSpendEur', 'maxParallelWorkflows'] as const) {
      const actual = pursuit.resourceLimits[key] ?? 0;
      if (!Number.isFinite(actual) || actual !== request.resourceLimits[key]) return false;
    }
    if (text(pursuit.resourceLimits.notes) !== text(request.resourceLimits.notes)) return false;
    // Compare submitted contract fields, allowing backend-generated stop timestamps.
    const matchesItems = (actual: any, expected: any[], fields: string[], defaultStatus: string): boolean => {
      if (!Array.isArray(actual) || actual.length !== expected.length) return false;
      return expected.every((item, index) => {
        const returned = actual[index];
        if (!returned || typeof returned !== 'object') return false;
        if (text(returned.status).toLowerCase() !== (text(item.status).toLowerCase() || defaultStatus)) return false;
        return fields.every((field) => {
          if (field === 'evidenceRequired') return returned[field] === (item[field] ?? false);
          if (field.endsWith('At')) return field === 'dueAt' || item[field]
            ? dateMatches(returned[field], item[field]) : true;
          if (field === 'verificationStatus') return text(returned[field]).toLowerCase() === text(item[field]).toLowerCase();
          return text(returned[field]) === text(item[field]);
        });
      });
    };
    return matchesItems(pursuit.successCriteria, request.successCriteria,
      ['id', 'description', 'evidenceRequired', 'evidenceUri', 'verificationStatus', 'waiverReason'], 'pending') &&
      matchesItems(pursuit.stopConditions, request.stopConditions,
        ['id', 'description', 'reason', 'triggeredAt', 'resolvedAt'], 'monitoring') &&
      matchesItems(pursuit.dependencies, request.dependencies,
        ['id', 'label', 'owner', 'relatedPursuitId', 'evidenceUri', 'reason', 'dueAt'], 'pending');
  }

  addSuccessCriterion(): void {
    this.draftSuccessCriteria.push({ id: this.contractId(), description: '', status: 'pending', evidenceRequired: true });
  }

  removeSuccessCriterion(index: number): void {
    this.draftSuccessCriteria.splice(index, 1);
  }

  addStopCondition(): void {
    this.draftStopConditions.push({ id: this.contractId(), description: '', status: 'monitoring' });
  }

  removeStopCondition(index: number): void {
    this.draftStopConditions.splice(index, 1);
  }

  addDependency(): void {
    this.draftDependencies.push({ id: this.contractId(), label: '', status: 'pending' });
  }

  removeDependency(index: number): void {
    this.draftDependencies.splice(index, 1);
  }

  private criteriaFromLines(value: string, fallback: string): IPursuitSuccessCriterion[] {
    const lines = this.nonEmptyLines(value);
    return (lines.length ? lines : [fallback]).map((description) => ({
      id: this.contractId(),
      description,
      status: 'pending',
      evidenceRequired: true,
    }));
  }

  private stopConditionsFromLines(value: string): IPursuitStopCondition[] {
    const lines = this.nonEmptyLines(value);
    return (lines.length ? lines : ['Stop and request review when the pursuit no longer serves its outcome or exceeds an approved boundary.']).map((description) => ({
      id: this.contractId(),
      description,
      status: 'monitoring',
    }));
  }

  private dependenciesFromLines(value: string): IPursuitDependency[] {
    return this.nonEmptyLines(value).map((label) => ({ id: this.contractId(), label, status: 'pending' }));
  }

  private nonEmptyLines(value: string): string[] {
    return String(value || '').split(/\r?\n/).map((item) => item.trim()).filter(Boolean);
  }

  private resourceLimitsFromForm(value: any): IPursuitResourceLimits {
    return {
      maxEffortHours: Math.max(0, Number(value.maxEffortHours || 0)),
      maxSpendEur: Math.max(0, Number(value.maxSpendEur || 0)),
      maxParallelWorkflows: Math.max(0, Number(value.maxParallelWorkflows || 0)),
      notes: String(value.resourceNotes || '').trim(),
    };
  }

  private localDateToRFC3339(value: string): string {
    return value ? `${value}T00:00:00Z` : '';
  }

  private rfc3339ToLocalDate(value?: string): string {
    return value ? value.slice(0, 10) : '';
  }

  private contractId(): string {
    return typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
      ? crypto.randomUUID()
      : `contract-${Date.now()}-${Math.random().toString(16).slice(2)}`;
  }

  runIntake(): void {
    if (!this.selected || this.detailLoading || this.intakeRunning || this.lifecycleRunningId || this.resolvingDecisionId) return;
    if (this.intakeForm.invalid || !String(this.intakeForm.value.input || '').trim()) {
      this.intakeForm.markAllAsTouched();
      return;
    }
    const selection = this.selected;
    const pursuitId = selection.pursuit.id;
    const draft = this.intakeForm.getRawValue();
    const draftSnapshot = JSON.stringify(draft);
    const existingIds = new Set((selection.workflows || []).map(item => item.id));
    const requiresReview = selection.pursuit.riskLevel === 'high' || selection.pursuit.autonomyLevel === 'approve_before_execute';
    this.intakeRunning = true;
    let received = false;
    this.intakeSub = this.pursuitsService.intake(pursuitId, {
      ...draft,
      projectKey: selection.pursuit.projectKey,
      requiresReview,
      reviewReason: requiresReview ? 'Pursuit intake requires approval before execution.' : undefined,
      trigger: 'pursuit_dashboard',
    }).pipe(take(1), finalize(() => { this.intakeRunning = false; })).subscribe({
      next: (detail) => {
        received = true;
        const linkedWork = Array.isArray(detail?.workflows) && Array.isArray(detail?.links)
          ? detail.workflows.filter(item => item && typeof item.id === 'string' && item.id.trim() &&
            detail.links.some(link => link && link.pursuitId === pursuitId && link.linkType === 'workflow' &&
              link.linkId === item.id && link.relationship === 'operational_work')) : [];
        if (detail?.pursuit?.id !== pursuitId || !linkedWork.length || (requiresReview &&
            linkedWork.filter(item => !existingIds.has(item.id)).some(item => item.requiresApproval !== true ||
              !['pending', 'approved', 'rejected'].includes(item.approvalStatus)))) {
          this.notification.warning('Intake unconfirmed', 'The response did not confirm linked work with the required approval context. Inspect current state before repeating this action.');
          return;
        }
        if (this.selected === selection && !this.detailLoading && JSON.stringify(this.intakeForm.getRawValue()) === draftSnapshot) {
          this.selected = detail;
          if (linkedWork.some(item => !existingIds.has(item.id))) {
            this.notification.success('Linked workflow returned', 'The response contains a new linked workflow. Execution is not confirmed.');
          } else {
            this.notification.info('Existing linked work', 'The response contains existing work; no new workflow is confirmed.');
          }
        }
        this.load();
      },
      error: () => {
        this.notification.error('Intake unconfirmed', 'HAI could not confirm intake. Your input is retained; inspect current state before repeating this action.');
      },
      complete: () => {
        if (!received) this.notification.warning('Intake unconfirmed', 'No intake record was returned. Your input is retained; inspect current state before repeating this action.');
      },
    });
  }

  routeIntake(): void {
    if (this.routedIntakeRunning || this.detailLoading || this.lifecycleRunningId || this.resolvingDecisionId) return;
    if (this.routedIntakeForm.invalid || !String(this.routedIntakeForm.value.input || '').trim()) {
      this.routedIntakeForm.markAllAsTouched();
      return;
    }
    const selection = this.selected;
    const requestedPursuitId = this.requestedPursuitId;
    const draft = this.routedIntakeForm.getRawValue();
    const draftSnapshot = JSON.stringify(draft);
    this.routedIntakeRunning = true;
    let received = false;
    this.routedIntakeSub = this.pursuitsService.routeIntake({
      ...draft,
      trigger: 'pursuit_dashboard_global_intake',
    }).pipe(take(1), finalize(() => { this.routedIntakeRunning = false; })).subscribe({
      next: (result) => {
        received = true;
        const pursuitId = result?.pursuitId;
        const detail = result?.detail;
        const candidateMode = result?.mode === 'candidate_created' || result?.mode === 'matched_candidate';
        const workflowMode = result?.mode === 'matched_existing' || result?.mode === 'matched_after_workflow';
        const source = String(detail?.pursuit?.sourceOfCreation || '').trim().toLowerCase();
        const candidateRecord = source === 'pursuit_candidate' || source.includes('_pursuit_candidate');
        const linkedWork = Array.isArray(detail?.workflows) && Array.isArray(detail?.links) &&
          detail.workflows.some(item => item && typeof item.id === 'string' && item.id.trim() &&
            detail.links.some(link => link && link.pursuitId === pursuitId && link.linkType === 'workflow' && link.linkId === item.id));
        const flagsMatch = result?.mode === 'candidate_created' ? result.createdCandidate === true && result.matched === false
          : result?.createdCandidate === false && result?.matched === true;
        if (typeof pursuitId !== 'string' || !pursuitId.trim() || detail?.pursuit?.id !== pursuitId || !flagsMatch ||
            (candidateMode ? !candidateRecord : !workflowMode || candidateRecord || !linkedWork)) {
          this.notification.warning('Routing unconfirmed', 'The response did not confirm a matching candidate or linked workflow. Your input is retained; inspect current state before repeating this action.');
          return;
        }
        this.load();
        if (this.selected !== selection || this.requestedPursuitId !== requestedPursuitId || this.detailLoading ||
            JSON.stringify(this.routedIntakeForm.getRawValue()) !== draftSnapshot) return;
        this.routedIntakeForm.patchValue({ input: '', sourceLabel: '', sourceUri: '' });
        if (candidateMode) {
          this.notification.info(
            'Pursuit candidate needs review',
            'The returned pursuit is a candidate awaiting explicit acceptance. This response does not authorize execution.'
          );
        } else {
          this.notification.success(
            'Input routed',
            'The response confirms a pursuit with linked workflow context. Execution is not confirmed.'
          );
        }
        this.selectPursuitById(pursuitId, true);
      },
      error: () => {
        this.notification.error('Routing unconfirmed', 'HAI could not confirm routing. Your input is retained; inspect current state before repeating this action.');
      },
      complete: () => {
        if (!received) this.notification.warning('Routing unconfirmed', 'No routing record was returned. Your input is retained; inspect current state before repeating this action.');
      },
    });
  }

  refreshSelectedSummary(): void {
    if (!this.selected || this.detailLoading || this.summaryLoading || this.lifecycleRunningId || this.resolvingDecisionId) {
      return;
    }
    const pursuitId = this.selected.pursuit.id;
    const selection = this.selected;
    this.summaryLoading = true;
    let received = false;
    this.summarySub = this.pursuitsService.refreshSummary(pursuitId).pipe(
      take(1), finalize(() => { this.summaryLoading = false; }),
    ).subscribe({
      next: (detail) => {
        received = true;
        if (detail?.pursuit?.id !== pursuitId || typeof detail.summary?.currentState !== 'string' || !detail.summary.currentState.trim()) {
          this.notification.warning('Summary unconfirmed', 'The response did not contain a matching pursuit summary. Reload the pursuit before repeating this action.');
          return;
        }
        // A newer detail read or mutation must not be replaced by this older snapshot.
        if (this.selected === selection && !this.detailLoading) {
          this.selected = detail;
          this.notification.success('Summary refreshed', 'The server returned an updated summary for the requested pursuit.');
        }
        this.load();
      },
      error: () => {
        this.notification.error('Summary unconfirmed', 'HAI could not confirm the summary update. Reload the pursuit before repeating this action.');
      },
      complete: () => {
        if (!received) this.notification.warning('Summary unconfirmed', 'No summary was returned. Reload the pursuit before repeating this action.');
      },
    });
  }

  completeSelectedReview(): void {
    if (!this.selected || this.detailLoading || this.reviewing || this.lifecycleRunningId || this.resolvingDecisionId) {
      return;
    }
    const request = {
      action: 'complete',
      note: 'Scheduled pursuit review completed from the dashboard.',
    } as const;
    this.observeScheduledReview(this.selected, request.note, 'pursuit.reviewed', this.pursuitsService.review(this.selected.pursuit.id, request));
  }

  snoozeSelectedReview(days: number = 3): void {
    if (!this.selected || this.detailLoading || this.reviewing || this.lifecycleRunningId || this.resolvingDecisionId) {
      return;
    }
    if (!Number.isInteger(days) || days < 1 || days > 90) {
      this.notification.warning('Invalid review delay', 'Choose a whole number of days between 1 and 90.');
      return;
    }
    const request = {
      action: 'snooze',
      snoozeDays: days,
      note: `Scheduled pursuit review snoozed for ${days} days from the dashboard.`,
    } as const;
    this.observeScheduledReview(this.selected, request.note, 'pursuit.review_snoozed', this.pursuitsService.review(this.selected.pursuit.id, request));
  }

  private observeScheduledReview(selection: IPursuitDetail, note: string, eventType: string, response: Observable<IPursuitDetail>): void {
    const pursuitId = selection.pursuit.id;
    const previousAuditIds = new Set((selection.activity || []).map(item => item.id));
    this.reviewing = true;
    let received = false;
    this.reviewSub = response.pipe(take(1), finalize(() => { this.reviewing = false; })).subscribe({
      next: (detail) => {
        received = true;
        const nextReviewAt = detail?.pursuit?.nextReviewAt;
        const audit = Array.isArray(detail?.activity) && detail.activity.some(item => item &&
          typeof item.id === 'string' && item.id.trim() && !previousAuditIds.has(item.id) &&
          item.pursuitId === pursuitId && item.eventType === eventType && item.message === note);
        if (detail?.pursuit?.id !== pursuitId || typeof nextReviewAt !== 'string' ||
            !Number.isFinite(Date.parse(nextReviewAt)) || !audit) {
          this.notification.warning('Review unconfirmed', 'The response did not confirm the review record and schedule. Inspect current state before repeating this action.');
          return;
        }
        if (this.selected === selection && !this.detailLoading) {
          this.selected = detail;
          this.notification.success('Review recorded', 'The returned record includes a matching review entry and next review date.');
        }
        this.load();
      },
      error: () => {
        this.notification.error('Review unconfirmed', 'HAI could not confirm the review update. Inspect current state before repeating this action.');
      },
      complete: () => {
        if (!received) this.notification.warning('Review unconfirmed', 'No review record was returned. Inspect current state before repeating this action.');
      },
    });
  }

  createFirstWorkflowPlan(): void {
    if (!this.selected || this.detailLoading || this.planning || this.reviewing || this.lifecycleRunningId || this.resolvingDecisionId) {
      return;
    }
	if (!this.canCreateFirstWorkflow(this.selected)) {
	  this.notification.info('Planning blocked', this.planningBlockReason(this.selected));
	  return;
	}
    const selection = this.selected;
    const pursuitId = selection.pursuit.id;
    const requiresReview = selection.pursuit.riskLevel === 'high' || selection.pursuit.autonomyLevel === 'approve_before_execute';
    const existingIds = new Set((selection.workflows || []).map(item => item.id));
    this.planning = true;
    let received = false;
    this.planningSub = this.pursuitsService.plan(pursuitId, {
      requiresReview,
      reviewReason: requiresReview
        ? 'High-risk pursuit planning requires Robert approval before execution.'
        : 'First pursuit workflow plan created from pursuit dashboard.',
    }).pipe(take(1), finalize(() => { this.planning = false; })).subscribe({
      next: (detail) => {
        received = true;
        const plans = Array.isArray(detail?.workflows) && Array.isArray(detail?.links)
          ? detail.workflows.filter(item => item && typeof item.id === 'string' && item.id.trim() &&
            item.sourceType === 'pursuit' && item.sourceId === pursuitId &&
            detail.links.some(link => link && link.pursuitId === pursuitId && link.linkType === 'workflow' &&
              link.linkId === item.id && link.relationship === 'first_workflow_plan')) : [];
        if (detail?.pursuit?.id !== pursuitId || !plans.length || (requiresReview &&
            plans.some(item => item.requiresApproval !== true || !['pending', 'approved', 'rejected'].includes(item.approvalStatus)))) {
          this.notification.warning('Planning unconfirmed', 'The response did not confirm a linked plan with the required approval context. Inspect current state before repeating this action.');
          return;
        }
        if (this.selected === selection && !this.detailLoading) {
          this.selected = detail;
          if (plans.some(item => !existingIds.has(item.id))) {
            this.notification.success('Workflow plan returned', 'The response includes a new linked plan. This does not confirm execution.');
          } else {
            this.notification.info('Existing workflow plan', 'HAI returned the existing linked plan; no new workflow is confirmed.');
          }
        }
        this.load();
      },
      error: () => {
        this.notification.error('Planning unconfirmed', 'HAI could not confirm the plan. Inspect current state before repeating this action.');
      },
      complete: () => {
        if (!received) this.notification.warning('Planning unconfirmed', 'No plan was returned. Inspect current state before repeating this action.');
      },
    });
  }

  canCreateFirstWorkflow(detail: IPursuitDetail | null | undefined = this.selected): boolean {
    return !!detail?.actionQueues?.systemReady?.some((action) =>
      action.label === 'Create the first workflow item for this pursuit'
    );
  }

  planningBlockReason(detail: IPursuitDetail | null | undefined = this.selected): string {
    if (!detail) {
      return 'Select a pursuit before creating operational work.';
    }
    return detail.resourceUsage?.blockingReason
      || detail.blockers?.[0]?.reason
      || 'HAI has not authorized a system-ready planning action for this pursuit.';
  }

  prepareDelegationPackage(): void {
    if (!this.selected || this.detailLoading || this.delegationLoading) {
      return;
    }
    const pursuitId = this.selected.pursuit.id;
    this.delegationPackage = undefined;
    this.delegationLoading = true;
    let received = false;
    this.delegationSub = this.pursuitsService.delegationPackage(pursuitId).pipe(
      take(1), finalize(() => { this.delegationLoading = false; }),
    ).subscribe({
      next: (delegationPackage) => {
        received = true;
        if (this.selected?.pursuit.id !== pursuitId) return;
        const hasInstructions = (values: unknown): boolean => Array.isArray(values) && values.length > 0 &&
          values.every(value => typeof value === 'string' && value.trim().length > 0);
        const ready = delegationPackage?.ready;
        if (!delegationPackage || delegationPackage.pursuitId !== pursuitId ||
            typeof ready !== 'boolean' || delegationPackage.status !== (ready ? 'ready' : 'not_ready') ||
            typeof delegationPackage.reason !== 'string' || !delegationPackage.reason.trim() ||
            !hasInstructions(delegationPackage.allowedActions) || !hasInstructions(delegationPackage.blockedActions) ||
            !hasInstructions(delegationPackage.deliveryRequirements) ||
            (ready && (!Array.isArray(delegationPackage.workItems) || !delegationPackage.workItems.length ||
              !delegationPackage.workItems.every(item => item && typeof item.workflowId === 'string' &&
                item.workflowId.trim() && typeof item.instructions === 'string' && item.instructions.trim())))) {
          this.notification.warning('VA brief unavailable', 'The response did not contain a matching, bounded delegation package. Refresh before preparing it again.');
          return;
        }
        this.delegationPackage = delegationPackage;
        const title = delegationPackage.ready ? 'VA brief ready' : 'VA brief blocked';
        this.notification.info(title, delegationPackage.reason);
      },
      error: () => {
        this.notification.error('VA brief unavailable', 'HAI could not load the delegation package. Refresh before trying again.');
      },
      complete: () => {
        if (!received) this.notification.warning('VA brief unavailable', 'No delegation package was returned. Refresh before trying again.');
      },
    });
  }

  archiveSelected(): void {
    if (!this.selected || this.detailLoading || this.lifecycleRunningId || this.resolvingDecisionId) {
      return;
    }
    if (this.isClosedPursuit(this.selected.pursuit)) {
      this.reopenSelected();
      return;
    }
    const pursuitId = this.selected.pursuit.id;
    this.lifecycleRunningId = pursuitId;
    this.observePursuitLifecycle(pursuitId, true, this.pursuitsService.archive(pursuitId, true));
  }

  reopenSelected(): void {
    if (!this.selected || this.detailLoading || this.lifecycleRunningId || this.resolvingDecisionId ||
      !this.isClosedPursuit(this.selected.pursuit)) {
      return;
    }
    const pursuit = this.selected.pursuit;
    this.lifecycleRunningId = pursuit.id;
    this.observePursuitLifecycle(pursuit.id, false, this.pursuitsService.reopen(pursuit.id));
  }

  private observePursuitLifecycle(pursuitId: string, archived: boolean, response: Observable<IPursuit>): void {
    let received = false;
    this.lifecycleSub = response.pipe(
      take(1),
      finalize(() => { this.lifecycleRunningId = ''; }),
    ).subscribe({
      next: (pursuit) => {
        received = true;
        if (pursuit?.id !== pursuitId || pursuit.archived !== archived ||
          pursuit.status !== (archived ? 'archived' : 'active') || (!archived && pursuit.completionState !== 'open')) {
          this.notification.warning('Pursuit change not confirmed', 'The returned record does not confirm the requested state. Refresh the original pursuit before repeating the request.');
          return;
        }
        if (archived && (this.selected?.pursuit?.id === pursuitId ||
          (!this.selected && this.requestedPursuitId === pursuitId))) {
          this.pursuitDetailSub?.unsubscribe();
          this.decisionDetailSub?.unsubscribe();
          this.evidenceResolutionSub?.unsubscribe();
          this.inspectedRuntimeEvidence = undefined;
          this.inspectedEvidence = undefined;
          this.inspectedAction = undefined;
          this.selected = undefined;
          this.requestedPursuitId = '';
          this.detailLoading = false;
          this.setSelectedQuery();
        } else if (!archived && this.selected?.pursuit?.id === pursuitId) {
          this.selected = { ...this.selected, pursuit };
          this.loadPursuitDetail(pursuitId, false);
        }
        this.notification.success('Pursuit state record received', `The returned record reports this pursuit as ${archived ? 'archived' : 'active'}. No task execution is implied.`);
        this.load();
      },
      error: () => {
        this.notification.error('Pursuit change not confirmed', 'HAI could not confirm the state response. Refresh the original pursuit before repeating the request.');
      },
      complete: () => {
        if (!received) {
          this.notification.warning('Pursuit change not confirmed', 'No pursuit state response was received. Refresh the original pursuit before repeating the request.');
        }
      },
    });
  }

  isClosedPursuit(pursuit: IPursuit): boolean {
    return pursuit.archived || pursuit.status === 'completed' || pursuit.completionState === 'verified';
  }

  openWorkflow(workflowId?: string): void {
    this.router.navigate(['/workflow-engine'], { queryParams: workflowId ? { workflowId } : undefined });
  }

  verifySelectedEvidence(): void {
    if (!this.selected) {
      return;
    }
    const pursuit = this.selected.pursuit;
    this.router.navigate(['/grounded-answers'], {
      queryParams: {
        pursuitId: pursuit.id,
        projectKey: pursuit.projectKey || undefined,
        question: pursuit.nextRecommendedAction || pursuit.desiredOutcome || pursuit.title,
      },
    });
  }

  openAssistant(): void {
    if (!this.selected) {
      return;
    }
    const pursuit = this.selected.pursuit;
    this.router.navigate(['/task-blueprint'], {
      queryParams: {
        pursuitId: pursuit.id,
        projectKey: pursuit.projectKey || undefined,
        request: pursuit.nextRecommendedAction || pursuit.desiredOutcome || pursuit.title,
      },
    });
  }

  openAction(action: IPursuitAction): void {
    if (action.workflowId) {
      this.openWorkflow(action.workflowId);
      return;
    }
    this.inspectedAction = action;
  }

  openDigestLane(lane: 'robert' | 'va' | 'system' | 'waiting'): void {
    const queues = this.selected?.actionQueues;
    const actions = lane === 'robert'
      ? queues?.needsRobert
      : lane === 'va'
        ? queues?.vaReady
        : lane === 'system'
          ? queues?.systemReady
          : queues?.waiting;
    if (!actions?.length) {
      const label = lane === 'robert' ? 'Robert-only' : lane === 'va' ? 'VA-ready' : lane === 'system' ? 'System-ready' : 'Waiting';
      this.notification.info(`${label} lane`, `There is no ${label.toLowerCase()} action to open for this pursuit.`);
      return;
    }
    this.openAction(actions[0]);
  }

  closeAction(): void {
    this.inspectedAction = undefined;
  }

  openAutomations(): void {
    this.router.navigate(['/home']);
  }

  canStopAutomation(automation: IPursuitAutomation): boolean {
    return automation.launchType === 'agent_runtime' &&
      !!automation.id &&
      ['deepseek-harness', 'hermes', 'odysseus', 'openclaw'].includes((automation.runtimeType || '').toLowerCase());
  }

  stopRuntimeAutomation(automation: IPursuitAutomation): void {
    if (!this.canStopAutomation(automation) || this.stoppingAutomationId) {
      return;
    }
    this.stoppingAutomationId = automation.id;
    const pursuitId = this.selected?.pursuit?.id || '';
    let received = false;
    this.runtimeStopSub = this.automationsService.stopRuntimeTask(automation.id).pipe(
      take(1),
      finalize(() => { this.stoppingAutomationId = ''; }),
    ).subscribe({
      next: (result) => {
        received = true;
        const uri = typeof result?.evidenceUri === 'string' ? result.evidenceUri : '';
        const validEvidence = /^automation-launch:\/\/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(uri) &&
          !uri.endsWith('00000000-0000-0000-0000-000000000000');
        const bound = typeof result?.runtimeId === 'string' && result.runtimeId.trim().toLowerCase() === automation.runtimeType?.trim().toLowerCase() &&
          typeof result?.taskId === 'string' && result.taskId.trim() !== '' && validEvidence;
        const evidence = validEvidence ? ` Evidence: ${uri}` : '';
        if (!bound) {
          this.notification.warning('Runtime stop not confirmed', `The response has no verifiable task receipt. Review runtime evidence before retrying.${evidence}`);
        } else if (result.status === 'stopped') {
          this.notification.success('Runtime stop confirmed', `The stored response reports this task as stopped.${evidence}`);
        } else if (result.status === 'cancellation_requested' || result.status === 'stopping') {
          this.notification.info('Runtime stop requested', `Cancellation was requested; downstream termination is not yet confirmed.${evidence}`);
        } else if (result.status === 'failed') {
          this.notification.error('Runtime stop failed', `Review the stored attempt before trying another cancellation.${evidence}`);
        } else {
          this.notification.warning(result.status === 'blocked' ? 'Runtime stop blocked' : 'Runtime stop not confirmed', `Review runtime evidence before retrying. No stop completion is claimed.${evidence}`);
        }
        this.reloadSelectedAfterDecision();
      },
      error: (error) => {
        const recovery = error?.error?.recovery;
        const candidate = typeof recovery?.candidateEvidenceUri === 'string' ? recovery.candidateEvidenceUri : '';
        const validCandidate = recovery?.automationId === automation.id &&
          recovery?.reconciliationRequired === true && recovery?.retryAllowed === false &&
          typeof recovery?.candidateStopEventId === 'string' &&
          /^automation-launch:\/\/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(candidate) &&
          !candidate.endsWith('00000000-0000-0000-0000-000000000000') &&
          candidate === `automation-launch://${recovery.candidateStopEventId}`;
        this.notification.error(
          'Runtime stop not confirmed',
          'HAI could not confirm the stop request. Review runtime evidence before retrying.' +
            (validCandidate ? ` Candidate evidence (persistence unconfirmed): ${candidate}. Do not repeat this stop before reconciliation.` : '')
        );
        if (validCandidate) {
          if (pursuitId) {
            this.runtimeStopRecovery = { pursuitId, automationId: automation.id, runtimeId: (automation.runtimeType || '').trim().toLowerCase(), evidenceUri: candidate };
          }
          this.reloadSelectedAfterDecision();
        }
      },
      complete: () => {
        if (!received) {
          this.notification.warning('Runtime stop not confirmed', 'No response was received. Review runtime evidence before retrying.');
        }
      },
    });
  }

  runtimeEvidenceUri(attempt: IAutomationLaunchEvent): string {
    return attempt?.id ? `automation-launch://${attempt.id}` : '';
  }

  get visibleRuntimeStopRecovery(): { pursuitId: string; automationId: string; runtimeId: string; evidenceUri: string } | undefined {
    const recovery = this.runtimeStopRecovery;
    return recovery && this.selected?.pursuit?.id === recovery.pursuitId &&
      this.selected.automations?.some((automation) => automation.id === recovery.automationId) ? recovery : undefined;
  }

  reviewRuntimeStopRecovery(): void {
    const recovery = this.visibleRuntimeStopRecovery;
    if (!recovery) {
      return;
    }
    const attempt = this.runtimeAttemptFromEvidenceUri(recovery.evidenceUri);
    if (!attempt || attempt.automationId !== recovery.automationId ||
      (attempt.runtimeType || '').trim().toLowerCase() !== recovery.runtimeId ||
      !['agent_runtime_stop', 'agent_runtime_stop_intent'].includes(attempt.launchType || '')) {
      this.notification.warning('Stop evidence unavailable', 'The exact stop record is not available in this pursuit. Cancellation remains unconfirmed.');
      this.reloadSelectedAfterDecision();
      return;
    }
    this.inspectRuntimeEvidence(attempt);
  }

  runtimeEvidenceLabel(attempt: IAutomationLaunchEvent): string {
    return this.runtimeEvidenceUri(attempt) || 'No persisted launch-event id';
  }

  runtimeEvidenceTitle(attempt: IAutomationLaunchEvent): string {
    const uri = this.runtimeEvidenceUri(attempt);
    if (!uri) {
      return 'This runtime attempt is missing a persisted launch-event ID, so it cannot be used as exact evidence.';
    }
    return `Exact runtime evidence URI: ${uri}`;
  }

  copyRuntimeEvidence(attempt: IAutomationLaunchEvent): void {
    this.copyEvidenceUri(this.runtimeEvidenceUri(attempt));
  }

  copyEvidenceUri(uri?: string): void {
    const value = (uri || '').trim();
    if (!value) {
      this.notification.warning('Evidence missing', 'This record does not have a stable evidence URI.');
      return;
    }
    if (!navigator?.clipboard?.writeText) {
      this.notification.info('Evidence URI', value);
      return;
    }
    navigator.clipboard.writeText(value).then(
      () => this.notification.success('Evidence URI copied', value),
      () => this.notification.info('Evidence URI', value)
    );
  }

  inspectRuntimeEvidence(attempt: IAutomationLaunchEvent): void {
    if (!attempt?.id) {
      this.notification.warning('Runtime evidence missing', 'This runtime attempt is missing a persisted launch-event ID.');
      return;
    }
    this.inspectedRuntimeEvidence = attempt;
  }

  closeRuntimeEvidence(): void {
    this.inspectedRuntimeEvidence = undefined;
  }

  inspectEvidence(record: IPursuitEvidenceResolution): void {
    this.inspectedEvidence = record;
  }

  closeEvidence(): void {
    this.inspectedEvidence = undefined;
  }

  runtimeEvidenceType(attempt?: IAutomationLaunchEvent): string {
    if (!attempt) {
      return 'runtime';
    }
    if ((attempt.launchType || '').toLowerCase() === 'agent_runtime_stop') {
      return 'runtime stop';
    }
    return attempt.launchType || 'runtime attempt';
  }

  runtimeEvidenceDuration(attempt?: IAutomationLaunchEvent): string {
    if (!attempt) {
      return '-';
    }
    return `${attempt.durationMs || 0} ms`;
  }

  runtimeTraceHeadline(trace?: IAutomationRuntimeRouteTrace): string {
    if (!trace) {
      return 'No routing trace recorded';
    }
    return [
      trace.intent || trace.runtimeId || 'runtime task',
      trace.riskLevel ? `${trace.riskLevel} risk` : '',
      trace.executionMode || '',
    ].filter(Boolean).join(' / ');
  }

  runtimeTraceSummary(trace?: IAutomationRuntimeRouteTrace): string {
    if (!trace) {
      return 'This runtime attempt did not return a route trace.';
    }
    const parts = [
      this.runtimeTraceCount(trace.recommendedSkills, 'skill'),
      this.runtimeTraceCount(trace.visibleProviders, 'provider'),
      this.runtimeTraceCount(trace.visibleTools, 'tool'),
      this.runtimeTraceCount(trace.relevantMaps, 'map'),
      this.runtimeTraceCount(trace.blockedSurfaces, 'blocked surface'),
      this.runtimeTraceCount(trace.requiredControls, 'control'),
      this.runtimeTraceCount(trace.validationChecklist, 'validation check'),
    ].filter(Boolean);
    return parts.length ? parts.join(' / ') : 'Route trace recorded without expanded planning details.';
  }

  runtimeTraceItems(values?: string[], limit: number = 4): string[] {
    return (values || []).filter(Boolean).slice(0, limit);
  }

  runtimeTraceMore(values?: string[], limit: number = 4): number {
    return Math.max(0, (values || []).filter(Boolean).length - limit);
  }

  runtimeTraceTitle(label: string, values?: string[]): string {
    const items = (values || []).filter(Boolean);
    return items.length ? `${label}: ${items.join(', ')}` : `${label}: none recorded`;
  }

  private runtimeTraceCount(values: string[] | undefined, label: string): string {
    const count = (values || []).filter(Boolean).length;
    if (!count) {
      return '';
    }
    return `${count} ${label}${count === 1 ? '' : 's'}`;
  }

  addLink(): void {
    if (!this.selected || this.detailLoading || this.linkSaving || this.detachingLinkId || this.linkConfirmationId || this.lifecycleRunningId || this.resolvingDecisionId) return;
    if (!this.selected || this.linkForm.invalid) {
      this.linkForm.markAllAsTouched();
      return;
    }
    const selection = this.selected;
    const pursuitID = selection.pursuit.id;
    const snapshot = JSON.stringify(this.linkForm.getRawValue());
    const value = this.linkForm.getRawValue();
    const request = {
      linkType: String(value.linkType || '').trim(), linkId: String(value.linkId || '').trim(),
      relationship: String(value.relationship || '').trim() || 'related',
      sourceUri: String(value.sourceUri || '').trim(), sourceLabel: String(value.sourceLabel || '').trim(),
      confidence: Number(value.confidence),
    };
    if (!this.linkTypes.includes(request.linkType) || !request.linkId ||
        !Number.isFinite(request.confidence) || request.confidence <= 0 || request.confidence > 1) {
      this.notification.warning('Check link details', 'Choose a record type, a non-empty record ID, and confidence greater than zero and no more than one.');
      return;
    }
    this.linkSaving = true;
    let received = false;
    this.linkSaveSub = this.pursuitsService.link(pursuitID, request).pipe(
      take(1), finalize(() => { this.linkSaving = false; }),
    ).subscribe({
      next: (link) => {
        received = true;
        if (!link?.id || link.pursuitId !== pursuitID || link.linkType !== request.linkType ||
            link.linkId !== request.linkId || link.relationship !== request.relationship ||
            (link.sourceUri || '') !== request.sourceUri || (link.sourceLabel || '') !== request.sourceLabel ||
            link.confidence !== request.confidence) {
          this.notification.warning('Link unconfirmed', 'The returned record did not match the requested link. Your input is retained; inspect current links before retrying.');
          return;
        }
        if (this.selected === selection && !this.detailLoading && JSON.stringify(this.linkForm.getRawValue()) === snapshot) {
          this.linkForm.patchValue({ linkId: '', sourceUri: '', sourceLabel: '', confidence: 0.7 });
          this.notification.info('Link response received', 'The server returned the matching link. Reloading this pursuit for current state.');
          this.loadPursuitDetail(pursuitID, false);
        }
        this.load();
      },
      error: () => {
        this.notification.error('Link unconfirmed', 'HAI could not confirm this link. Your input is retained; inspect current links before retrying.');
      },
      complete: () => {
        if (!received) this.notification.warning('Link unconfirmed', 'No link record was returned. Your input is retained; inspect current links before retrying.');
      },
    });
  }

  deleteLink(linkId: string): void {
    if (!this.selected || !linkId || this.detailLoading || this.linkSaving || this.detachingLinkId ||
        this.linkConfirmationId || this.lifecycleRunningId || this.resolvingDecisionId) return;
    const selection = this.selected;
    const pursuitId = selection.pursuit.id;
    const link = selection.links?.find(item => item.id === linkId && item.pursuitId === pursuitId);
    if (!link) {
      this.notification.warning('Link unavailable', 'Reload this pursuit and inspect the current link before detaching it.');
      return;
    }
    const linkSnapshot = JSON.stringify(link);
    this.linkConfirmationId = linkId;
    const revision = ++this.linkConfirmationRevision;
    this.modal.confirm({
      nzTitle: 'Detach this record from the pursuit?',
      nzContent: 'This removes only the pursuit association, not the source record or document. HAI will read this pursuit again to check whether the association is absent.',
      nzOkText: 'Detach record', nzOkDanger: true, nzCancelText: 'Keep link', nzAutofocus: 'cancel',
      nzOnCancel: () => {
        if (this.linkConfirmationRevision === revision) this.linkConfirmationId = '';
      },
      nzOnOk: () => {
        if (this.linkConfirmationRevision !== revision || this.linkConfirmationId !== linkId || this.detachingLinkId) return;
        this.linkConfirmationId = '';
        if (this.selected !== selection || this.detailLoading || this.linkSaving || this.lifecycleRunningId || this.resolvingDecisionId ||
            JSON.stringify(this.selected?.links?.find(item => item.id === linkId)) !== linkSnapshot) {
          this.notification.warning('Detach needs review', 'The selected pursuit or link changed. Inspect the current association before confirming again.');
          return;
        }
        this.detachingLinkId = linkId;
        let received = false;
        this.linkDetachSub = this.pursuitsService.deleteLink(pursuitId, linkId).pipe(
          take(1), finalize(() => { this.detachingLinkId = ''; }),
        ).subscribe({
          next: (absent) => {
            received = true;
            if (absent !== true) {
              this.notification.warning('Detach unconfirmed', 'The current pursuit still lists this association. Inspect current state before retrying.');
              return;
            }
            if (this.selected === selection && !this.detailLoading) {
              this.notification.info('Link absent in current read', 'The server no longer lists this association. The source record is not deleted.');
              this.loadPursuitDetail(pursuitId, false);
            }
            this.load();
          },
          error: () => {
            this.notification.error('Detach unconfirmed', 'HAI could not confirm the current association state. Inspect current links before retrying; the source record is not deleted.');
          },
          complete: () => {
            if (!received) this.notification.warning('Detach unconfirmed', 'No scoped link readback was returned. Inspect current links before retrying.');
          },
        });
      },
    });
  }

  openSource(uri?: string): void {
    const value = (uri || '').trim();
    if (!value) {
      return;
    }
    if (/[\u0000-\u001f\u007f]/.test(value) || /%(?:0[0-9a-f]|1[0-9a-f]|7f)/i.test(value) ||
        /^(javascript|vbscript|data|blob):/i.test(value)) {
      this.notification.warning('Source link blocked', 'This source reference is not safe to open. Inspect its origin instead.');
      return;
    }
    const runtimeAttempt = this.runtimeAttemptFromEvidenceUri(value);
    if (runtimeAttempt) {
      this.inspectRuntimeEvidence(runtimeAttempt);
      return;
    }
    const href = this.browserSourceHref(value);
    if (href) {
      window.open(href, '_blank', 'noopener,noreferrer');
      return;
    }
    if (/^(https?|mailto|tel):/i.test(value)) {
      this.notification.warning('Source link blocked', 'This web or contact link is malformed or includes embedded credentials. Inspect its origin instead.');
      return;
    }
    if (this.selected?.pursuit?.id) {
      this.resolveEvidence(value);
      return;
    }
    this.notification.warning('Source context required', 'Select the pursuit containing this local or internal reference before opening it.');
  }

  private resolveEvidence(uri: string): void {
    if (!this.selected?.pursuit?.id || this.resolvingEvidenceUri || this.detailLoading) {
      return;
    }
    const pursuitId = this.selected.pursuit.id;
    let received = false;
    this.resolvingEvidenceUri = uri;
    this.evidenceResolutionSub = this.pursuitsService.resolveEvidence(pursuitId, uri).pipe(
      take(1),
      finalize(() => { this.resolvingEvidenceUri = ''; }),
    ).subscribe({
      next: (record) => {
        received = true;
        if (this.selected?.pursuit?.id !== pursuitId) {
          return;
        }
        const runtimeUri = uri.toLowerCase().startsWith('automation-launch://');
        const returnedUri = typeof record?.uri === 'string' ? record.uri : '';
        if (!(runtimeUri ? returnedUri.toLowerCase() === uri.toLowerCase() : returnedUri === uri) ||
          (record?.runtimeAttempt && (!runtimeUri ||
            this.runtimeEvidenceUri(record.runtimeAttempt).toLowerCase() !== uri.toLowerCase()))) {
          this.notification.warning('Evidence unavailable', 'The response does not match the requested evidence. Refresh this pursuit before retrying.');
          return;
        }
        if (record.runtimeAttempt) {
          this.inspectRuntimeEvidence(record.runtimeAttempt);
          return;
        }
        this.inspectEvidence(record);
      },
      error: () => {
        if (this.selected?.pursuit?.id === pursuitId) {
          this.notification.warning('Evidence unavailable', 'HAI could not resolve this source. Refresh the pursuit before retrying.');
        }
      },
      complete: () => {
        if (!received && this.selected?.pursuit?.id === pursuitId) {
          this.notification.warning('Evidence unavailable', 'No evidence record was received. Refresh the pursuit before retrying.');
        }
      },
    });
  }

  private browserSourceHref(uri: string): string | undefined {
    try {
      const parsed = new URL(uri);
      if (parsed.protocol === 'http:' || parsed.protocol === 'https:') {
        return /^https?:\/\//i.test(uri) && !uri.includes('\\') && parsed.hostname && !parsed.username && !parsed.password
          ? parsed.href : undefined;
      }
      if (parsed.protocol === 'mailto:') {
        return /^[^\s@]+@[^\s@]+$/.test(parsed.pathname) ? parsed.href : undefined;
      }
      if (parsed.protocol === 'tel:') {
        return /^\+?[0-9][0-9(). -]{0,49}$/.test(parsed.pathname) && !parsed.search && !parsed.hash ? parsed.href : undefined;
      }
    } catch {
      return undefined;
    }
    return undefined;
  }

  private runtimeAttemptFromEvidenceUri(uri: string): IAutomationLaunchEvent | undefined {
    const prefix = 'automation-launch://';
    if (!uri.startsWith(prefix)) {
      return undefined;
    }
    const id = uri.slice(prefix.length).trim().toLowerCase();
    if (!id || !this.selected?.runtimeAttempts?.length) {
      return undefined;
    }
    return this.selected.runtimeAttempts.find((attempt) => (attempt.id || '').toLowerCase() === id);
  }

  statusColor(status: string): string {
    switch (status) {
      case 'completed':
        return 'success';
      case 'blocked':
        return 'error';
      case 'waiting':
        return 'warning';
      case 'archived':
        return 'default';
      default:
        return 'processing';
    }
  }

  riskColor(risk: string): string {
    switch (risk) {
      case 'high':
        return 'error';
      case 'medium':
        return 'warning';
      case 'low':
        return 'success';
      default:
        return 'default';
    }
  }

  count(key: string): number {
    return Number(this.dashboard?.counts?.[key] || 0);
  }

  topQueue(): IPursuitListItem[] {
    return [
      ...(this.dashboard?.needsRobert || []),
      ...(this.dashboard?.reviewDue || []),
      ...(this.dashboard?.planningNeeded || []),
      ...(this.dashboard?.blocked || []),
      ...(this.dashboard?.stale || []),
    ].slice(0, 6);
  }

  queueItemLabel(item: IPursuitListItem): string {
    if (item.blocked) {
      return 'blocked';
    }
    if (item.needsRobert) {
      return 'needs Robert';
    }
    if (item.reviewDue) {
      return 'review due';
    }
    if (item.planningNeeded) {
      return 'needs planning';
    }
    if (item.stale) {
      return 'stale';
    }
    return 'attention';
  }

  queueItemColor(item: IPursuitListItem): string {
    if (item.blocked) {
      return 'error';
    }
    if (item.needsRobert || item.reviewDue || item.planningNeeded) {
      return 'warning';
    }
    return 'processing';
  }

  actionOwnerLabel(action: IPursuitAction): string {
    return action.requiresApproval ? `${action.owner} approval` : action.owner;
  }

  blockerOwnerLabel(blocker: IPursuitBlocker): string {
    return blocker.followUpAt ? `${blocker.owner} · follow up ${new Date(blocker.followUpAt).toLocaleDateString()}` : blocker.owner;
  }

  canResolveDecision(decision: IPursuitDecision): boolean {
    return decision.status === 'pending' &&
      (
        decision.decisionType === 'approval' ||
        decision.decisionType === 'proposal' ||
        decision.decisionType === 'pursuit_next_action' ||
        decision.decisionType === 'pursuit_candidate_review' ||
        decision.decisionType === 'runtime_attempt_review' ||
        decision.decisionType === 'pursuit_completion_review'
      );
  }

  resolveDecision(decision: IPursuitDecision, approved: boolean): void {
    if (!this.selected || this.detailLoading || this.lifecycleRunningId || this.resolvingDecisionId ||
      typeof decision?.id !== 'string' || !decision.id.trim()) {
      return;
    }
    const current = Array.isArray(this.selected.decisionQueue)
      ? this.selected.decisionQueue.find((entry) => entry?.id === decision.id) : undefined;
    if (!current || !this.canResolveDecision(current) || current.decisionType !== decision.decisionType ||
      (current.workflowId || '') !== (decision.workflowId || '')) {
      this.notification.warning('Decision needs refresh', 'This card no longer matches a pending decision in the selected pursuit. Refresh before reviewing it.');
      return;
    }
    decision = current;
    if (decision.decisionType === 'approval' && decision.workflowId) {
      this.resolveWorkflowApproval(decision, approved);
      return;
    }
    if (decision.decisionType === 'proposal' && decision.workflowId) {
      this.resolveWorkflowProposal(decision, approved);
      return;
    }
    if (decision.decisionType === 'pursuit_next_action') {
      this.resolvePursuitNextAction(decision, approved);
      return;
    }
    if (decision.decisionType === 'runtime_attempt_review') {
      this.resolveRuntimeAttemptReview(decision, approved);
      return;
    }
    if (decision.decisionType === 'pursuit_completion_review') {
      this.resolvePursuitCompletionReview(decision, approved);
      return;
    }
    if (decision.decisionType === 'pursuit_candidate_review') {
      this.resolvePursuitCandidateReview(decision, approved);
      return;
    }
    this.notification.info('Open workflow', 'Open the linked workflow to inspect this decision record.');
  }

  private resolveWorkflowApproval(decision: IPursuitDecision, approved: boolean): void {
    if (!this.selected || !decision.workflowId) {
      return;
    }
    const pursuitId = this.selected.pursuit.id;
    this.resolvingDecisionId = decision.id;
    this.observeWorkflowDecision(pursuitId, decision.workflowId, approved, this.workflowService.resolveApproval(decision.workflowId, {
      approved,
      note: approved ? decision.yesConsequence : decision.noConsequence,
      actor: 'Robert',
    }));
  }

  private resolveWorkflowProposal(decision: IPursuitDecision, approved: boolean): void {
    if (!this.selected || !decision.workflowId) {
      return;
    }
    const proposalId = this.proposalIdFromDecision(decision);
    if (!proposalId) {
      this.notification.error('Proposal unavailable', 'The proposal ID is missing from this decision card.');
      return;
    }
    const pursuitId = this.selected.pursuit.id;
    this.resolvingDecisionId = decision.id;
    this.observeWorkflowDecision(pursuitId, decision.workflowId, approved, this.workflowService.resolveProposal(decision.workflowId, proposalId, {
      approved,
      status: approved ? 'approved' : 'rejected',
      selectedOption: approved ? decision.yesLabel : decision.noLabel,
      note: approved ? decision.yesConsequence : decision.noConsequence,
      actor: 'Robert',
    }), proposalId);
  }

  private observeWorkflowDecision(pursuitId: string, workflowId: string, approved: boolean,
    response: Observable<IWorkflowRecord>, proposalId?: string): void {
    const expectedStatus = approved ? 'approved' : 'rejected';
    const decisionType = proposalId ? 'proposal' : 'approval';
    let received = false;
    this.decisionMutationSub = response.pipe(
      take(1),
      finalize(() => { this.resolvingDecisionId = ''; }),
    ).subscribe({
      next: (record) => {
        received = true;
        const auditMatches = Array.isArray(record?.decisions) && record.decisions.some((audit) =>
          typeof audit?.id === 'string' && !!audit.id.trim() && audit.workflowId === workflowId &&
          audit.decisionType === decisionType && audit.decision === expectedStatus && audit.approved === approved);
        const stateMatches = proposalId
          ? Array.isArray(record?.proposals) && record.proposals.some((proposal) =>
            proposal?.id === proposalId && proposal.workflowId === workflowId && proposal.status === expectedStatus)
          : record?.item?.approvalStatus === expectedStatus;
        if (record?.item?.id !== workflowId || !auditMatches || !stateMatches) {
          this.notification.warning('Workflow decision not confirmed', 'The response does not contain the matching decision record. Review the original workflow before repeating the request.');
          return;
        }
        this.notification.success('Workflow decision record received', `The returned workflow and audit report this ${decisionType} as ${expectedStatus}. Execution status is separate.`);
        if (this.selected?.pursuit?.id === pursuitId) {
          this.reloadSelectedAfterDecision();
        } else {
          this.load();
        }
      },
      error: () => {
        this.notification.error('Workflow decision not confirmed', 'HAI could not confirm the decision response. Review the original workflow before repeating the request.');
      },
      complete: () => {
        if (!received) {
          this.notification.warning('Workflow decision not confirmed', 'No workflow decision response was received. Review the original workflow before repeating the request.');
        }
      },
    });
  }

  private resolvePursuitNextAction(decision: IPursuitDecision, approved: boolean): void {
    if (!this.selected) {
      return;
    }
    const pursuitId = this.selected.pursuit.id;
    this.resolvingDecisionId = decision.id;
    this.observePursuitDecision(pursuitId, decision.id, this.pursuitsService.resolveDecision(pursuitId, {
      decisionId: decision.id,
      decisionType: decision.decisionType,
      approved,
      reason: decision.reason,
      note: approved
        ? decision.yesConsequence || `Robert approved the proposed next action: ${decision.recommended}`
        : decision.noConsequence || `Robert rejected the proposed next action: ${decision.recommended}`,
      evidenceUri: decision.evidenceUri,
      evidenceLabel: decision.evidenceLabel,
    }));
  }

  private resolveRuntimeAttemptReview(decision: IPursuitDecision, approved: boolean): void {
    if (!this.selected) {
      return;
    }
    const pursuitId = this.selected.pursuit.id;
    this.resolvingDecisionId = decision.id;
    this.observePursuitDecision(pursuitId, decision.id, this.pursuitsService.resolveDecision(pursuitId, {
      decisionId: decision.id,
      decisionType: decision.decisionType,
      approved,
      reason: decision.reason,
      note: approved ? decision.yesConsequence || decision.recommended : decision.noConsequence || 'Keep runtime attempt blocked until reviewed.',
      evidenceUri: decision.evidenceUri,
      evidenceLabel: decision.evidenceLabel,
    }));
  }

  private resolvePursuitCompletionReview(decision: IPursuitDecision, approved: boolean): void {
    if (!this.selected) {
      return;
    }
    const pursuitId = this.selected.pursuit.id;
    this.resolvingDecisionId = decision.id;
    this.observePursuitDecision(pursuitId, decision.id, this.pursuitsService.resolveDecision(pursuitId, {
      decisionId: decision.id,
      decisionType: decision.decisionType,
      approved,
      reason: decision.reason,
      note: approved
        ? decision.yesConsequence || 'Robert approved verified pursuit completion.'
        : decision.noConsequence || 'Robert kept the pursuit active after completion review.',
      evidenceUri: decision.evidenceUri,
      evidenceLabel: decision.evidenceLabel,
    }));
  }

  private observePursuitDecision(pursuitId: string, decisionId: string, response: Observable<IPursuitDetail>): void {
    let received = false;
    this.decisionMutationSub = response.pipe(
      take(1),
      finalize(() => { this.resolvingDecisionId = ''; }),
    ).subscribe({
      next: (detail) => {
        received = true;
        if (detail?.pursuit?.id !== pursuitId || !Array.isArray(detail.decisionQueue) ||
          detail.decisionQueue.some((decision) => !decision || typeof decision.id !== 'string' ||
            typeof decision.status !== 'string' || decision.id === decisionId)) {
          this.notification.warning('Decision not confirmed', 'The response does not confirm this decision. Refresh the original pursuit before repeating the request.');
          return;
        }
        if (this.selected?.pursuit?.id === pursuitId) {
          this.selected = detail;
        }
        this.notification.success('Decision record refreshed', 'This decision is no longer pending in the returned pursuit record. Review related work for its execution status.');
        this.load();
      },
      error: () => {
        this.notification.error('Decision not confirmed', 'HAI could not confirm the decision response. Refresh the original pursuit before repeating the request.');
      },
      complete: () => {
        if (!received) {
          this.notification.warning('Decision not confirmed', 'No decision response was received. Refresh the original pursuit before repeating the request.');
        }
      },
    });
  }

  private resolvePursuitCandidateReview(decision: IPursuitDecision, approved: boolean): void {
    if (!this.selected) {
      return;
    }
    const pursuitId = this.selected.pursuit.id;
    this.resolvingDecisionId = decision.id;
    if (approved) {
      this.observeCandidateAcceptance(pursuitId, decision.id, this.pursuitsService.acceptCandidate(pursuitId, {
        requiresReview: decision.riskLevel === 'high',
        reviewReason: decision.reason,
      }));
      return;
    }
    let received = false;
    this.decisionMutationSub = this.pursuitsService.archive(pursuitId, true).pipe(
      take(1),
      finalize(() => { this.resolvingDecisionId = ''; }),
    ).subscribe({
      next: (pursuit) => {
        received = true;
        if (pursuit?.id !== pursuitId || pursuit.archived !== true || pursuit.status !== 'archived') {
          this.notification.warning('Archive not confirmed', 'The response does not confirm this candidate was archived. Refresh the original pursuit before repeating the request.');
          return;
        }
        if (this.selected?.pursuit?.id === pursuitId) {
          this.evidenceResolutionSub?.unsubscribe();
          this.inspectedRuntimeEvidence = undefined;
          this.inspectedEvidence = undefined;
          this.inspectedAction = undefined;
          this.selected = undefined;
        }
        this.notification.success('Candidate archive recorded', 'The returned pursuit record reports this candidate as archived.');
        this.load();
      },
      error: () => {
        this.notification.error('Archive not confirmed', 'HAI could not confirm the archive response. Refresh the original pursuit before repeating the request.');
      },
      complete: () => {
        if (!received) {
          this.notification.warning('Archive not confirmed', 'No archive response was received. Refresh the original pursuit before repeating the request.');
        }
      },
    });
  }

  private observeCandidateAcceptance(pursuitId: string, decisionId: string, response: Observable<IPursuitDetail>): void {
    let received = false;
    this.decisionMutationSub = response.pipe(
      take(1),
      finalize(() => { this.resolvingDecisionId = ''; }),
    ).subscribe({
      next: (detail) => {
        received = true;
        const source = String(detail?.pursuit?.sourceOfCreation || '').trim().toLowerCase();
        const acceptedSourceIsContradictory = source === 'pursuit_candidate' || source.includes('_pursuit_candidate');
        const candidateDecisionPending = Array.isArray(detail?.decisionQueue) &&
          detail.decisionQueue.some((entry) => entry?.id === decisionId && entry.status === 'pending');
        if (detail?.pursuit?.id !== pursuitId || detail.pursuit.status !== 'active' ||
            acceptedSourceIsContradictory || candidateDecisionPending) {
          this.notification.warning('Candidate acceptance not confirmed', 'The returned pursuit does not confirm that the candidate was accepted. Refresh this pursuit before repeating the request.');
          return;
        }
        if (this.selected?.pursuit?.id === pursuitId) {
          this.selected = detail;
        }
        this.notification.success('Candidate accepted', 'The returned pursuit is now active for governed HAI handling. Review linked workflow state separately; execution is not confirmed.');
        this.load();
      },
      error: () => {
        this.notification.error('Candidate acceptance not confirmed', 'HAI could not confirm the acceptance response. Refresh the original pursuit before repeating the request.');
      },
      complete: () => {
        if (!received) {
          this.notification.warning('Candidate acceptance not confirmed', 'No acceptance response was received. Refresh the original pursuit before repeating the request.');
        }
      },
    });
  }

  private proposalIdFromDecision(decision: IPursuitDecision): string {
    return decision.id.startsWith('proposal:') ? decision.id.substring('proposal:'.length) : '';
  }

  private reloadSelectedAfterDecision(): void {
    if (!this.selected) {
      this.load();
      return;
    }
    this.loadPursuitDetail(this.selected.pursuit.id, false);
    this.load();
  }

  trackPursuit(_: number, pursuit: IPursuit): string {
    return pursuit.id;
  }
}
