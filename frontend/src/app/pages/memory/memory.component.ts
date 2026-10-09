import { ChangeDetectionStrategy, Component, Inject, OnDestroy, OnInit, QueryList, ViewChildren } from '@angular/core';
import { FormBuilder, FormGroup, Validators } from '@angular/forms';
import { Router } from '@angular/router';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { Observable, Subject, Subscription } from 'rxjs';
import { takeUntil, timeout } from 'rxjs/operators';
import {
  IContextMemory,
  IMemoryRetrieveResult,
  ISemanticMemoryReindexResult,
} from '../../models/context-memory.model.interface';
import { IAIConversationImportResult } from '../../models/memory-engine.model.interface';
import { CONTEXT_MEMORY_SERVICE_TOKEN } from '../../services/context-memory/context-memory.service.token';
import { IContextMemoryService, IMemoryQueryRequest, IMemoryQueryResult } from '../../services/context-memory.service.interface';
import { MEMORY_ENGINE_SERVICE_TOKEN } from '../../services/memory-engine/memory-engine.service.token';
import { IMemoryEngineService } from '../../services/memory-engine.service.interface';
import { ThemeMode, ThemeService } from '../../services/theme.service';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { HaiProgressiveSectionComponent } from '../../control-room/progressive-section.component';

type MemoryAction = 'store' | 'retrieve' | 'import' | 'corrections' | 'review' | 'export' | 'cleanup';

interface MemoryActionCard {
  id: MemoryAction;
  title: string;
  detail: string;
  icon: string;
  metric: string;
  tone: 'blue' | 'green' | 'gold' | 'red';
}

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-memory',
    templateUrl: './memory.component.html',
    styleUrls: ['./memory.component.scss'],
    standalone: false
})
export class MemoryComponent implements OnInit, OnDestroy {
  readonly moduleId = 'memory';
  @ViewChildren(HaiProgressiveSectionComponent) private disclosures?: QueryList<HaiProgressiveSectionComponent>;
  memories: IContextMemory[] = [];
  retrieveResult?: IMemoryRetrieveResult;
  loading = false;
  saving = false;
  retrieving = false;
  importing = false;
  reindexingSemantic = false;
  includeArchived = false;
  editingId?: string;
  selectedAction: MemoryAction = 'retrieve';
  memoryActions: MemoryActionCard[] = [];
  importResult?: IAIConversationImportResult;
  semanticReindexResult?: ISemanticMemoryReindexResult;
  selectedMemoryId?: string;
  themeMode: ThemeMode = 'light';
  memoriesLoaded = false;
  memoryLoadError = '';
  operationError = '';
  libraryPage = 1;
  libraryResult?: IMemoryQueryResult;
  readonly pendingMemoryIds = new Set<string>();
  exporting = false;
  private readonly destroyed$ = new Subject<void>();
  private retrieveSubscription?: Subscription;
  private readonly subscriptions = new Subscription();
  private readonly loadTimeoutMs = 6000;
  private readonly operationTimeoutMs = 15000;
  private refreshSubscription?: Subscription;
  private themeSubscription?: Subscription;

  memoryForm: FormGroup = this.fb.group({
    projectKey: ['018-HAI'],
    kind: ['project', [Validators.required]],
    content: ['', [Validators.required]],
    summary: [''],
    tags: [''],
    confidence: [0.7, [Validators.required, Validators.min(0.1), Validators.max(1)]],
    sourceUri: [''],
    sourceLabel: [''],
  });

  retrieveForm: FormGroup = this.fb.group({
    query: ['LLM routing project preferences', [Validators.required]],
    projectKey: ['018-HAI'],
    limit: [8, [Validators.required, Validators.min(1), Validators.max(20), Validators.pattern(/^\d+$/)]],
  });

