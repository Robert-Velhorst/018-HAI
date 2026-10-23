import { ChangeDetectionStrategy, Component, Inject, OnDestroy, OnInit, QueryList, ViewChildren } from '@angular/core';
import { FormBuilder, FormGroup, Validators } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { defer, EMPTY, Observable, Subject, Subscription, take, takeUntil, throwIfEmpty } from 'rxjs';
import { finalize, timeout } from 'rxjs/operators';
import {
  IResearchCandidateEvidence,
  IVerificationResult,
  IVerificationRun,
} from '../../models/verification.model.interface';
import { IResearchProbe, IResearchResult, IResearchStatus } from '../../models/research.model.interface';
import { IRAGFlowProbeResult, IRAGFlowResult, IRAGFlowStatus } from '../../models/ragflow.model.interface';
import { IAnythingLLMResult, IAnythingLLMStatus } from '../../models/anythingllm.model.interface';
import { AnythingLLMService } from '../../services/anythingllm.service';
import { RAGFlowService } from '../../services/ragflow.service';
import { ResearchService } from '../../services/research.service';
import { VERIFICATION_SERVICE_TOKEN } from '../../services/verification/verification.service.token';
import { IVerificationService } from '../../services/verification.service.interface';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { HaiProgressiveSectionComponent } from '../../control-room/progressive-section.component';

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-grounded-answers',
    templateUrl: './grounded-answers.component.html',
    styleUrls: ['./grounded-answers.component.scss'],
    standalone: false
})
export class GroundedAnswersComponent implements OnInit, OnDestroy {
  @ViewChildren(HaiProgressiveSectionComponent) progressiveSections?: QueryList<HaiProgressiveSectionComponent>;

  readonly moduleId = 'grounded-answers';
  private readonly destroyed$ = new Subject<void>();
  private destroyed = false;
  private detailSubscription?: Subscription;
  result?: IVerificationResult;
  runs: IVerificationRun[] = [];
  runsLoading = false;
  runsUnavailable = false;
  runsLoaded = false;
  loading = false;
  answerError = '';
  researchLoading = false;
  researchProbeLoading = false;
  researchStatusLoading = false;
  researchStatusLoaded = false;
  researchStatus?: IResearchStatus;
  researchProbe?: IResearchProbe;
  researchResults: IResearchResult[] = [];
  selectedResearchCandidate?: IResearchResult;
  ragflowLoading = false;
  ragflowProbeLoading = false;
  ragflowStatusLoading = false;
  ragflowStatusLoaded = false;
  ragflowStatus?: IRAGFlowStatus;
  ragflowProbe?: IRAGFlowProbeResult;
  ragflowResults: IRAGFlowResult[] = [];
  selectedRAGFlowCandidate?: IRAGFlowResult;
  anythingLLMLoading = false;
  anythingLLMStatusLoading = false;
  anythingLLMStatusLoaded = false;
  anythingLLMStatus?: IAnythingLLMStatus;
  anythingLLMResults: IAnythingLLMResult[] = [];
  selectedAnythingLLMCandidate?: IAnythingLLMResult;

  answerForm: FormGroup = this.fb.group({
    question: ['What did connected sources say about source-grounded task context?', [Validators.required]],
    projectKey: ['018-HAI'],
    pursuitId: [''],
    mode: ['grounded'],
    includeSensitive: [false],
    includeRagflowCandidates: [false],
    includeResearchCandidates: [false],
    allowMemoryUpdate: [false],
    evidenceLabel: [''],
    evidenceUri: [''],
    evidenceSnippet: [''],
    official: [false],
    primary: [true],
  });

  constructor(
    private fb: FormBuilder,
    @Inject(VERIFICATION_SERVICE_TOKEN)
    private verificationService: IVerificationService,
    private researchService: ResearchService,
    private ragflowService: RAGFlowService,
    private anythingLLMService: AnythingLLMService,
    private notification: NzNotificationService,
    private route: ActivatedRoute,
    private router: Router,
    private viewPreferences: ModuleViewPreferencesService
  ) {}

  get isAdvancedView(): boolean {
    return this.viewPreferences.get(this.moduleId).mode === 'advanced';
  }

  ngOnDestroy(): void {
    this.destroyed = true;
    this.destroyed$.next();
    this.destroyed$.complete();
  }

  private observe<T>(request: () => Observable<T>, settle: () => void, deadlineMs = 6000): Observable<T> {
    if (this.destroyed) return EMPTY;
    return defer(request).pipe(
      timeout(deadlineMs), take(1), throwIfEmpty(), takeUntil(this.destroyed$), finalize(settle),
    );
  }

