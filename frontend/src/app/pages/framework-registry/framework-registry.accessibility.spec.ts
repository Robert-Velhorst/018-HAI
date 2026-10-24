import { ComponentFixture, TestBed } from '@angular/core/testing';
import { NoopAnimationsModule } from '@angular/platform-browser/animations';
import { RouterTestingModule } from '@angular/router/testing';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { NZ_ICONS } from 'ng-zorro-antd/icon';
import { of, throwError } from 'rxjs';
import { HAI_ICONS } from '../../hai-icons';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { IAuthSession } from '../../models/auth-session.model.interface';
import { IConstitution, IFrameworkRegistryOverview } from '../../models/framework-registry.model.interface';
import { AuthSessionService } from '../../services/auth-session.service';
import { FrameworkRegistryService } from '../../services/framework-registry.service';
import { FrameworkRegistryComponent } from './framework-registry.component';
import { FrameworkRegistryModule } from './framework-registry.module';

describe('FrameworkRegistryComponent informational controls', () => {
  let fixture: ComponentFixture<FrameworkRegistryComponent>;
  let preferences: ModuleViewPreferencesService;
  let service: jasmine.SpyObj<FrameworkRegistryService>;

  beforeEach(async () => {
    service = jasmine.createSpyObj<FrameworkRegistryService>('FrameworkRegistryService', [
      'overview',
      'frameworks',
      'selections',
      'constitution',
      'constitutionHistory',
    ]);
    service.overview.and.returnValue(of({
      generatedAt: '2026-09-30T00:00:00Z',
      total: 0,
      enabled: 0,
      experimental: 0,
      deprecated: 0,
      pinned: 0,
      families: {},
      constitutionVersion: 1,
      constitutionSource: 'test',
      recentSelections: 0,
      selectionContract: [],
    } as IFrameworkRegistryOverview));
    service.frameworks.and.returnValue(of([]));
    service.selections.and.returnValue(of([]));
    service.constitution.and.returnValue(of({
      constitution: {
        id: 'constitution-test',
        version: 1,
        baseVersion: 0,
        status: 'active',
        values: [],
        prohibitions: [],
        standingPermissions: [],
        preferences: [],
        relationshipRules: [],
        financialBoundaries: [],
        communicationRules: [],
        escalationRules: [],
        protectedRules: [],
        createdAt: '2026-09-30T00:00:00Z',
      } as IConstitution,
      source: 'test',
    }));
    service.constitutionHistory.and.returnValue(of({ history: [], limit: 50, truncated: false }));

    const authSession = jasmine.createSpyObj<AuthSessionService>('AuthSessionService', ['session']);
    authSession.session.and.returnValue(of(undefined as unknown as IAuthSession));

    await TestBed.configureTestingModule({
      imports: [
        FrameworkRegistryModule,
        NoopAnimationsModule,
        RouterTestingModule,
      ],
      providers: [
        { provide: NZ_ICONS, useValue: HAI_ICONS },
        { provide: FrameworkRegistryService, useValue: service },
        { provide: AuthSessionService, useValue: authSession },
        { provide: NzNotificationService, useValue: { success: () => undefined, warning: () => undefined, error: () => undefined } },
        { provide: ModuleViewPreferencesService, useFactory: () => new ModuleViewPreferencesService(document) },
      ],
    }).compileComponents();

    preferences = TestBed.inject(ModuleViewPreferencesService);
    preferences.reset('framework-registry');
    fixture = TestBed.createComponent(FrameworkRegistryComponent);
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture?.destroy();
    preferences?.reset('framework-registry');
  });

  it('exposes the two help tooltips as labelled, keyboard-focusable notes rather than action buttons', () => {
    const controls = Array.from(
      (fixture.nativeElement as HTMLElement).querySelectorAll<HTMLElement>('.help-button'),
    );

    expect(controls.length).toBe(2);
    expect(controls.map((control) => control.tagName)).toEqual(['SPAN', 'SPAN']);
    expect(controls.map((control) => control.getAttribute('role'))).toEqual(['note', 'note']);
    expect(controls.map((control) => control.getAttribute('tabindex'))).toEqual(['0', '0']);
    expect(controls[0].getAttribute('aria-label')).toContain('active Constitution');
    expect(controls[1].getAttribute('aria-label')).toContain('reproducibility contract');

    controls.forEach((control) => {
      control.focus();
      expect(document.activeElement).toBe(control);
      expect(control.hasAttribute('nz-tooltip')).toBeTrue();
    });
  });

  function openSection(id: string): HTMLElement {
    preferences.setMode('framework-registry', 'advanced');
    preferences.setSection('framework-registry', id, true);
    fixture.detectChanges();
    return fixture.nativeElement.querySelector(`[data-hai-section="${id}"]`) as HTMLElement;
  }

  it('keeps retained selection rows visible with a stale warning when refresh fails', async () => {
    const selection = {
      id: 'fixture-selection', createdAt: '2026-09-30T00:00:00Z', lifeDomain: 'legal',
      selected: [{ name: 'Previously recorded framework', riskCeiling: 'high' }],
      maximumAutonomyLevel: 2, requiresApproval: true,
    } as any;
    fixture.componentInstance.selections = [selection];
    service.selections.and.returnValue(throwError(() => new Error('fixture offline')));
    fixture.componentInstance.refresh();
    const section = openSection('selection-history');
    await fixture.whenStable();
    fixture.detectChanges();

    expect(section.textContent).toContain('Selection history is unavailable.');
    expect(section.textContent).toContain('Previously recorded framework');
    expect(section.textContent).toContain('Previously loaded');
    expect(section.textContent).not.toContain('No recorded selections');
  });

  it('does not label a first failed history read as an empty recorded history', () => {
    service.selections.and.returnValue(throwError(() => new Error('fixture offline')));
    fixture.componentInstance.refresh();
    const section = openSection('selection-history');
    expect(section.textContent).toContain('Selection history unavailable');
    expect(section.textContent).not.toContain('No recorded selections');
  });

  it('retries failed history through its rendered action and clears the warning', async () => {
    service.selections.and.returnValue(throwError(() => new Error('fixture offline')));
    fixture.componentInstance.refresh();
    const section = openSection('selection-history');
    service.selections.and.returnValue(of([]));
    const retry = Array.from(section.querySelectorAll('button')).find(button => button.textContent?.trim() === 'Retry');
    expect(retry).toBeDefined();
    retry!.click();
    await fixture.whenStable();
    fixture.detectChanges();
    expect(section.textContent).not.toContain('Selection history unavailable');
    expect(section.textContent).toContain('No recorded selections');
  });

  it('does not mutate read-only capability hints through real checkbox hosts', async () => {
    const section = openSection('selection-context');
    await fixture.whenStable();
    const before = { ...fixture.componentInstance.selectionDraft };
    const checkboxes = Array.from(section.querySelectorAll<HTMLElement>('label[nz-checkbox]'));
    expect(checkboxes.length).toBe(6);
    checkboxes.forEach(checkbox => checkbox.click());
    await fixture.whenStable();
    fixture.detectChanges();
    expect(fixture.componentInstance.selectionDraft).toEqual(before);
    expect(checkboxes.every(checkbox => checkbox.classList.contains('ant-checkbox-wrapper-disabled'))).toBeTrue();
  });

  function authorizeOwner(): void {
    fixture.componentInstance.authSession = {
      authenticated: true, subject: 'fixture-owner', role: 'owner',
      permissions: { canRead: true, canOperate: true, canApprove: true, canAdminister: true },
    };
  }

  it('keeps capability hints frozen while an authorized selection is pending', async () => {
    authorizeOwner();
    fixture.componentInstance.selectionBusy = true;
    const section = openSection('selection-context');
    await fixture.whenStable();
    const before = { ...fixture.componentInstance.selectionDraft };
    const checkboxes = Array.from(section.querySelectorAll<HTMLElement>('label[nz-checkbox]'));
    checkboxes.forEach(checkbox => checkbox.click());
    await fixture.whenStable();
    expect(fixture.componentInstance.selectionDraft).toEqual(before);
    expect(checkboxes.every(checkbox => checkbox.classList.contains('ant-checkbox-wrapper-disabled'))).toBeTrue();
  });

  it('lets an authorized idle user edit capability hints through the same controls', async () => {
    authorizeOwner();
    const section = openSection('selection-context');
    await fixture.whenStable();
    const checkboxes = Array.from(section.querySelectorAll<HTMLElement>('label[nz-checkbox]'));
    checkboxes.forEach(checkbox => checkbox.click());
    await fixture.whenStable();
    const draft = fixture.componentInstance.selectionDraft;
    expect([draft.needsMemory, draft.needsTools, draft.needsDocuments, draft.needsWebAccess,
      draft.needsLocalExecution, draft.executeRequested]).toEqual([true, true, true, true, true, true]);
    expect(checkboxes.every(checkbox => !checkbox.classList.contains('ant-checkbox-wrapper-disabled'))).toBeTrue();
  });

  it('keeps amendment preparation read-only until owner controls are available', () => {
    const section = openSection('constitution-governance');
    const prepare = Array.from(section.querySelectorAll('button')).find(button => button.textContent?.trim() === 'Prepare amendment');
    expect(prepare).toBeDefined();
    expect(prepare!.disabled).toBeTrue();
    prepare!.click();
    expect(fixture.componentInstance.constitutionEditing).toBeFalse();
    authorizeOwner();
    fixture.detectChanges();
    expect(prepare!.disabled).toBeFalse();
    prepare!.click();
    fixture.detectChanges();
    expect(fixture.componentInstance.constitutionEditing).toBeTrue();
    expect(section.querySelector('.constitution-editor')).not.toBeNull();
    expect(section.querySelector('.activation-panel')).toBeNull();
  });
});
