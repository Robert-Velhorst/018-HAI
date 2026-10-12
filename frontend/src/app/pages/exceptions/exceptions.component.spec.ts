import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzEmptyModule } from 'ng-zorro-antd/empty';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { of, throwError } from 'rxjs';
import { ControlRoomModule } from '../../control-room/control-room.module';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { IWorkflowItem } from '../../models/workflow.model.interface';
import { WorkflowService } from '../../services/workflow/workflow.service';
import { ExceptionsComponent } from './exceptions.component';

const preferenceKey = 'hai.module-view.v1.exceptions';

function detectFixture(fixture: ComponentFixture<ExceptionsComponent>): void {
  fixture.changeDetectorRef.markForCheck();
  fixture.changeDetectorRef.detectChanges();
}

function makeItem(overrides: Partial<IWorkflowItem> = {}): IWorkflowItem {
  return {
    id: 'workflow-1',
    title: 'Example workflow',
    currentState: 'blocked',
    taskType: 'manual_review',
    riskLevel: 'low',
    priorityScore: 0,
    confidence: 0,
    autonomyLevel: 'manual',
    requiresApproval: false,
    approvalStatus: 'not_required',
    retryCount: 0,
    maxRetries: 2,
    archived: false,
    createdAt: '2026-09-24T10:00:00.000Z',
    updatedAt: '2026-09-24T10:30:00.000Z',
    ...overrides,
  };
}