  ngOnInit(): void {
    const params = this.route.snapshot.queryParamMap;
    this.answerForm.patchValue({
      pursuitId: params.get('pursuitId') || '',
      projectKey: params.get('projectKey') || this.answerForm.value.projectKey,
      question: params.get('question') || this.answerForm.value.question,
    });
  }

  onSourceDiscoveryOpen(open: boolean): void {
    if (!open) return;
    this.loadResearchStatus();
    this.loadRAGFlowStatus();
    this.loadAnythingLLMStatus();
  }

  onVerificationHistoryOpen(open: boolean): void {
    if (open && !this.runsLoaded) this.loadRuns();
  }

  openAdvancedSection(sectionId: string): void {
    this.viewPreferences.setMode(this.moduleId, 'advanced');
    this.viewPreferences.setSection(this.moduleId, sectionId, true);
    this.progressiveSections?.find((section) => section.sectionId === sectionId)?.setOpen(true);
    void this.router.navigate(['/grounded-answers'], {
      fragment: sectionId,
      queryParamsHandling: 'preserve',
    });
  }

  usedEvidenceCount(result: IVerificationResult): number {
    return (result.evidence || []).filter((source) => source.used).length;
  }

  rejectedEvidenceCount(result: IVerificationResult): number {
    return (result.evidence || []).filter((source) => Boolean(source.rejectReason) || source.used === false).length;
  }

  answer(): void {
    if (this.destroyed || this.loading || this.answerForm.invalid) {
      return;
    }
    this.detailSubscription?.unsubscribe();
    this.loading = true;
    this.answerError = '';
    const snippet = String(this.answerForm.value.evidenceSnippet || '').trim();
    this.observe(() => this.verificationService.answer({
        question: this.answerForm.value.question,
        projectKey: this.answerForm.value.projectKey,
        pursuitId: this.answerForm.value.pursuitId,
        mode: this.answerForm.value.mode,
        includeSensitive: this.answerForm.value.includeSensitive,
        includeRagflowCandidates: this.answerForm.value.includeRagflowCandidates,
        includeResearchCandidates: this.answerForm.value.includeResearchCandidates,
        allowMemoryUpdate: this.answerForm.value.allowMemoryUpdate,
        externalEvidence: snippet
          ? [
              {
                sourceType: this.selectedRAGFlowCandidate
                  ? 'ragflow_candidate_evidence'
                  : this.selectedAnythingLLMCandidate
                    ? 'anythingllm_candidate_evidence'
                    : this.selectedResearchCandidate
                      ? 'local_research'
                      : 'manual',
                sourceLabel: this.answerForm.value.evidenceLabel,
                sourceUri: this.answerForm.value.evidenceUri,
                snippet,
                authority: this.answerForm.value.official ? 'official' : 'operator_supplied',
                freshness: 'operator_supplied',
                official: this.answerForm.value.official,
                primary: this.answerForm.value.primary,
              },
            ]
          : [],
      }), () => this.loading = false, 30000)
      .subscribe({
        next: (result) => {
          this.result = result;
          this.loading = false;
          if (result.pursuitLinkError) {
            this.notification.warning('Verification saved, pursuit link failed', result.pursuitLinkError);
          } else if (result.pursuitLinked) {
            this.notification.success('Verification linked', 'The verification run is now visible in the selected pursuit evidence timeline.');
          }
          if (this.viewPreferences.get(this.moduleId).openSections['verification-history']) this.loadRuns();
        },
        error: () => {
          this.loading = false;
          this.answerError = 'Verification completion could not be confirmed. Refresh history before retrying; a run or permitted memory update may already have been saved.';
          this.notification.error('Error', 'Failed to create grounded answer.');
        },
      });
  }

  loadRuns(): void {
    if (this.destroyed || this.runsLoading) {
      return;
    }
    this.runsLoading = true;
    this.observe(() => this.verificationService.runs(), () => this.runsLoading = false).subscribe({
      next: (runs) => {
        this.runs = runs;
        this.runsUnavailable = false;
        this.runsLoaded = true;
      },
      error: () => {
        this.runsUnavailable = true;
        this.runsLoaded = false;
      },
    });
  }

  loadResearchStatus(force = false): void {
    if (this.destroyed || this.researchStatusLoading || (this.researchStatusLoaded && !force)) return;
    this.researchStatusLoading = true;
    this.observe(() => this.researchService.status(), () => this.researchStatusLoading = false).subscribe({
      next: (status) => {
        this.researchStatus = status;
        this.researchStatusLoaded = true;
        this.researchStatusLoading = false;
      },
      error: () => {
        this.researchStatus = undefined;
        this.researchStatusLoaded = false;
        this.researchStatusLoading = false;
      },
    });
  }