  libraryForm = this.fb.nonNullable.group({
    projectKey: ['018-HAI'],
    q: [''],
    kind: [''],
    tag: [''],
    sort: this.fb.nonNullable.control<NonNullable<IMemoryQueryRequest['sort']>>('updatedAt'),
    order: this.fb.nonNullable.control<NonNullable<IMemoryQueryRequest['order']>>('desc'),
    pageSize: [20],
  });
  private appliedLibraryFilters: IMemoryQueryRequest = this.libraryForm.getRawValue();

  get libraryProjectKey(): string {
    return this.appliedLibraryFilters.projectKey || '';
  }

  importForm: FormGroup = this.fb.group({
    platform: ['chatgpt', [Validators.required]],
    externalId: [''],
    title: ['Imported AI thread', [Validators.required]],
    sourceUri: ['', [Validators.required]],
    projectKey: ['018-HAI'],
    messagesText: ['', [Validators.required]],
  });

  constructor(
    private fb: FormBuilder,
    @Inject(CONTEXT_MEMORY_SERVICE_TOKEN)
    private memoryService: IContextMemoryService,
    @Inject(MEMORY_ENGINE_SERVICE_TOKEN)
    private memoryEngine: IMemoryEngineService,
    private notification: NzNotificationService,
    private router: Router,
    private themeService: ThemeService,
    private viewPreferences: ModuleViewPreferencesService
  ) {}

  get isAdvancedView(): boolean {
    return this.viewPreferences.get(this.moduleId).mode === 'advanced';
  }

  get basicMemoryActions(): MemoryActionCard[] {
    return this.memoryActions.filter((action) => this.isBasicAction(action.id));
  }

  get advancedMemoryActions(): MemoryActionCard[] {
    return this.memoryActions.filter((action) => !this.isBasicAction(action.id));
  }

  ngOnInit(): void {
    this.themeMode = this.themeService.mode();
    this.themeSubscription = this.themeService.changes$?.subscribe((mode) => this.themeMode = mode);
    this.closeSelectedMemoryDisclosures();
    this.updateMemoryActions();
    this.subscriptions.add(this.retrieveForm.valueChanges.subscribe(() => this.clearRetrieval()));
    this.subscriptions.add(this.viewPreferences.watchMode(this.moduleId).subscribe((mode) => {
      if (mode === 'basic') {
        this.refreshSubscription?.unsubscribe();
        this.loading = false;
        if (!this.isBasicAction(this.selectedAction)) this.selectedAction = 'retrieve';
        this.updateMemoryActions();
      }
    }));
  }

  ngOnDestroy(): void {
    this.refreshSubscription?.unsubscribe();
    this.themeSubscription?.unsubscribe();
    this.retrieveSubscription?.unsubscribe();
    this.subscriptions.unsubscribe();
    this.destroyed$.next();
    this.destroyed$.complete();
  }

  refresh(): void {
    if (!this.isAdvancedView) return;
    this.loading = true;
    this.memoryLoadError = '';
    this.memoriesLoaded = false;
    this.updateMemoryActions();
    this.refreshSubscription?.unsubscribe();
    this.refreshSubscription = this.memoryService
      .query({ ...this.appliedLibraryFilters, includeArchived: this.includeArchived, page: this.libraryPage })
      .pipe(timeout(this.loadTimeoutMs))
      .subscribe({
        next: (result) => {
          if (result.page > Math.max(result.totalPages, 1)) {
            this.libraryPage = Math.max(result.totalPages, 1);
            this.refresh();
            return;
          }
          const memories = result.items;
          const previousSelection = this.selectedMemory()?.id;
          this.memories = memories;
          this.libraryResult = result;
          this.libraryPage = result.page;
          if (!this.selectedMemory() && memories.length) {
            this.selectedMemoryId = memories[0].id;
          }
          if (previousSelection && this.selectedMemory()?.id !== previousSelection) this.closeSelectedMemoryDisclosures();
          this.memoriesLoaded = true;
          this.updateMemoryActions();
          this.loading = false;
        },
        error: () => {
          this.loading = false;
          this.memoryLoadError = 'Memory records could not be loaded. Existing records are preserved.';
          this.updateMemoryActions();
          this.notification.error('Error', 'Failed to load memories.');
        },
      });
  }