describe('ExceptionsComponent', () => {
  let fixture: ComponentFixture<ExceptionsComponent>;
  let component: ExceptionsComponent;
  let workflow: jasmine.SpyObj<WorkflowService>;
  let router: jasmine.SpyObj<Router>;
  let preferences: ModuleViewPreferencesService;

  beforeEach(async () => {
    localStorage.removeItem(preferenceKey);
    workflow = jasmine.createSpyObj<WorkflowService>('WorkflowService', ['items']);
    workflow.items.and.returnValue(of([]));
    router = jasmine.createSpyObj<Router>('Router', ['navigate']);

    await TestBed.configureTestingModule({
      declarations: [ExceptionsComponent],
      imports: [
        CommonModule,
        FormsModule,
        ControlRoomModule,
        NzButtonModule,
        NzEmptyModule,
        NzSpinModule,
        NzTagModule,
      ],
      providers: [
        { provide: WorkflowService, useValue: workflow },
        { provide: Router, useValue: router },
      ],
    }).compileComponents();

    preferences = TestBed.inject(ModuleViewPreferencesService);
    fixture = TestBed.createComponent(ExceptionsComponent);
    component = fixture.componentInstance;
    detectFixture(fixture);
  });

  afterEach(() => {
    fixture?.destroy();
    localStorage.removeItem(preferenceKey);
    window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}`);
    document.body.classList.remove('hai-view-advanced');
  });

  it('loads only non-archived workflow items and records successful queue freshness', () => {
    const refreshed = makeItem();
    workflow.items.and.returnValue(of([refreshed]));

    component.refresh();
    detectFixture(fixture);

    expect(workflow.items).toHaveBeenCalledWith(false);
    expect(component.items).toEqual([refreshed]);
    expect(component.exceptions.map((item) => item.id)).toEqual(['workflow-1']);
    expect(component.loading).toBeFalse();
    expect(component.lastRefreshedAt).toBeTruthy();
  });

  it('keeps approval-required and high-risk open items in Basic and excludes completed items', () => {
    component.items = [
      makeItem({ id: 'ordinary', currentState: 'blocked', updatedAt: '2026-09-24T12:00:00Z' }),
      makeItem({ id: 'approval', title: 'Needs decision', currentState: 'ready', requiresApproval: true, approvalStatus: 'pending' }),
      makeItem({ id: 'risk', title: 'High risk', currentState: 'classified', riskLevel: 'high' }),
      makeItem({ id: 'done-risk', currentState: 'completed', riskLevel: 'high' }),
    ];

    expect(component.isAdvancedView).toBeFalse();
    expect(component.exceptions.map((item) => item.id)).toEqual(['approval', 'risk', 'ordinary']);
    expect(component.displayedExceptions.map((item) => item.id)).toContain('approval');
    expect(component.displayedExceptions.map((item) => item.id)).toContain('risk');
    expect(component.displayedExceptions.map((item) => item.id)).not.toContain('done-risk');
  });

  it('recognizes backend approval states and recovery-review state', () => {
    expect(ExceptionsComponent.needsAttention('NEEDS_APPROVAL')).toBeTrue();
    expect(ExceptionsComponent.needsAttention('AWAITING_APPROVAL')).toBeTrue();
    expect(ExceptionsComponent.needsAttention('failed')).toBeTrue();
    expect(ExceptionsComponent.needsAttention('done')).toBeFalse();
    expect(ExceptionsComponent.needsAttention('')).toBeFalse();

    component.items = [makeItem({
      id: 'review',
      currentState: 'ready',
      recoveryStatus: 'needs_review',
    })];
    expect(component.exceptions.map((item) => item.id)).toEqual(['review']);
  });

  it('sorts approval items before high-risk and other recovery items', () => {
    component.items = [
      makeItem({ id: 'newest-blocked', currentState: 'blocked', updatedAt: '2026-09-24T13:00:00Z' }),
      makeItem({ id: 'high', currentState: 'ready', riskLevel: 'high' }),
      makeItem({ id: 'approval', currentState: 'ready', requiresApproval: true, approvalStatus: 'pending' }),
    ];

    expect(component.exceptions.map((item) => item.id)).toEqual(['approval', 'high', 'newest-blocked']);
  });

  it('applies search, state, and risk filters only in Advanced', () => {
    component.items = [
      makeItem({ id: 'approval', title: 'Approval task', currentState: 'ready', requiresApproval: true, approvalStatus: 'pending' }),
      makeItem({ id: 'high', title: 'High-risk task', currentState: 'classified', riskLevel: 'high' }),
      makeItem({ id: 'blocked', title: 'Retry the source', currentState: 'blocked', blockedReason: 'Provider is unavailable' }),
    ];
    component.searchQuery = 'no match';
    component.stateFilter = 'failed';
    component.riskFilter = 'low';

    expect(component.displayedExceptions.length).toBe(3);

    preferences.setMode('exceptions', 'advanced');
    detectFixture(fixture);
    expect(component.isAdvancedView).toBeTrue();
    expect(component.displayedExceptions).toEqual([]);

    component.clearFilters();
    component.searchQuery = 'retry';
    detectFixture(fixture);
    expect(component.displayedExceptions.map((item) => item.id)).toEqual(['blocked']);
  });

  it('remembers Advanced disclosure state under the Exceptions module only', () => {
    component.items = [makeItem()];
    preferences.setMode('exceptions', 'advanced');
    detectFixture(fixture);

    const trigger = fixture.nativeElement.querySelector('.hai-progressive-section__summary') as HTMLButtonElement;
    expect(trigger).not.toBeNull();
    trigger.click();
    detectFixture(fixture);

    const filters = fixture.nativeElement.querySelector('[data-testid="exceptions-advanced-filters"]');
    expect(filters).not.toBeNull();
    expect(preferences.get('exceptions').openSections['filters']).toBeTrue();
    expect(preferences.get('workflow-engine').openSections).toEqual({});
  });

  it('opens Queue filters and enables Advanced when directly linked by its supported fragment', () => {
    fixture.destroy();
    window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}#filters`);
    fixture = TestBed.createComponent(ExceptionsComponent);
    component = fixture.componentInstance;
    detectFixture(fixture);

    expect(component.isAdvancedView).toBeTrue();
    expect(preferences.get('exceptions').openSections['filters']).toBeTrue();
    expect(fixture.nativeElement.querySelector('[data-testid="exceptions-advanced-filters"]')).not.toBeNull();
  });

  it('does not infer an owner and uses recorded recovery context', () => {
    const item = makeItem({
      blockedReason: 'Waiting for the source provider to recover.',
      recoveryNote: 'A later retry is permitted.',
    });

    expect(component.recoveryReason(item)).toBe('Waiting for the source provider to recover.');
    expect(component.recoveryReason(makeItem({ blockedReason: '', recoveryNote: '', approvalReason: '', lastWorkerError: '' })))
      .toBe('No recovery reason is recorded.');

    component.items = [item];
    detectFixture(fixture);
    expect(fixture.nativeElement.querySelector('.exceptions__details')?.textContent).toContain('Owner is not recorded in this workflow item.');
  });

  it('labels unknown state and risk values without presenting them as verified severity', () => {
    expect(component.stateLabel('needs_approval')).toBe('Needs approval');
    expect(component.stateLabel('provider_wait')).toBe('Provider Wait');
    expect(component.stateLabel('')).toBe('State not recorded');
    expect(component.riskLabel('high')).toBe('High');
    expect(component.riskLabel('')).toBe('Not recorded');
    expect(component.tagColor('failed')).toBe('red');
    expect(component.tagColor('blocked')).toBe('orange');
    expect(component.tagColor('needs_approval')).toBe('gold');
  });

  it('keeps the last successful queue visible and marks refresh failure', () => {
    const lastGood = makeItem({ id: 'last-good' });
    workflow.items.and.returnValue(of([lastGood]));
    component.refresh();
    detectFixture(fixture);
    const freshness = component.lastRefreshedAt;

    workflow.items.and.returnValue(throwError(() => new Error('offline')));
    component.refresh();
    detectFixture(fixture);

    expect(component.items).toEqual([lastGood]);
    expect(component.lastRefreshedAt).toBe(freshness);
    expect(component.loadError).toContain('could not be refreshed');
    expect(fixture.nativeElement.textContent).toContain('last successful results');
  });

  it('clears Advanced filters through the visible control', () => {
    component.items = [makeItem()];
    preferences.setMode('exceptions', 'advanced');
    component.searchQuery = 'example';
    component.stateFilter = 'blocked';
    detectFixture(fixture);

    const trigger = fixture.nativeElement.querySelector('.hai-progressive-section__summary') as HTMLButtonElement;
    trigger.click();
    detectFixture(fixture);
    const clear = fixture.nativeElement.querySelector('[data-testid="exceptions-clear-filters"]') as HTMLButtonElement;
    expect(clear.disabled).toBeFalse();
    clear.click();
    detectFixture(fixture);

    expect(component.hasActiveFilters).toBeFalse();
    expect(component.displayedExceptions.map((item) => item.id)).toEqual(['workflow-1']);
  });

  it('navigates to the existing workflow detail and Command Center routes', () => {
    const item = makeItem({ id: 'workflow-target', title: 'Inspect this item' });
    component.items = [item];
    detectFixture(fixture);

    const openButton = fixture.nativeElement.querySelector('[data-testid="open-workflow-workflow-target"]') as HTMLButtonElement;
    expect(openButton).not.toBeNull();
    openButton.click();
    expect(router.navigate).toHaveBeenCalledWith(['/workflow-engine'], {
      queryParams: { workflowId: 'workflow-target' },
    });

    (fixture.nativeElement.querySelector('[data-testid="exceptions-command-center"]') as HTMLButtonElement).click();
    expect(router.navigate).toHaveBeenCalledWith(['/control-center']);
  });

  it('refresh button calls the existing workflow list API', () => {
    fixture.nativeElement.querySelector('[data-testid="exceptions-refresh"]').click();
    expect(workflow.items).toHaveBeenCalledWith(false);
  });
});
