import { ChangeDetectionStrategy, Component, Inject, OnInit } from '@angular/core';
import { FormBuilder, FormGroup, Validators } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { NzNotificationService } from 'ng-zorro-antd/notification';
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

@Component({
  changeDetection: ChangeDetectionStrategy.Eager,
  standalone: false,
  selector: 'app-grounded-answers',
  templateUrl: './grounded-answers.component.html',
  styleUrls: ['./grounded-answers.component.scss'],
})
export class GroundedAnswersComponent implements OnInit {
  result?: IVerificationResult;
  runs: IVerificationRun[] = [];
  loading = false;
  researchLoading = false;
  researchProbeLoading = false;
  researchStatus?: IResearchStatus;
  researchProbe?: IResearchProbe;
  researchResults: IResearchResult[] = [];
  selectedResearchCandidate?: IResearchResult;
  ragflowLoading = false;
  ragflowProbeLoading = false;
  ragflowStatus?: IRAGFlowStatus;
  ragflowProbe?: IRAGFlowProbeResult;
  ragflowResults: IRAGFlowResult[] = [];
  selectedRAGFlowCandidate?: IRAGFlowResult;
  anythingLLMLoading = false;
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
    evidenceLabel: ['Manual evidence'],
    evidenceUri: ['local://manual-evidence'],
    evidenceSnippet: ['Connected-source records should be checked before task planning, and unsupported claims should be marked for review.'],
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
    private router: Router
  ) {}

  ngOnInit(): void {
    const params = this.route.snapshot.queryParamMap;
    this.answerForm.patchValue({
      pursuitId: params.get('pursuitId') || '',
      projectKey: params.get('projectKey') || this.answerForm.value.projectKey,
      question: params.get('question') || this.answerForm.value.question,
    });
    this.loadRuns();
    this.loadResearchStatus();
    this.loadRAGFlowStatus();
    this.loadAnythingLLMStatus();
  }

  answer(): void {
    if (this.answerForm.invalid) {
      return;
    }
    this.loading = true;
    const snippet = String(this.answerForm.value.evidenceSnippet || '').trim();
    this.verificationService
      .answer({
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
      })
      .subscribe({
        next: (result) => {
          this.result = result;
          this.loading = false;
          if (result.pursuitLinkError) {
            this.notification.warning('Verification saved, pursuit link failed', result.pursuitLinkError);
          } else if (result.pursuitLinked) {
            this.notification.success('Verification linked', 'The verification run is now visible in the selected pursuit evidence timeline.');
          }
          this.loadRuns();
        },
        error: () => {
          this.loading = false;
          this.notification.error('Error', 'Failed to create grounded answer.');
        },
      });
  }

  loadRuns(): void {
    this.verificationService.runs().subscribe({
      next: (runs) => (this.runs = runs),
      error: () => (this.runs = []),
    });
  }

  loadResearchStatus(): void {
    this.researchService.status().subscribe({
      next: (status) => (this.researchStatus = status),
      error: () => (this.researchStatus = undefined),
    });
  }

  probeResearch(): void {
    if (!this.researchStatus?.configured || this.researchProbeLoading) return;
    this.researchProbeLoading = true;
    this.researchService.probe().subscribe({
      next: (probe) => {
        this.researchProbeLoading = false;
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
    if (!query || this.researchLoading) return;
    this.researchLoading = true;
    this.researchResults = [];
    this.researchService.search(query).subscribe({
      next: (response) => {
        this.researchLoading = false;
        this.researchResults = response.results || [];
      },
      error: () => {
        this.researchLoading = false;
        this.notification.warning('Local research unavailable', 'Configure a reviewed local SearXNG instance to discover public source candidates. No evidence was added.');
        this.loadResearchStatus();
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

  loadRAGFlowStatus(): void {
    this.ragflowService.status().subscribe({
      next: (status) => (this.ragflowStatus = status),
      error: () => (this.ragflowStatus = undefined),
    });
  }

  probeRAGFlow(): void {
    if (!this.ragflowStatus?.configured || this.ragflowProbeLoading) return;
    this.ragflowProbeLoading = true;
    this.ragflowProbe = undefined;
    this.ragflowService.probe().subscribe({
      next: (probe) => {
        this.ragflowProbeLoading = false;
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
    if (!query || this.ragflowLoading) return;
    this.ragflowLoading = true;
    this.ragflowResults = [];
    this.ragflowService.retrieve(query).subscribe({
      next: (response) => {
        this.ragflowLoading = false;
        this.ragflowResults = response.results || [];
      },
      error: () => {
        this.ragflowLoading = false;
        this.notification.warning('Local RAGFlow unavailable', 'Configure and approve a local RAGFlow dataset allowlist before retrieving candidate evidence. No evidence was added.');
        this.loadRAGFlowStatus();
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

  loadAnythingLLMStatus(): void {
    this.anythingLLMService.status().subscribe({
      next: (status) => (this.anythingLLMStatus = status),
      error: () => (this.anythingLLMStatus = undefined),
    });
  }

  searchAnythingLLM(workspaceSlug: string): void {
    const query = String(this.answerForm.value.question || '').trim();
    if (!query || !workspaceSlug || this.anythingLLMLoading) return;
    this.anythingLLMLoading = true;
    this.anythingLLMResults = [];
    this.anythingLLMService.retrieve(query, workspaceSlug).subscribe({
      next: (response) => {
        this.anythingLLMLoading = false;
        this.anythingLLMResults = response.results || [];
      },
      error: () => {
        this.anythingLLMLoading = false;
        this.notification.warning('Local AnythingLLM unavailable', 'Configure an approved local workspace allowlist and confirm local embeddings before retrieving candidate evidence. No evidence was added.');
        this.loadAnythingLLMStatus();
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
    this.verificationService.runDetails(run.id).subscribe({
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