  onMemoryRecordsOpen(open: boolean): void {
    if (open && !this.memoriesLoaded && !this.loading) this.refresh();
  }

  applyLibraryFilters(): void {
    this.appliedLibraryFilters = this.libraryForm.getRawValue();
    this.libraryPage = 1;
    this.memories = [];
    this.libraryResult = undefined;
    this.selectedMemoryId = undefined;
    this.clearRetrieval();
    this.closeSelectedMemoryDisclosures();
    this.refresh();
  }

  changeLibraryPage(page: number): void {
    if (!this.loading && page >= 1 && page <= (this.libraryResult?.totalPages || 1)) {
      this.libraryPage = page;
      this.refresh();
    }
  }

  private clearRetrieval(): void {
    this.retrieveSubscription?.unsubscribe();
    this.retrieving = false;
    if (this.retrieveResult?.usedContext.some((item) => item.memory.id === this.selectedMemoryId)) {
      this.selectedMemoryId = undefined;
      this.closeSelectedMemoryDisclosures();
    }
    this.retrieveResult = undefined;
    this.updateMemoryActions();
  }

  private closeSelectedMemoryDisclosures(): void {
    const recordSections = ['memory-record-content', 'memory-record-danger-zone'];
    recordSections.forEach((sectionId) => this.viewPreferences.setSection(this.moduleId, sectionId, false));
    this.disclosures?.forEach((disclosure) => {
      if (recordSections.includes(disclosure.sectionId)) disclosure.setOpen(false);
    });
  }

  private refreshIfMemoryRecordsOpen(): void {
    this.memoriesLoaded = false;
    this.updateMemoryActions();
    const openSections = this.viewPreferences.get(this.moduleId).openSections;
    if (this.isAdvancedView && (openSections['memory-records'] || openSections['memory-record-detail'] || openSections['memory-overview'] || ['review', 'corrections'].includes(this.selectedAction))) {
      this.refresh();
    }
  }

  isBasicAction(action: MemoryAction): boolean {
    return action === 'store' || action === 'retrieve' || action === 'import';
  }

  save(): void {
    if (this.saving) return;
    if (this.memoryForm.invalid) {
      Object.values(this.memoryForm.controls).forEach((control) => {
        control.markAsDirty();
        control.updateValueAndValidity();
      });
      return;
    }

    this.saving = true;
    this.operationError = '';
    const request = this.formRequest();
    if (this.isReviewManagedKind(request.kind)) {
      this.operationError = 'This memory type can only be written by its review workflow.';
      this.saving = false;
      return;
    }
    const save$ = this.editingId
      ? this.memoryService.update(this.editingId, request)
      : this.memoryService.create(request);
    this.memoryForm.disable({ emitEvent: false });

    save$.pipe(timeout(this.operationTimeoutMs), takeUntil(this.destroyed$)).subscribe({
      next: (saved) => {
        this.saving = false;
        this.memoryForm.enable({ emitEvent: false });
        this.replaceMemory(saved);
        this.clearForm();
        this.refreshIfMemoryRecordsOpen();
        this.notification.success('Memory saved', 'Context memory was stored locally.');
      },
      error: () => {
        this.saving = false;
        this.memoryForm.enable({ emitEvent: false });
        this.operationError = 'Saving could not be confirmed. Your draft is preserved; reload records before retrying.';
        this.notification.error('Error', 'Failed to save memory.');
      },
    });
  }

  edit(memory: IContextMemory): void {
    if (!memory.id || this.saving || this.pendingMemoryIds.has(memory.id)) return;
    if (this.isReviewManagedKind(memory.kind)) {
      this.operationError = 'This record must be corrected in its source or verification review workflow.';
      return;
    }
    this.editingId = memory.id;
    this.selectMemory(memory);
    this.selectedAction = 'store';
    this.memoryForm.patchValue({
      projectKey: memory.projectKey || '',
      kind: memory.kind,
      content: memory.content,
      summary: memory.summary || '',
      tags: memory.tags || '',
      confidence: memory.confidence || 0.7,
      sourceUri: memory.sourceUri || '',
      sourceLabel: memory.sourceLabel || '',
    });
    this.updateMemoryActions();
  }

