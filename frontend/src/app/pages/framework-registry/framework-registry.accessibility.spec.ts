import { CommonModule } from '@angular/common';
import { NO_ERRORS_SCHEMA } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { FormsModule } from '@angular/forms';
import { NoopAnimationsModule } from '@angular/platform-browser/animations';
import { RouterTestingModule } from '@angular/router/testing';
import { NzCheckboxModule } from 'ng-zorro-antd/checkbox';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzInputNumberModule } from 'ng-zorro-antd/input-number';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { of } from 'rxjs';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { IAuthSession } from '../../models/auth-session.model.interface';
import { IConstitution, IFrameworkRegistryOverview } from '../../models/framework-registry.model.interface';
import { AuthSessionService } from '../../services/auth-session.service';
import { FrameworkRegistryService } from '../../services/framework-registry.service';
import { FrameworkRegistryComponent } from './framework-registry.component';

describe('FrameworkRegistryComponent informational controls', () => {
  let fixture: ComponentFixture<FrameworkRegistryComponent>;
  let preferences: ModuleViewPreferencesService;

  beforeEach(async () => {
    const service = jasmine.createSpyObj<FrameworkRegistryService>('FrameworkRegistryService', [
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
      declarations: [FrameworkRegistryComponent],
      imports: [
        CommonModule,
        FormsModule,
        NoopAnimationsModule,
        RouterTestingModule,
        NzCheckboxModule,
        NzInputModule,
        NzInputNumberModule,
        NzSelectModule,
        NzTooltipModule,
      ],
      providers: [
        { provide: FrameworkRegistryService, useValue: service },
        { provide: AuthSessionService, useValue: authSession },
        { provide: NzNotificationService, useValue: { success: () => undefined, warning: () => undefined, error: () => undefined } },
        { provide: ModuleViewPreferencesService, useFactory: () => new ModuleViewPreferencesService(document) },
      ],
      schemas: [NO_ERRORS_SCHEMA],
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
});