  probeResearch(): void {
    if (this.destroyed || !this.researchStatus?.configured || this.researchProbeLoading) return;
    this.researchProbeLoading = true;
    this.observe(() => this.researchService.probe(), () => this.researchProbeLoading = false).subscribe({
      next: (probe) => {
        this.researchProbeLoading = false;
        if (probe?.reachable !== true) {
          this.researchProbe = undefined;
          this.notification.warning('Local SearXNG unavailable', 'The endpoint did not report a successful health check. No source candidates were added.');
          return;
        }
        this.researchProbe = probe;
        this.notification.success('Local SearXNG reachable', 'Endpoint health passed. Search result provenance and verification are still required.');
      },
      error: () => {
        this.researchProbeLoading = false;
        this.researchProbe = undefined;
        this.notification.warning('Local SearXNG unavailable', 'The configured endpoint did not pass its health probe. No source candidates were added.');
      },
    });
  }

  searchResearch(): void {
    const query = String(this.answerForm.value.question || '').trim();
    if (this.destroyed || !query || this.researchLoading) return;
    this.researchLoading = true;
    this.researchResults = [];
    this.observe(() => this.researchService.search(query), () => this.researchLoading = false, 30000).subscribe({
      next: (response) => {
        this.researchLoading = false;
        this.researchResults = response.results || [];
      },
      error: () => {
        this.researchLoading = false;
        this.notification.warning('Local research unavailable', 'Configure a reviewed local SearXNG instance to discover public source candidates. No evidence was added.');
        this.loadResearchStatus(true);
      },
    });
  }

  useResearchResult(result: IResearchResult): void {
    this.selectedResearchCandidate = result;
    this.selectedRAGFlowCandidate = undefined;
    this.selectedAnythingLLMCandidate = undefined;
    this.answerForm.patchValue({
      evidenceLabel: result.title || 'Local research candidate',
      evidenceUri: result.sourceUri,
      evidenceSnippet: result.snippet,
      official: false,
      primary: false,
    });
    this.notification.info('Candidate selected', 'The source is attached as unverified evidence. Claim verification remains required.');
  }

  useResearchCandidate(candidate: IResearchCandidateEvidence): void {
    this.useResearchResult({
      title: candidate.sourceLabel,
      sourceUri: candidate.sourceUri,
      snippet: candidate.snippet,
    });
  }

  loadRAGFlowStatus(force = false): void {
    if (this.destroyed || this.ragflowStatusLoading || (this.ragflowStatusLoaded && !force)) return;
    this.ragflowStatusLoading = true;
    this.observe(() => this.ragflowService.status(), () => this.ragflowStatusLoading = false).subscribe({
      next: (status) => {
        this.ragflowStatus = status;
        this.ragflowStatusLoaded = true;
        this.ragflowStatusLoading = false;
      },
      error: () => {
        this.ragflowStatus = undefined;
        this.ragflowStatusLoaded = false;
        this.ragflowStatusLoading = false;
      },
    });
  }

  probeRAGFlow(): void {
    if (this.destroyed || !this.ragflowStatus?.configured || this.ragflowProbeLoading) return;
    this.ragflowProbeLoading = true;
    this.ragflowProbe = undefined;
    this.observe(() => this.ragflowService.probe(), () => this.ragflowProbeLoading = false).subscribe({
      next: (probe) => {
        this.ragflowProbeLoading = false;
        if (probe?.reachable !== true) {
          this.ragflowProbe = undefined;
          this.notification.error('Local RAGFlow unavailable', 'The endpoint did not report a successful health check. No dataset was queried and no evidence was added.');
          return;
        }
        this.ragflowProbe = probe;
        this.notification.success('Local RAGFlow reachable', 'HAI checked only the configured health endpoint. No dataset was queried and no evidence was added.');
      },
      error: () => {
        this.ragflowProbeLoading = false;
        this.notification.error('Local RAGFlow unavailable', 'HAI could not verify the configured health endpoint. No dataset was queried and no evidence was added.');
      },
    });
  }

  searchRAGFlow(): void {
    const query = String(this.answerForm.value.question || '').trim();
    if (this.destroyed || !query || this.ragflowLoading) return;
    this.ragflowLoading = true;
    this.ragflowResults = [];
    this.observe(() => this.ragflowService.retrieve(query), () => this.ragflowLoading = false, 30000).subscribe({
      next: (response) => {
        this.ragflowLoading = false;
        this.ragflowResults = response.results || [];
      },
      error: () => {
        this.ragflowLoading = false;
        this.notification.warning('Local RAGFlow unavailable', 'Configure and approve a local RAGFlow dataset allowlist before retrieving candidate evidence. No evidence was added.');
        this.loadRAGFlowStatus(true);
      },
    });
  }