  clearForm(): void {
    if (this.saving) return;
    this.editingId = undefined;
    this.memoryForm.reset({
      projectKey: '018-HAI',
      kind: 'project',
      content: '',
      summary: '',
      tags: '',
      confidence: 0.7,
      sourceUri: '',
      sourceLabel: '',
    });
    this.updateMemoryActions();
  }

  retrieve(): void {
    if (this.retrieveForm.invalid) {
      this.retrieveForm.markAllAsTouched();
      return;
    }
    if (!String(this.retrieveForm.value.query || '').trim()) return;
    this.clearRetrieval();
    this.retrieving = true;
    this.operationError = '';
    this.retrieveSubscription = this.memoryService.retrieve(this.retrieveForm.value).pipe(timeout(this.operationTimeoutMs), takeUntil(this.destroyed$)).subscribe({
      next: (result) => {
        this.retrieveResult = result;
        if (result.usedContext.length) {
          this.selectMemory(result.usedContext[0].memory, false);
        }
        this.updateMemoryActions();
        this.retrieving = false;
        this.refreshIfMemoryRecordsOpen();
      },
      error: () => {
        this.retrieving = false;
        this.operationError = 'Memory retrieval failed. No context was returned for this request.';
        this.notification.error('Error', 'Failed to retrieve memory context.');
      },
    });
  }

  importConversation(): void {
    if (this.importing) return;
    if (this.importForm.invalid) {
      this.importForm.markAllAsTouched();
      return;
    }
    const messages = this.parseMessages(String(this.importForm.value.messagesText || ''));
    if (!messages.length) {
      this.notification.warning('Import needs messages', 'Paste at least one user, assistant, or system message.');
      return;
    }
    this.importing = true;
    this.importResult = undefined;
    this.operationError = '';
    this.memoryEngine.importConversation({
      platform: this.importForm.value.platform,
      externalId: this.importForm.value.externalId,
      title: this.importForm.value.title,
      sourceUri: this.importForm.value.sourceUri,
      projectKey: this.importForm.value.projectKey,
      messages,
    }).pipe(timeout(this.operationTimeoutMs), takeUntil(this.destroyed$)).subscribe({
      next: (result) => {
        this.importResult = result;
        this.importing = false;
        this.updateMemoryActions();
        this.refreshIfMemoryRecordsOpen();
        const pursuitCount = result.pursuitLinks?.length || 0;
        this.notification.success(
          'AI thread imported',
          `${result.insights.length} insight(s), ${result.workflowIds.length} workflow(s), ${pursuitCount} pursuit link(s).`
        );
      },
      error: (error) => {
        this.importing = false;
        this.operationError = 'Import could not be confirmed. Check records before retrying; some import steps may have completed.';
        this.notification.error(
          'Import blocked',
          error?.error?.error || 'HAI could not import this AI thread. Check encryption key and source URL.'
        );
      },
    });
  }

  archive(memory: IContextMemory): void {
    if (!memory.id || memory.archived || this.recordBusy(memory)) return;
    this.mutateRecord(memory, this.memoryService.archive(memory.id), 'Archive');
  }

  restore(memory: IContextMemory): void {
    if (!memory.id || !memory.archived || this.recordBusy(memory)) return;
    this.mutateRecord(memory, this.memoryService.restore(memory.id), 'Restore');
  }

  delete(memory: IContextMemory): void {
    if (!memory.id || this.recordBusy(memory) || !window.confirm('Delete this memory permanently?')) {
      return;
    }
    this.mutateRecord(memory, this.memoryService.delete(memory.id), 'Delete');
  }

  recordBusy(memory: IContextMemory): boolean {
    return this.loading || !memory.id || this.pendingMemoryIds.has(memory.id) || (this.saving && this.editingId === memory.id);
  }

