import { ChangeDetectionStrategy, Component, Inject, OnDestroy, OnInit } from '@angular/core';
import { FormBuilder, FormGroup, Validators } from '@angular/forms';
import { Router } from '@angular/router';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { of, Subject } from 'rxjs';
import { catchError, switchMap, takeUntil, timeout } from 'rxjs/operators';
import {
  IContextMemory,
  IMemoryPageResult,
  IMemoryQueryParams,
  IMemoryRetrieveResult,
  ISemanticMemoryReindexResult,
} from '../../models/context-memory.model.interface';
import { IAIConversationImportResult } from '../../models/memory-engine.model.interface';
import { CONTEXT_MEMORY_SERVICE_TOKEN } from '../../services/context-memory/context-memory.service.token';
import { IContextMemoryService } from '../../services/context-memory.service.interface';
import { MEMORY_ENGINE_SERVICE_TOKEN } from '../../services/memory-engine/memory-engine.service.token';
import { IMemoryEngineService } from '../../services/memory-engine.service.interface';
import { ThemeMode, ThemeService } from '../../services/theme.service';

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
  standalone: false,
  selector: 'app-memory',
  templateUrl: './memory.component.html',
  styleUrls: ['./memory.component.scss'],
})
export class MemoryComponent implements OnInit, OnDestroy {
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
  memoryPage: IMemoryPageResult = {
    items: [],
    total: 0,
    page: 1,
    pageSize: 12,
    totalPages: 0,
    sort: 'updatedAt',
    order: 'desc',
  };
  themeMode: ThemeMode = 'light';
  private readonly loadTimeoutMs = 6000;
  private readonly operationTimeoutMs = 15000;
  private readonly queryRequests = new Subject<IMemoryQueryParams>();
  private readonly destroy$ = new Subject<void>();

