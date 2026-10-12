import { FormBuilder } from '@angular/forms';
import { fakeAsync, TestBed, tick } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { By } from '@angular/platform-browser';
import { Router } from '@angular/router';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { provideNzIcons } from 'ng-zorro-antd/icon';
import { AppstoreOutline, ClearOutline, DatabaseOutline, DownloadOutline, EditOutline, ImportOutline, InboxOutline, PlusCircleOutline, ReloadOutline, SafetyCertificateOutline, SearchOutline, StarOutline, UndoOutline, UnorderedListOutline } from '@ant-design/icons-angular/icons';
import { of, Subject, throwError } from 'rxjs';
import { HaiProgressiveSectionComponent } from '../../control-room/progressive-section.component';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { IContextMemory } from '../../models/context-memory.model.interface';
import { IMemoryQueryResult } from '../../services/context-memory.service.interface';
import { ContextMemoryService } from '../../services/context-memory/context-memory.service';
import { CONTEXT_MEMORY_SERVICE_TOKEN } from '../../services/context-memory/context-memory.service.token';
import { MEMORY_ENGINE_SERVICE_TOKEN } from '../../services/memory-engine/memory-engine.service.token';
import { ThemeService } from '../../services/theme.service';
import { MemoryComponent } from './memory.component';
import { MemoryModule } from './memory.module';

afterEach(() => localStorage.removeItem('hai.module-view.v1.memory'));

const memoryIcons = [AppstoreOutline, ClearOutline, DatabaseOutline, DownloadOutline, EditOutline, ImportOutline, InboxOutline, PlusCircleOutline, ReloadOutline, SafetyCertificateOutline, SearchOutline, StarOutline, UndoOutline, UnorderedListOutline];

function page(items: IContextMemory[] = [], pageNumber = 1, total = items.length): IMemoryQueryResult {
  return { items, total, page: pageNumber, pageSize: 20, totalPages: Math.ceil(total / 20), sort: 'updatedAt', order: 'desc' };
}

function record(id = 'synthetic-memory', kind = 'project'): IContextMemory {
  return { id, kind, content: `Content of ${id}`, summary: id, confidence: 0.9, archived: false, projectKey: 'synthetic-project' };
}