  private mutateRecord(memory: IContextMemory, request: Observable<IContextMemory | void>, action: string): void {
    const id = memory.id!;
    this.pendingMemoryIds.add(id);
    this.operationError = '';
    request.pipe(timeout(this.operationTimeoutMs), takeUntil(this.destroyed$)).subscribe({
      next: (saved) => {
        this.pendingMemoryIds.delete(id);
        this.retrieveSubscription?.unsubscribe();
        this.retrieving = false;
        if (saved) this.replaceMemory(saved);
        if (!saved || (saved.archived && !this.includeArchived)) {
          this.memories = this.memories.filter((item) => item.id !== id);
        }
        if (this.retrieveResult) {
          this.retrieveResult = { ...this.retrieveResult, usedContext: this.retrieveResult.usedContext.filter((item) => item.memory.id !== id) };
        }
        if (this.selectedMemoryId === id) {
          this.selectedMemoryId = undefined;
          this.closeSelectedMemoryDisclosures();
        }
        if (this.editingId === id) this.clearForm();
        this.refreshIfMemoryRecordsOpen();
        this.updateMemoryActions();
      },
      error: () => {
        this.pendingMemoryIds.delete(id);
        this.operationError = `${action} could not be confirmed. Reload records before retrying.`;
        this.notification.error(`${action} failed`, this.operationError);
      },
    });
  }

  exportMemories(): void {
    if (!this.isAdvancedView || this.exporting) return;
    this.exporting = true;
    this.operationError = '';
    this.memoryService.exportMemories(this.libraryProjectKey).pipe(timeout(this.operationTimeoutMs), takeUntil(this.destroyed$)).subscribe({
      next: (data) => {
        this.exporting = false;
        const blob = new Blob([JSON.stringify(data, null, 2)], {
          type: 'application/json',
        });
        const url = window.URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = '018-hai-context-memory.json';
        link.click();
        window.URL.revokeObjectURL(url);
      },
      error: () => {
        this.exporting = false;
        this.operationError = 'Memory export failed.';
        this.notification.error('Error', this.operationError);
      },
    });
  }

  reindexSemantic(): void {
    if (!this.isAdvancedView || this.reindexingSemantic) return;
    this.reindexingSemantic = true;
    this.operationError = '';
    this.semanticReindexResult = undefined;
    this.memoryService.reindexSemantic(100).pipe(timeout(this.operationTimeoutMs), takeUntil(this.destroyed$)).subscribe({
      next: (result) => {
        this.semanticReindexResult = result;
        this.reindexingSemantic = false;
        if (!result.enabled) {
          this.notification.info('Local semantic index unavailable', result.explanation);
          return;
        }
        const outcome = `${result.indexed} indexed, ${result.deferred} deferred, ${result.failed} failed.`;
        if (result.failed > 0) {
          this.notification.warning('Local semantic memory partially refreshed', outcome);
          return;
        }
        this.notification.success('Local semantic memory refreshed', outcome);
      },
      error: (error) => {
        this.reindexingSemantic = false;
        this.operationError = 'Local semantic retrieval could not be refreshed. Some index entries may have changed; check the index before retrying.';
        this.notification.error(
          'Local semantic index failed',
          error?.error?.error || 'Check the local embedding configuration and index before trying again.'
        );
      },
    });
  }

  setAction(action: MemoryAction): void {
    if (!this.isBasicAction(action) && !this.isAdvancedView) return;
    const wasCorrections = this.selectedAction === 'corrections';
    this.selectedAction = action;
    if (action === 'corrections') this.libraryForm.patchValue({ tag: 'source-correction' });
    else if (wasCorrections) this.libraryForm.patchValue({ tag: '' });
    if (action === 'review' || action === 'corrections') {
      this.applyLibraryFilters();
      this.viewPreferences.setSection(this.moduleId, 'memory-records', true);
    }
  }