  libraryForm: FormGroup = this.fb.group({
    projectKey: ['018-HAI'],
    q: [''],
    kind: [''],
    tag: [''],
    sort: ['updatedAt'],
    order: ['desc'],
    pageSize: [12],
  });

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
    limit: [8, [Validators.min(1), Validators.max(20)]],
  });

  importForm: FormGroup = this.fb.group({
    platform: ['chatgpt', [Validators.required]],
    externalId: [''],
    title: ['Imported AI thread', [Validators.required]],
    sourceUri: ['https://chatgpt.com/c/example', [Validators.required]],
    projectKey: ['018-HAI'],
    messagesText: ['user: What should HAI do next?\nassistant: Action: create a governed workflow and link it to the correct pursuit.', [Validators.required]],
  });

  constructor(
    private fb: FormBuilder,
    @Inject(CONTEXT_MEMORY_SERVICE_TOKEN)
    private memoryService: IContextMemoryService,
    @Inject(MEMORY_ENGINE_SERVICE_TOKEN)
    private memoryEngine: IMemoryEngineService,
    private notification: NzNotificationService,
    private router: Router,
    private themeService: ThemeService
  ) {}

  ngOnInit(): void {
    this.themeMode = this.themeService.mode();
    this.updateMemoryActions();
    this.queryRequests.pipe(
      switchMap((params) => this.memoryService.query(params).pipe(
        timeout(this.loadTimeoutMs),
        catchError(() => {
          this.notification.error('Memory unavailable', 'HAI could not load this memory page. Retry or adjust the filters.');
          return of(undefined);
        })
      )),
      takeUntil(this.destroy$)
    ).subscribe((result) => {
      this.loading = false;
      if (!result) {
        this.updateMemoryActions();
        return;
      }
      this.memoryPage = result;
      this.memories = result.items || [];
      if (!this.memories.some((memory) => memory.id === this.selectedMemoryId)) {
        this.selectedMemoryId = this.memories[0]?.id;
      }
      this.updateMemoryActions();
    });
    this.refresh();
  }

  ngOnDestroy(): void {
    this.destroy$.next();
    this.destroy$.complete();
    this.queryRequests.complete();
  }

  refresh(): void {
    this.loading = true;
    this.queryRequests.next(this.libraryQuery(this.memoryPage.page));
  }

  applyLibraryFilters(): void {
    this.loading = true;
    this.queryRequests.next(this.libraryQuery(1));
  }

  clearLibraryFilters(): void {
    this.libraryForm.patchValue({
      q: '',
      kind: '',
      tag: '',
      sort: 'updatedAt',
      order: 'desc',
    });
    this.applyLibraryFilters();
  }

  changeLibraryPage(page: number): void {
    this.loading = true;
    this.queryRequests.next(this.libraryQuery(page));
  }

  save(): void {
    if (this.memoryForm.invalid) {
      Object.values(this.memoryForm.controls).forEach((control) => {
        control.markAsDirty();
        control.updateValueAndValidity();
      });
      return;
    }

    this.saving = true;
    const request = this.formRequest();
    const save$ = this.editingId
      ? this.memoryService.update(this.editingId, request)
      : this.memoryService.create(request);

    save$.pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: () => {
        this.saving = false;
        this.libraryForm.patchValue({ projectKey: request.projectKey || '' });
        this.clearForm();
        this.refresh();
        this.notification.success('Memory saved', 'Context memory was stored locally.');
      },
      error: () => {
        this.saving = false;
        this.notification.error('Error', 'Failed to save memory.');
      },
    });
  }

  edit(memory: IContextMemory): void {
    this.editingId = memory.id;
    this.selectedMemoryId = memory.id;
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
      return;
    }
    this.retrieving = true;
    this.memoryService.retrieve(this.retrieveForm.value).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: (result) => {
        this.retrieveResult = result;
        if (result.usedContext.length) {
          this.selectedMemoryId = result.usedContext[0].memory.id;
        }
        this.updateMemoryActions();
        this.retrieving = false;
        this.refresh();
      },
      error: () => {
        this.retrieving = false;
        this.notification.error('Error', 'Failed to retrieve memory context.');
      },
    });
  }

  importConversation(): void {
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
    this.memoryEngine.importConversation({
      platform: this.importForm.value.platform,
      externalId: this.importForm.value.externalId,
      title: this.importForm.value.title,
      sourceUri: this.importForm.value.sourceUri,
      projectKey: this.importForm.value.projectKey,
      messages,
    }).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: (result) => {
        this.importResult = result;
        this.importing = false;
        this.updateMemoryActions();
        this.refresh();
        const pursuitCount = result.pursuitLinks?.length || 0;
        this.notification.success(
          'AI thread imported',
          `${result.insights.length} insight(s), ${result.workflowIds.length} workflow(s), ${pursuitCount} pursuit link(s).`
        );
      },
      error: (error) => {
        this.importing = false;
        this.notification.error(
          'Import blocked',
          error?.error?.error || 'HAI could not import this AI thread. Check encryption key and source URL.'
        );
      },
    });
  }

  archive(memory: IContextMemory): void {
    if (!memory.id) {
      return;
    }
    this.memoryService.archive(memory.id).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: () => this.refresh(),
      error: () => this.notification.error('Archive failed', 'The memory could not be archived.'),
    });
  }

  restore(memory: IContextMemory): void {
    if (!memory.id) {
      return;
    }
    this.memoryService.restore(memory.id).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: () => this.refresh(),
      error: () => this.notification.error('Restore failed', 'The memory could not be restored.'),
    });
  }

  delete(memory: IContextMemory): void {
    if (!memory.id || !window.confirm('Delete this memory permanently?')) {
      return;
    }
    this.memoryService.delete(memory.id).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: () => this.refresh(),
      error: () => this.notification.error('Delete failed', 'The memory could not be deleted.'),
    });
  }

  exportMemories(): void {
    this.memoryService.exportMemories(this.libraryForm.value.projectKey).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: (data) => {
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
      error: () => this.notification.error('Error', 'Failed to export memories.'),
    });
  }

  reindexSemantic(): void {
    this.reindexingSemantic = true;
    this.memoryService.reindexSemantic(100).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: (result) => {
        this.semanticReindexResult = result;
        this.reindexingSemantic = false;
        if (!result.enabled) {
          this.notification.info('Local semantic index unavailable', result.explanation);
          return;
        }
        const outcome = `${result.indexed} indexed, ${result.deferred} deferred, ${result.failed} failed.`;
        this.notification.success('Local semantic memory refreshed', outcome);
      },
      error: (error) => {
        this.reindexingSemantic = false;
        this.notification.error(
          'Local semantic index failed',
          error?.error?.error || 'No memory records were changed. Check the local embedding configuration and try again.'
        );
      },
    });
  }

  setAction(action: MemoryAction): void {
    this.selectedAction = action;
  }

  private updateMemoryActions(): void {
    this.memoryActions = [
      {
        id: 'store',
        title: this.editingId ? 'Correct memory' : 'Store memory',
        detail: 'Add verified context with source notes.',
        icon: 'plus-circle',
        metric: this.editingId ? 'editing' : `${this.memoryPage.total} matching`,
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
        metric: `${this.lowConfidenceCount()} low confidence`,
        tone: this.lowConfidenceCount() ? 'gold' : 'blue',
      },
      {
        id: 'corrections',
        title: 'Learned corrections',
        detail: 'Review what HAI learned from source fixes.',
        icon: 'safety-certificate',
        metric: `${this.sourceCorrectionCount()} learned`,
        tone: this.sourceCorrectionCount() ? 'green' : 'blue',
      },
      {
        id: 'cleanup',
        title: 'Cleanup',
        detail: 'Show archived and stale records.',
        icon: 'clear',
        metric: `${this.archivedCount()} on page`,
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

  selectedMemory(): IContextMemory | undefined {
    return (
      this.memories.find((memory) => memory.id === this.selectedMemoryId) ||
      this.memories[0]
    );
  }

  selectMemory(memory: IContextMemory): void {
    this.selectedMemoryId = memory.id;
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

  pageRangeStart(): number {
    if (!this.memoryPage.total || !this.memories.length) {
      return 0;
    }
    return (this.memoryPage.page - 1) * this.memoryPage.pageSize + 1;
  }

  pageRangeEnd(): number {
    if (!this.memoryPage.total || !this.memories.length) {
      return 0;
    }
    return this.pageRangeStart() + this.memories.length - 1;
  }

  sourceCorrectionMemories(): IContextMemory[] {
    return this.memories
      .filter((memory) => this.memoryTags(memory).includes('source-correction'))
      .slice(0, 12);
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
      .split(/\r?\n(?=(user|assistant|system)\s*:)/i)
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

  private libraryQuery(page: number): IMemoryQueryParams {
    const values = this.libraryForm.value;
    return {
      projectKey: String(values.projectKey || '').trim() || undefined,
      includeArchived: this.includeArchived,
      q: String(values.q || '').trim() || undefined,
      kind: String(values.kind || '').trim() || undefined,
      tag: String(values.tag || '').trim() || undefined,
      sort: values.sort || 'updatedAt',
      order: values.order || 'desc',
      page: Math.max(1, Math.trunc(Number(page) || 1)),
      pageSize: Math.min(100, Math.max(1, Math.trunc(Number(values.pageSize) || 12))),
    };
  }
}