describe('MemoryComponent', () => {
  function createComponent() {
    const memoryService = jasmine.createSpyObj('ContextMemoryService', ['query', 'list', 'retrieve', 'create', 'update', 'archive', 'restore', 'delete', 'reindexSemantic', 'exportMemories']);
    memoryService.query.and.returnValue(of(page()));
    const notification = jasmine.createSpyObj<NzNotificationService>('NzNotificationService', ['success', 'error', 'info', 'warning']);
    const router = jasmine.createSpyObj<Router>('Router', ['navigate']);
    const memoryEngine = jasmine.createSpyObj('MemoryEngine', ['importConversation']);
    const preferences = new ModuleViewPreferencesService();
    preferences.reset('memory');
    const component = new MemoryComponent(
      new FormBuilder(),
      memoryService,
      memoryEngine,
      notification,
      router,
      { mode: () => 'dark' } as any,
      preferences,
    );
    return { component, memoryService, preferences, memoryEngine, notification };
  }

  it('keeps Basic as the module default and does not list records until Advanced opens', () => {
    const { component, memoryService, preferences } = createComponent();

    component.ngOnInit();

    expect(component.isAdvancedView).toBeFalse();
    expect(memoryService.query).not.toHaveBeenCalled();
    expect(component.memoryActions.find((action) => action.id === 'store')?.metric).toBe('inventory not loaded');
    expect(preferences.get('connected-sources').mode).toBe('basic');
  });

  it('loads the memory library on demand and persists its module-specific section state', () => {
    const { component, memoryService, preferences } = createComponent();
    preferences.setMode('memory', 'advanced');
    component.onMemoryRecordsOpen(true);

    expect(memoryService.query).toHaveBeenCalledTimes(1);
    expect(component.memoriesLoaded).toBeTrue();
    expect(component.memoryActions.find((action) => action.id === 'store')?.metric).toBe('0 matching');
    expect(preferences.get('memory').mode).toBe('advanced');

    preferences.setMode('memory', 'advanced');
    preferences.setSection('memory', 'memory-records', true);

    expect(component.isAdvancedView).toBeTrue();
    expect(preferences.get('connected-sources').openSections['memory-records']).toBeUndefined();
  });

  it('shows unavailable inventory instead of false zero counts after a failed load', () => {
    const { component, memoryService, preferences } = createComponent();
    component.ngOnInit();
    preferences.setMode('memory', 'advanced');
    memoryService.query.and.returnValue(throwError(() => new Error('offline')));

    component.onMemoryRecordsOpen(true);

    expect(component.memoryLoadError).toContain('could not be loaded');
    expect(component.memoryActions.find((action) => action.id === 'review')?.metric).toBe('inventory unavailable');
    expect(component.memoryActions.find((action) => action.id === 'corrections')?.metric).toBe('inventory unavailable');
    expect(component.memoryActions.find((action) => action.id === 'cleanup')?.metric).toBe('inventory unavailable');
  });

  it('closes record content and danger disclosures on startup and when another memory is selected', () => {
    const { component, preferences } = createComponent();
    component.memories = [
      { id: 'memory-a', content: 'first record' } as IContextMemory,
      { id: 'memory-b', content: 'second record' } as IContextMemory,
    ];
    component.selectedMemoryId = 'memory-a';
    preferences.setSection('memory', 'memory-record-content', true);
    preferences.setSection('memory', 'memory-record-danger-zone', true);

    component.ngOnInit();
    expect(preferences.get('memory').openSections['memory-record-content']).toBeFalse();
    expect(preferences.get('memory').openSections['memory-record-danger-zone']).toBeFalse();

    preferences.setSection('memory', 'memory-record-content', true);
    preferences.setSection('memory', 'memory-record-danger-zone', true);
    component.selectMemory(component.memories[1]);

    expect(component.selectedMemoryId).toBe('memory-b');
    expect(preferences.get('memory').openSections['memory-record-content']).toBeFalse();
    expect(preferences.get('memory').openSections['memory-record-danger-zone']).toBeFalse();
    expect(preferences.get('connected-sources').openSections).toEqual({});
  });

  it('keeps the local semantic-index disclosure closed until the Advanced action opens it', () => {
    const { component, memoryService, preferences } = createComponent();
    component.ngOnInit();

    expect(preferences.get('memory').openSections['memory-semantic-index']).toBeUndefined();
    expect(memoryService.query).not.toHaveBeenCalled();

    preferences.setSection('memory', 'memory-semantic-index', true);
    expect(preferences.get('memory').openSections['memory-semantic-index']).toBeTrue();
    expect(preferences.get('connected-sources').openSections['memory-semantic-index']).toBeUndefined();
  });

  it('renders memory selection as a focusable native button and retains its selection action', async () => {
    const preferences = new ModuleViewPreferencesService();
    preferences.reset('memory');
    preferences.setMode('memory', 'advanced');
    const records = [
      { id: 'memory-first', summary: 'First lesson', content: 'first content', kind: 'project', projectKey: '018-HAI', confidence: 0.9 } as IContextMemory,
      { id: 'memory-second', summary: 'Second lesson', content: 'second content', kind: 'project', projectKey: '018-HAI', confidence: 0.8 } as IContextMemory,
    ];
    const memoryService = jasmine.createSpyObj('ContextMemoryService', ['query']);
    memoryService.query.and.returnValue(of(page(records)));

    await TestBed.configureTestingModule({
      imports: [MemoryModule],
      providers: [
        provideNzIcons(memoryIcons),
        { provide: CONTEXT_MEMORY_SERVICE_TOKEN, useValue: memoryService },
        { provide: MEMORY_ENGINE_SERVICE_TOKEN, useValue: {} },
        { provide: ModuleViewPreferencesService, useValue: preferences },
        { provide: ThemeService, useValue: { mode: () => 'dark', changes$: of('dark'), label: () => 'Dark mode', icon: () => 'star' } },
        { provide: Router, useValue: jasmine.createSpyObj<Router>('Router', ['navigate']) },
        { provide: NzNotificationService, useValue: jasmine.createSpyObj('NzNotificationService', ['success', 'error', 'warning']) },
      ],
    }).compileComponents();

    const fixture = TestBed.createComponent(MemoryComponent);
    fixture.detectChanges();
    expect(memoryService.query).not.toHaveBeenCalled();

    const library = fixture.debugElement.queryAll(By.directive(HaiProgressiveSectionComponent))
      .map((item) => item.componentInstance as HaiProgressiveSectionComponent)
      .find((item) => item.sectionId === 'memory-records');
    expect(library).toBeTruthy();
    library?.setOpen(true);
    fixture.detectChanges();

    const button = fixture.nativeElement.querySelector('.memory-row') as HTMLButtonElement;
    expect(memoryService.query).toHaveBeenCalledTimes(1);
    expect(button).toBeTruthy();
    expect(button.tagName).toBe('BUTTON');
    expect(button.type).toBe('button');
    expect(button.tabIndex).toBe(0);
    expect(button.getAttribute('aria-pressed')).toBe('true');
    expect(button.getAttribute('aria-label')).toContain('First lesson');
    button.focus();
    expect(document.activeElement).toBe(button);

    const nextRecord = fixture.nativeElement.querySelectorAll('.memory-row')[1] as HTMLButtonElement;
    nextRecord.click();
    expect(fixture.componentInstance.selectedMemoryId).toBe('memory-second');
    expect(preferences.get('memory').openSections['memory-record-content']).toBeFalse();
    expect(preferences.get('memory').openSections['memory-record-danger-zone']).toBeFalse();
    fixture.destroy();
  });

  it('cancels an obsolete refresh so stale memories cannot replace the newest view', () => {
    const { component, memoryService, preferences } = createComponent();
    preferences.setMode('memory', 'advanced');
    const first = new Subject<IMemoryQueryResult>();
    const second = new Subject<IMemoryQueryResult>();
    memoryService.query.and.returnValues(first.asObservable(), second.asObservable());

    component.refresh();
    component.refresh();

    second.next(page([{ id: 'new-memory', content: 'latest' } as IContextMemory]));
    second.complete();
    first.next(page([{ id: 'old-memory', content: 'stale' } as IContextMemory]));
    first.complete();

    expect(component.memories.map((memory) => memory.id)).toEqual(['new-memory']);
  });

  it('shows the retrieved memory instead of an unrelated first library record', () => {
    const { component } = createComponent();
    const unrelated = { id: 'library-memory', summary: 'Different project memory', content: 'unrelated' } as IContextMemory;
    const retrieved = { id: 'retrieved-memory', summary: 'Retrieved source memory', content: 'relevant' } as IContextMemory;
    component.memories = [unrelated];
    component.retrieveResult = {
      query: 'relevant context',
      usedContext: [{ memory: retrieved, score: 0.9, explanation: 'Matched the query' }],
      explanation: 'One relevant memory found.',
    };

    component.selectMemory(retrieved);

    expect(component.selectedMemory()?.id).toBe('retrieved-memory');
    expect(component.memoryTitle(component.selectedMemory()!)).toBe('Retrieved source memory');

    component.selectedMemoryId = 'missing-memory';
    expect(component.selectedMemory()).toBeUndefined();
  });

  it('does not truncate a server page to twelve records', () => {
    const { component, memoryService, preferences } = createComponent();
    preferences.setMode('memory', 'advanced');
    memoryService.query.and.returnValue(of(page(Array.from({ length: 20 }, (_, i) => record(`memory-${i}`)), 1, 43)));
    component.refresh();
    expect(component.recentMemories().length).toBe(20);
    expect(component.libraryResult?.total).toBe(43);
    component.changeLibraryPage(2);
    expect(memoryService.query.calls.mostRecent().args[0].page).toBe(2);
  });

  it('resets page and selection when the project or filters change', () => {
    const { component, memoryService, preferences } = createComponent();
    preferences.setMode('memory', 'advanced');
    component.memories = [record('old-project-memory')];
    component.selectedMemoryId = 'old-project-memory';
    component.libraryPage = 3;
    component.libraryForm.patchValue({ projectKey: 'new-project', q: 'needle', kind: 'lesson', tag: 'reviewed', sort: 'confidence', order: 'asc' });
    component.applyLibraryFilters();
    expect(memoryService.query).toHaveBeenCalledWith(jasmine.objectContaining({ projectKey: 'new-project', q: 'needle', kind: 'lesson', tag: 'reviewed', page: 1, sort: 'confidence', order: 'asc' }));
    expect(component.selectedMemory()).toBeUndefined();
  });

  it('recovers the last valid page after the final record on a page is removed', () => {
    const { component, memoryService, preferences } = createComponent();
    preferences.setMode('memory', 'advanced');
    component.libraryPage = 3;
    memoryService.query.and.returnValues(of(page([], 3, 21)), of(page([record()], 2, 21)));
    component.refresh();
    expect(component.libraryPage).toBe(2);
    expect(memoryService.query.calls.mostRecent().args[0].page).toBe(2);
    expect(component.loading).toBeFalse();
  });

  it('pages through applied filters rather than a newer unsubmitted search draft', () => {
    const { component, memoryService, preferences } = createComponent();
    preferences.setMode('memory', 'advanced');
    memoryService.query.and.returnValue(of(page([record()], 1, 40)));
    component.libraryForm.patchValue({ q: 'applied', projectKey: 'applied-project' });
    component.applyLibraryFilters();
    component.libraryForm.patchValue({ q: 'unsubmitted', projectKey: 'unsubmitted-project' });
    component.changeLibraryPage(2);
    expect(memoryService.query.calls.mostRecent().args[0]).toEqual(jasmine.objectContaining({ q: 'applied', projectKey: 'applied-project', page: 2 }));
    expect(component.libraryProjectKey).toBe('applied-project');
  });

  it('queries all matching corrections instead of filtering only the first page', () => {
    const { component, memoryService, preferences } = createComponent();
    preferences.setMode('memory', 'advanced');
    component.setAction('corrections');
    expect(memoryService.query.calls.mostRecent().args[0].tag).toBe('source-correction');
    expect(preferences.get('memory').openSections['memory-records']).toBeTrue();
  });

  it('keeps maintenance actions and record loads out of Basic, including direct callbacks', () => {
    const { component, memoryService } = createComponent();
    component.ngOnInit();
    component.setAction('review');
    component.onMemoryRecordsOpen(true);
    component.refresh();
    component.reindexSemantic();
    component.exportMemories();
    expect(component.selectedAction).toBe('retrieve');
    expect(memoryService.query).not.toHaveBeenCalled();
    expect(memoryService.reindexSemantic).not.toHaveBeenCalled();
    expect(memoryService.exportMemories).not.toHaveBeenCalled();
    component.ngOnDestroy();
  });

  it('cancels a library load when Advanced closes and retries on the next open', () => {
    const { component, memoryService, preferences } = createComponent();
    const pending = new Subject<IMemoryQueryResult>();
    memoryService.query.and.returnValue(pending);
    component.ngOnInit();
    preferences.setMode('memory', 'advanced');
    component.onMemoryRecordsOpen(true);
    preferences.setMode('memory', 'basic');
    pending.next(page([record('obsolete')]));
    expect(component.memories).toEqual([]);
    expect(component.loading).toBeFalse();
    preferences.setMode('memory', 'advanced');
    memoryService.query.and.returnValue(of(page([record('fresh')])));
    component.onMemoryRecordsOpen(true);
    expect(component.memories[0].id).toBe('fresh');
    component.ngOnDestroy();
  });

  it('cancels outdated retrieval and removes old context when the query changes or fails', () => {
    const { component, memoryService } = createComponent();
    component.ngOnInit();
    const pending = new Subject<any>();
    memoryService.retrieve.and.returnValue(pending);
    component.retrieve();
    component.retrieveForm.patchValue({ query: 'new request' });
    pending.next({ query: 'old request', usedContext: [{ memory: record('obsolete') }] });
    expect(component.retrieveResult).toBeUndefined();
    expect(component.retrieving).toBeFalse();
    memoryService.retrieve.and.returnValue(throwError(() => new Error('offline')));
    component.retrieve();
    expect(component.operationError).toContain('retrieval failed');
    expect(component.retrieveResult).toBeUndefined();
    component.ngOnDestroy();
  });

  it('rejects empty queries and missing or fractional retrieval limits', () => {
    const { component, memoryService } = createComponent();
    component.retrieveForm.patchValue({ query: '   ' });
    component.retrieve();
    component.retrieveForm.patchValue({ query: 'synthetic', limit: null });
    component.retrieve();
    component.retrieveForm.patchValue({ limit: 1.5 });
    component.retrieve();
    expect(memoryService.retrieve).not.toHaveBeenCalled();
  });

  it('never permits generic editing or creation of review-managed kinds', () => {
    const { component, memoryService } = createComponent();
    for (const kind of ['source', 'source_supported_fact', 'correction_lesson', ' CORRECTION_fact ']) {
      const managed = record('managed', kind);
      component.edit(managed);
      expect(component.editingId).toBeUndefined();
      expect(component.canEdit(managed)).toBeFalse();
      component.memoryForm.patchValue({ kind, content: 'high confidence but unverified', confidence: 1 });
      component.save();
    }
    expect(memoryService.create).not.toHaveBeenCalled();
    expect(memoryService.update).not.toHaveBeenCalled();
    expect(component.saving).toBeFalse();
    expect(component.canEdit(record('manual', 'project'))).toBeTrue();
  });

  it('keeps the correction draft on failure and blocks overlapping saves', () => {
    const { component, memoryService } = createComponent();
    const pending = new Subject<IContextMemory>();
    memoryService.update.and.returnValue(pending);
    component.edit(record('manual'));
    component.memoryForm.patchValue({ content: 'corrected content', tags: ' one, two ' });
    component.save();
    component.save();
    component.edit(record('another'));
    component.clearForm();
    expect(memoryService.update).toHaveBeenCalledTimes(1);
    expect(memoryService.update).toHaveBeenCalledWith('manual', jasmine.objectContaining({ content: 'corrected content', tags: ['one', 'two'] }));
    pending.error(new Error('partial failure'));
    expect(component.saving).toBeFalse();
    expect(component.editingId).toBe('manual');
    expect(component.memoryForm.value.content).toBe('corrected content');
    expect(component.operationError).toContain('could not be confirmed');
    expect(component.operationError).not.toContain('not changed');
    component.ngOnDestroy();
  });

  it('invalidates a closed inventory after save so reopening does not show stale data', () => {
    const { component, memoryService, preferences } = createComponent();
    component.memoriesLoaded = true;
    component.memoryForm.patchValue({ content: 'synthetic new record' });
    memoryService.create.and.returnValue(of(record('saved')));
    component.save();
    expect(memoryService.query).not.toHaveBeenCalled();
    expect(component.memoriesLoaded).toBeFalse();
    preferences.setMode('memory', 'advanced');
    component.onMemoryRecordsOpen(true);
    expect(memoryService.query).toHaveBeenCalledTimes(1);
  });

  it('blocks overlapping archive/restore/delete and removes archived retrieval snapshots only after success', () => {
    const { component, memoryService } = createComponent();
    const memory = record();
    const pending = new Subject<IContextMemory>();
    memoryService.archive.and.returnValue(pending);
    component.retrieveResult = { query: 'synthetic', explanation: 'fixture', usedContext: [{ memory, score: 1, explanation: 'fixture' }] };
    component.selectMemory(memory);
    component.archive(memory);
    component.archive(memory);
    component.delete(memory);
    expect(memoryService.archive).toHaveBeenCalledTimes(1);
    expect(memoryService.delete).not.toHaveBeenCalled();
    expect(component.retrieveResult.usedContext.length).toBe(1);
    pending.next({ ...memory, archived: true });
    pending.complete();
    expect(component.retrieveResult.usedContext).toEqual([]);
    expect(component.selectedMemory()).toBeUndefined();
    expect(component.pendingMemoryIds.size).toBe(0);
    component.ngOnDestroy();
  });

  it('preserves a record on archive failure and clears the operation error on a successful retry', () => {
    const { component, memoryService } = createComponent();
    const memory = record();
    component.memories = [memory];
    memoryService.archive.and.returnValue(throwError(() => new Error('offline')));
    component.archive(memory);
    expect(component.memories).toEqual([memory]);
    expect(component.recordBusy(memory)).toBeFalse();
    expect(component.operationError).toContain('could not be confirmed');
    memoryService.archive.and.returnValue(of({ ...memory, archived: true }));
    component.archive(memory);
    expect(component.operationError).toBe('');
    expect(component.memories).toEqual([]);
  });

  it('requires per-record confirmation for permanent deletion', () => {
    const { component, memoryService } = createComponent();
    spyOn(window, 'confirm').and.returnValue(false);
    component.delete(record());
    expect(memoryService.delete).not.toHaveBeenCalled();
    (window.confirm as jasmine.Spy).and.returnValue(true);
    memoryService.delete.and.returnValue(of(undefined));
    component.delete(record());
    expect(memoryService.delete).toHaveBeenCalledWith('synthetic-memory');
  });

  it('restores an archived record only on confirmation from the API and prevents duplicate restore', () => {
    const { component, memoryService } = createComponent();
    const memory = { ...record(), archived: true };
    const pending = new Subject<IContextMemory>();
    component.memories = [memory];
    component.includeArchived = true;
    memoryService.restore.and.returnValue(pending);
    component.restore(memory);
    component.restore(memory);
    expect(memoryService.restore).toHaveBeenCalledTimes(1);
    expect(component.memories[0].archived).toBeTrue();
    pending.next({ ...memory, archived: false });
    pending.complete();
    expect(component.memories[0].archived).toBeFalse();
    expect(component.pendingMemoryIds.size).toBe(0);
    component.ngOnDestroy();
  });

  it('preserves import text on failure, blocks duplicate import and never claims a partial failure changed nothing', () => {
    const { component, memoryEngine, notification } = createComponent();
    const pending = new Subject();
    memoryEngine.importConversation.and.returnValue(pending);
    component.importForm.patchValue({ sourceUri: 'https://synthetic.invalid/thread', messagesText: 'user: Need context\nassistant: Decision: Keep source evidence.' });
    component.importConversation();
    component.importConversation();
    expect(memoryEngine.importConversation).toHaveBeenCalledTimes(1);
    expect(memoryEngine.importConversation.calls.mostRecent().args[0].messages).toEqual([
      { role: 'user', content: 'Need context' },
      { role: 'assistant', content: 'Decision: Keep source evidence.' },
    ]);
    pending.error(new Error('partial import'));
    expect(component.importing).toBeFalse();
    expect(component.importResult).toBeUndefined();
    expect(component.importForm.value.messagesText).toContain('Keep source evidence');
    expect(component.operationError).toContain('some import steps may have completed');
    expect(notification.success).not.toHaveBeenCalled();
    component.ngOnDestroy();
  });

  it('distinguishes a partial semantic reindex from a fully successful refresh', () => {
    const { component, memoryService, preferences, notification } = createComponent();
    preferences.setMode('memory', 'advanced');
    memoryService.reindexSemantic.and.returnValue(of({ enabled: true, indexed: 1, attempted: 2, failed: 1, deferred: 0, explanation: 'Synthetic partial result' }));
    component.reindexSemantic();
    expect(component.reindexingSemantic).toBeFalse();
    expect(notification.warning).toHaveBeenCalled();
    expect(notification.success).not.toHaveBeenCalled();
  });

  it('times out operations and cancels callbacks when the component is destroyed', fakeAsync(() => {
    const { component, memoryService } = createComponent();
    component.memoryForm.patchValue({ content: 'draft' });
    memoryService.create.and.returnValue(new Subject());
    component.save();
    tick(15001);
    expect(component.saving).toBeFalse();
    expect(component.memoryForm.value.content).toBe('draft');
    const pending = new Subject<IContextMemory>();
    memoryService.create.and.returnValue(pending);
    component.save();
    component.ngOnDestroy();
    pending.next(record());
    tick(15001);
    expect(component.memoryForm.value.content).toBe('draft');
    expect(memoryService.query).not.toHaveBeenCalled();
  }));
});