  private updateMemoryActions(): void {
    this.memoryActions = [
      {
        id: 'store',
        title: this.editingId ? 'Correct memory' : 'Store memory',
        detail: 'Add manual context with source notes.',
        icon: 'plus-circle',
        metric: this.editingId ? 'editing' : this.memoriesLoaded ? `${this.libraryResult?.total ?? this.memories.length} matching` : 'inventory not loaded',
        tone: 'blue',
      },
      {
        id: 'retrieve',
        title: 'Retrieve context',
        detail: 'Find only relevant memories.',
        icon: 'search',
        metric: this.retrieveResult ? `${this.retrieveResult.usedContext.length} hits` : 'ready',
        tone: 'green',
      },
      {
        id: 'import',
        title: 'Import AI thread',
        detail: 'Extract actions and link pursuits.',
        icon: 'import',
        metric: this.importResult ? `${this.importResult.pursuitLinks?.length || 0} pursuit links` : 'encrypted',
        tone: 'gold',
      },
      {
        id: 'review',
        title: 'Review memories',
        detail: 'Browse, correct, archive, delete.',
        icon: 'unordered-list',
        metric: this.inventoryMetric(`${this.lowConfidenceCount()} low confidence on page`),
        tone: this.lowConfidenceCount() ? 'gold' : 'blue',
      },
      {
        id: 'corrections',
        title: 'Learned corrections',
        detail: 'Review what HAI learned from source fixes.',
        icon: 'safety-certificate',
        metric: this.inventoryMetric(`${this.sourceCorrectionCount()} learned on page`),
        tone: this.sourceCorrectionCount() ? 'green' : 'blue',
      },
      {
        id: 'cleanup',
        title: 'Cleanup',
        detail: 'Show archived and stale records.',
        icon: 'clear',
        metric: this.inventoryMetric(`${this.archivedCount()} archived on page`),
        tone: this.archivedCount() ? 'gold' : 'blue',
      },
      {
        id: 'export',
        title: 'Export',
        detail: 'Download project memory JSON.',
        icon: 'download',
        metric: 'JSON',
        tone: 'blue',
      },
    ];
  }

  private inventoryMetric(value: string): string {
    if (this.loading) return 'loading inventory';
    if (this.memoryLoadError) return 'inventory unavailable';
    return this.memoriesLoaded ? value : 'inventory not loaded';
  }

  selectedMemory(): IContextMemory | undefined {
    if (!this.selectedMemoryId) return this.memories[0];

    return this.memories.find((memory) => memory.id === this.selectedMemoryId)
      || this.retrieveResult?.usedContext.find((item) => item.memory.id === this.selectedMemoryId)?.memory;
  }

  selectedRetrievedMemory(): IContextMemory | undefined {
    return this.retrieveResult?.usedContext.find((item) => item.memory.id === this.selectedMemoryId)?.memory;
  }

  selectMemory(memory: IContextMemory, openInspector = true): void {
    if (this.selectedMemory()?.id !== memory.id) this.closeSelectedMemoryDisclosures();
    this.selectedMemoryId = memory.id;
    if (openInspector && this.isAdvancedView) this.viewPreferences.setSection(this.moduleId, 'memory-record-detail', true);
  }

  canEdit(memory: IContextMemory): boolean {
    return !!memory.id && !this.isReviewManagedKind(memory.kind);
  }

  private isReviewManagedKind(kind: string): boolean {
    const normalized = String(kind || '').trim().toLowerCase();
    return normalized === 'source' || normalized === 'source_supported_fact' || normalized.startsWith('correction_');
  }

  openReviewWorkflow(memory: IContextMemory): void {
    this.router.navigate([memory.kind.trim().toLowerCase() === 'source_supported_fact' ? '/grounded-answers' : '/connected-sources']);
  }

  private replaceMemory(saved: IContextMemory): void {
    this.memories = this.memories.map((item) => item.id === saved.id ? saved : item);
    if (this.retrieveResult) {
      this.retrieveResult = {
        ...this.retrieveResult,
        usedContext: this.retrieveResult.usedContext.map((item) => item.memory.id === saved.id ? { ...item, memory: saved } : item),
      };
    }
  }