  useRAGFlowResult(result: IRAGFlowResult): void {
    this.selectedRAGFlowCandidate = result;
    this.selectedResearchCandidate = undefined;
    this.selectedAnythingLLMCandidate = undefined;
    this.answerForm.patchValue({
      evidenceLabel: result.documentName || `RAGFlow document ${result.documentId || 'candidate'}`,
      evidenceUri: this.ragflowEvidenceURI(result),
      evidenceSnippet: result.content,
      official: false,
      primary: false,
    });
    this.notification.info('Candidate selected', 'This local RAGFlow chunk is unverified candidate evidence. It cannot update memory or trigger actions by itself.');
  }

  private ragflowEvidenceURI(result: IRAGFlowResult): string {
    const segment = (value?: string) => encodeURIComponent(value || 'unknown');
    return `ragflow://dataset/${segment(result.datasetId)}/document/${segment(result.documentId)}/chunk/${segment(result.chunkId)}`;
  }

  loadAnythingLLMStatus(force = false): void {
    if (this.destroyed || this.anythingLLMStatusLoading || (this.anythingLLMStatusLoaded && !force)) return;
    this.anythingLLMStatusLoading = true;
    this.observe(() => this.anythingLLMService.status(), () => this.anythingLLMStatusLoading = false).subscribe({
      next: (status) => {
        this.anythingLLMStatus = status;
        this.anythingLLMStatusLoaded = true;
        this.anythingLLMStatusLoading = false;
      },
      error: () => {
        this.anythingLLMStatus = undefined;
        this.anythingLLMStatusLoaded = false;
        this.anythingLLMStatusLoading = false;
      },
    });
  }

  searchAnythingLLM(workspaceSlug: string): void {
    const query = String(this.answerForm.value.question || '').trim();
    if (this.destroyed || !query || !workspaceSlug || this.anythingLLMLoading) return;
    this.anythingLLMLoading = true;
    this.anythingLLMResults = [];
    this.observe(() => this.anythingLLMService.retrieve(query, workspaceSlug), () => this.anythingLLMLoading = false, 30000).subscribe({
      next: (response) => {
        this.anythingLLMLoading = false;
        this.anythingLLMResults = response.results || [];
      },
      error: () => {
        this.anythingLLMLoading = false;
        this.notification.warning('Local AnythingLLM unavailable', 'Configure an approved local workspace allowlist and confirm local embeddings before retrieving candidate evidence. No evidence was added.');
        this.loadAnythingLLMStatus(true);
      },
    });
  }

  useAnythingLLMResult(result: IAnythingLLMResult): void {
    this.selectedAnythingLLMCandidate = result;
    this.selectedRAGFlowCandidate = undefined;
    this.selectedResearchCandidate = undefined;
    this.answerForm.patchValue({
      evidenceLabel: result.title || `AnythingLLM workspace ${result.workspaceSlug}`,
      evidenceUri: this.anythingLLMEvidenceURI(result),
      evidenceSnippet: result.content,
      official: false,
      primary: false,
    });
    this.notification.info('Candidate selected', 'This local AnythingLLM chunk is unverified candidate evidence. It cannot update memory or trigger actions by itself.');
  }

  private anythingLLMEvidenceURI(result: IAnythingLLMResult): string {
    const segment = (value?: string) => encodeURIComponent(value || 'unknown');
    return `anythingllm://workspace/${segment(result.workspaceSlug)}/chunk/${segment(result.chunkId)}`;
  }

  loadDetails(run: IVerificationRun): void {
    if (this.destroyed || this.loading) return;
    this.detailSubscription?.unsubscribe();
    this.detailSubscription = this.observe(() => this.verificationService.runDetails(run.id), () => {}).subscribe({
      next: (details) => {
        details.run = run;
        this.result = details;
      },
      error: () => this.notification.error('Error', 'Failed to load verification run.'),
    });
  }

  goHome(): void {
    this.router.navigate(['/home']);
  }

  clearPursuitLink(): void {
    this.answerForm.patchValue({ pursuitId: '' });
  }

  openPursuit(id?: string): void {
    const pursuitId = id || this.answerForm.value.pursuitId;
    if (pursuitId) {
      this.router.navigate(['/pursuits'], { queryParams: { selected: pursuitId } });
    }
  }
}