describe('MemoryComponent rendered HTTP action chains (synthetic fixtures only)', () => {
  async function setup(advanced = false) {
    const preferences = new ModuleViewPreferencesService();
    preferences.reset('memory');
    if (advanced) preferences.setMode('memory', 'advanced');
    await TestBed.configureTestingModule({
      imports: [MemoryModule],
      providers: [
        provideNzIcons(memoryIcons),
        provideHttpClient(), provideHttpClientTesting(),
        { provide: CONTEXT_MEMORY_SERVICE_TOKEN, useClass: ContextMemoryService },
        { provide: MEMORY_ENGINE_SERVICE_TOKEN, useValue: {} },
        { provide: ModuleViewPreferencesService, useValue: preferences },
        { provide: ThemeService, useValue: { mode: () => 'dark', changes$: of('dark'), label: () => 'Dark mode', icon: () => 'star' } },
        { provide: Router, useValue: jasmine.createSpyObj('Router', ['navigate']) },
        { provide: NzNotificationService, useValue: jasmine.createSpyObj('Notification', ['success', 'error', 'warning']) },
      ],
    }).compileComponents();
    const fixture = TestBed.createComponent(MemoryComponent);
    fixture.detectChanges();
    const http = TestBed.inject(HttpTestingController);
    return { fixture, http, preferences };
  }

  it('renders Basic retrieval, provenance, correction and archive without loading the library', async () => {
    const { fixture, http } = await setup();
    // An earlier Advanced visit may have left unrelated records in memory.
    fixture.componentInstance.memories = [record('unrelated-library-record')];
    http.expectNone((request) => request.url.includes('/memory/query'));
    fixture.componentInstance.retrieve();
    const memory = { ...record(), sourceUri: 'https://synthetic.invalid/source', sourceLabel: 'Synthetic evidence' };
    http.expectOne('/api/v1/memory/retrieve').flush({ query: 'synthetic', explanation: 'Fixture result', usedContext: [{ memory, score: 0.9, explanation: 'Fixture match' }] });
    fixture.detectChanges();
    const detail = fixture.nativeElement.querySelector('.memory-basic-detail') as HTMLElement;
    expect(detail.textContent).toContain(memory.content);
    expect(detail.textContent).toContain(memory.sourceUri);
    expect(detail.textContent).toContain(memory.sourceLabel);
    (Array.from(detail.querySelectorAll('button')).find((button) => button.textContent?.includes('Archive'))!).click();
    const archive = http.expectOne(`/api/v1/memory/${memory.id}/archive`);
    expect(archive.request.method).toBe('POST');
    archive.flush({ ...memory, archived: true });
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('.memory-basic-detail')).toBeNull();
    http.verify();
    fixture.destroy();
  });

  it('renders server search/filter/page results and never caps them at twelve', async () => {
    const { fixture, http, preferences } = await setup(true);
    http.expectNone((request) => request.url.includes('/memory/query'));
    preferences.setSection('memory', 'memory-records', true);
    http.expectOne((request) => request.url.endsWith('/memory/query') && request.params.get('page') === '1').flush(page(Array.from({ length: 20 }, (_, i) => record(`memory-${i}`)), 1, 41));
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelectorAll('.memory-row').length).toBe(20);
    const search = fixture.nativeElement.querySelector('.memory-library-filters input[formControlName="q"]') as HTMLInputElement;
    search.value = 'needle';
    search.dispatchEvent(new Event('input'));
    fixture.nativeElement.querySelector('.memory-library-filters').dispatchEvent(new Event('submit'));
    const filtered = http.expectOne((request) => request.url.endsWith('/memory/query') && request.params.get('q') === 'needle');
    expect(filtered.request.params.get('page')).toBe('1');
    filtered.flush(page([record('filtered')], 1, 21));
    fixture.detectChanges();
    (fixture.nativeElement.querySelector('.ant-pagination-next') as HTMLElement).click();
    const next = http.expectOne((request) => request.url.endsWith('/memory/query') && request.params.get('page') === '2');
    next.flush(page([record('last-page')], 2, 21));
    fixture.detectChanges();
    const button = fixture.nativeElement.querySelector('.memory-row') as HTMLButtonElement;
    expect(button.textContent).toContain('last-page');
    button.click();
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('.inspector').textContent).toContain('last-page');
    expect(preferences.get('memory').openSections['memory-record-content']).toBeFalse();
    http.verify();
    fixture.destroy();
  });

  it('renders unavailable inventory, not a misleading empty-library success, and supports retry', async () => {
    const { fixture, http, preferences } = await setup(true);
    preferences.setSection('memory', 'memory-records', true);
    http.expectOne((request) => request.url.endsWith('/memory/query')).flush({ error: 'unavailable' }, { status: 503, statusText: 'Unavailable' });
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('.memory-message--error').textContent).toContain('could not be loaded');
    expect(fixture.nativeElement.querySelector('.memory-library nz-empty')).toBeNull();
    (fixture.nativeElement.querySelector('.memory-message--error button') as HTMLButtonElement).click();
    http.expectOne((request) => request.url.endsWith('/memory/query')).flush(page());
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('.memory-library nz-empty').textContent).toContain('No matching memory records');
    http.verify();
    fixture.destroy();
  });

  it('shows review navigation, not an accept or edit shortcut, for a source-supported fact', async () => {
    const { fixture, http } = await setup();
    const memory = record('verified-fixture', 'source_supported_fact');
    fixture.componentInstance.retrieve();
    http.expectOne('/api/v1/memory/retrieve').flush({ query: 'fixture', usedContext: [{ memory, score: 0.9, explanation: 'fixture' }], explanation: 'fixture' });
    fixture.detectChanges();
    const detail = fixture.nativeElement.querySelector('.memory-basic-detail') as HTMLElement;
    expect(detail.textContent).toContain('Open review workflow');
    expect(detail.textContent).not.toContain('Correct memory');
    expect(detail.textContent).not.toContain('Accept');
    (Array.from(detail.querySelectorAll('button')).find((button) => button.textContent?.includes('Open review'))!).click();
    expect(TestBed.inject(Router).navigate).toHaveBeenCalledWith(['/grounded-answers']);
    http.verify();
    fixture.destroy();
  });

  it('completes a rendered manual correction through the actual PATCH service without an inventory fetch in Basic', async () => {
    const { fixture, http } = await setup();
    const memory = record('manual-correction');
    fixture.componentInstance.retrieve();
    http.expectOne('/api/v1/memory/retrieve').flush({ query: 'fixture', usedContext: [{ memory, score: 0.9, explanation: 'fixture' }], explanation: 'fixture' });
    fixture.detectChanges();
    const correct = Array.from(fixture.nativeElement.querySelectorAll('.memory-basic-detail button') as NodeListOf<HTMLButtonElement>).find((button) => button.textContent?.includes('Correct'))!;
    correct.click();
    fixture.detectChanges();
    const content = fixture.nativeElement.querySelector('textarea[formControlName="content"]') as HTMLTextAreaElement;
    expect(content.value).toBe(memory.content);
    content.value = 'Corrected synthetic content';
    content.dispatchEvent(new Event('input'));
    const save = Array.from(fixture.nativeElement.querySelectorAll('.form-footer button') as NodeListOf<HTMLButtonElement>).find((button) => button.textContent?.includes('Save memory'))!;
    save.click();
    fixture.detectChanges();
    expect(content.disabled).toBeTrue();
    const update = http.expectOne('/api/v1/memory/manual-correction');
    expect(update.request.method).toBe('PATCH');
    expect(update.request.body.content).toBe('Corrected synthetic content');
    update.flush({ ...memory, content: 'Corrected synthetic content' });
    fixture.detectChanges();
    expect(content.value).toBe('');
    expect(content.disabled).toBeFalse();
    fixture.componentInstance.setAction('retrieve');
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('.memory-basic-detail').textContent).toContain('Corrected synthetic content');
    http.expectNone((request) => request.url.endsWith('/memory/query'));
    http.verify();
    fixture.destroy();
  });
});