  activeMemories(): IContextMemory[] {
    return this.memories.filter((memory) => !memory.archived);
  }

  archivedCount(): number {
    return this.memories.filter((memory) => memory.archived).length;
  }

  lowConfidenceCount(): number {
    return this.memories.filter((memory) => Number(memory.confidence || 0) < 0.65).length;
  }

  sourceLinkedCount(): number {
    return this.memories.filter((memory) => memory.sourceUri || memory.sourceLabel).length;
  }

  sourceCorrectionCount(): number {
    return this.sourceCorrectionMemories().length;
  }

  importPursuitLinkCount(): number {
    return this.importResult?.pursuitLinks?.length || 0;
  }

  importWorkflowCount(): number {
    return this.importResult?.workflowIds?.length || 0;
  }

  importWarningCount(): number {
    return this.importResult?.warnings?.length || 0;
  }

  averageConfidence(): string {
    if (!this.memories.length) {
      return '0.00';
    }
    const total = this.memories.reduce((sum, memory) => sum + Number(memory.confidence || 0), 0);
    return (total / this.memories.length).toFixed(2);
  }

  recentMemories(): IContextMemory[] {
    return this.memories;
  }

  sourceCorrectionMemories(): IContextMemory[] {
    return this.memories
      .filter((memory) => this.memoryTags(memory).includes('source-correction'));
  }

  memoryTags(memory?: IContextMemory): string[] {
    if (!memory?.tags) {
      return [];
    }
    return String(memory.tags)
      .split(',')
      .map((tag) => tag.trim())
      .filter(Boolean);
  }

  memoryTitle(memory: IContextMemory): string {
    return memory.summary || memory.content;
  }

  confidenceTone(memory?: IContextMemory): string {
    const confidence = Number(memory?.confidence || 0);
    if (confidence >= 0.8) {
      return 'good';
    }
    if (confidence >= 0.65) {
      return 'watch';
    }
    return 'bad';
  }

  kindTone(kind?: string): string {
    switch ((kind || '').toLowerCase()) {
      case 'preference':
        return 'green';
      case 'decision':
        return 'gold';
      case 'source':
        return 'blue';
      case 'lesson':
      case 'procedural':
        return 'green';
      default:
        return 'neutral';
    }
  }

  isSourceCorrection(memory?: IContextMemory): boolean {
    return this.memoryTags(memory).includes('source-correction');
  }

  updatedLabel(memory?: IContextMemory): string {
    if (!memory?.updatedAt && !memory?.createdAt) {
      return 'not dated';
    }
    return memory.updatedAt || memory.createdAt || '';
  }

  toggleTheme(): void {
    this.themeMode = this.themeService.toggle();
  }

  themeLabel(): string {
    return this.themeService.label();
  }

  themeIcon(): string {
    return this.themeService.icon();
  }

  goHome(): void {
    this.router.navigate(['/home']);
  }

  openPursuit(id?: string): void {
    if (!id) {
      return;
    }
    this.router.navigate(['/pursuits'], { queryParams: { selected: id } });
  }

  private parseMessages(text: string): Array<{ role: string; content: string }> {
    return text
      .split(/\r?\n(?=(?:user|assistant|system)\s*:)/i)
      .map((part) => part.trim())
      .filter(Boolean)
      .map((part) => {
        const match = part.match(/^(user|assistant|system)\s*:\s*([\s\S]*)$/i);
        if (!match) {
          return { role: 'user', content: part };
        }
        return { role: match[1].toLowerCase(), content: match[2].trim() };
      })
      .filter((message) => message.content);
  }

  private formRequest() {
    return {
      ...this.memoryForm.value,
      tags: String(this.memoryForm.value.tags || '')
        .split(',')
        .map((tag) => tag.trim())
        .filter(Boolean),
    };
  }
}
