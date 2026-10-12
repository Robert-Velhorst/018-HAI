import { CommonModule } from '@angular/common';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { FormsModule, ReactiveFormsModule } from '@angular/forms';
import { NoopAnimationsModule } from '@angular/platform-browser/animations';
import { RouterTestingModule } from '@angular/router/testing';
import { NO_ERRORS_SCHEMA } from '@angular/core';
import { NzCheckboxModule } from 'ng-zorro-antd/checkbox';
import { of } from 'rxjs';
import { ControlRoomModule } from '../../control-room/control-room.module';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { LLM_POLICY_SERVICE_TOKEN } from '../../services/llm-policy/llm-policy.service.token';
import { ThemeService } from '../../services/theme.service';
import { LLMPolicyComponent } from './llm-policy.component';

describe('LLMPolicyComponent accessible tier selection', () => {
  let fixture: ComponentFixture<LLMPolicyComponent>;
  let preferences: ModuleViewPreferencesService;

  beforeEach(async () => {
    const service = jasmine.createSpyObj('LLMPolicyService', [
      'getPolicy',
      'getLogs',
      'getGenerationHistory',
      'getProbeHistory',
      'getModelMaintenanceHistory',
    ]);
    service.getPolicy.and.returnValue(of({
      dailyPaidBudgetEur: 0,
      paidCallsAllowed: false,
      localModelsAllowed: true,
      freeCloudQuotaAllowed: true,
      localFirst: true,
      cacheRepeatedPrompts: true,
      routeSimpleTasksToSmallModels: true,
      routeComplexTasksToBestAvailableFreeModel: true,
      requireApprovalBeforePaidUsage: true,
      requireRecentLiveProviderProbe: false,
      providerProbeMaxAgeSeconds: 300,
      tierOrder: ['local', 'free'],
      dailyBudgetUsedEur: 0,
      inputTokensUsed: 0,
      outputTokensUsed: 0,
      providers: [{
        id: 'local-test-provider',
        name: 'Local test provider',
        enabled: true,
        local: true,
        paid: false,
        configured: true,
        readinessStatus: 'ready',
        quotaRemaining: 100,
        dailyBudgetEur: 2,
        budgetUsedEur: 0.25,
        inputTokensUsed: 123,
        outputTokensUsed: 45,
        usageAccountingStatus: 'durable',
        models: [],
      }],
      inferenceInfrastructure: {
        kvCacheLoadStrategy: 'default',
        disaggregatedServingVerified: false,
        dualPathInfrastructureAvailable: false,
        reason: 'Not configured.',
      },
    }));
    service.getLogs.and.returnValue(of([]));
    service.getGenerationHistory.and.returnValue(of([]));
    service.getProbeHistory.and.returnValue(of([]));
    service.getModelMaintenanceHistory.and.returnValue(of([]));

    const themeService = jasmine.createSpyObj<ThemeService>('ThemeService', ['mode', 'toggle', 'label', 'icon']);
    themeService.mode.and.returnValue('dark');
    themeService.icon.and.returnValue('star');

    await TestBed.configureTestingModule({
      declarations: [LLMPolicyComponent],
      imports: [
        CommonModule,
        FormsModule,
        ReactiveFormsModule,
        RouterTestingModule,
        NoopAnimationsModule,
        ControlRoomModule,
        NzCheckboxModule,
      ],
      schemas: [NO_ERRORS_SCHEMA],
      providers: [
        { provide: LLM_POLICY_SERVICE_TOKEN, useValue: service },
        { provide: ThemeService, useValue: { ...themeService, changes$: of('dark') } },
      ],
    }).compileComponents();

    preferences = TestBed.inject(ModuleViewPreferencesService);
    preferences.reset('llm-policy');
    fixture = TestBed.createComponent(LLMPolicyComponent);
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture?.destroy();
    preferences?.reset('llm-policy');
  });

  it('uses labelled selection buttons instead of incomplete tabs and exposes the selected tier', () => {
    preferences.setMode('llm-policy', 'advanced');
    fixture.detectChanges();
    const page: HTMLElement = fixture.nativeElement;
    const disclosure = page.querySelector('hai-progressive-section[sectionid="model-catalog"] .hai-progressive-section__summary') as HTMLButtonElement;
    disclosure.click();
    fixture.detectChanges();

    const selector = page.querySelector('.tier-selector') as HTMLElement;
    expect(selector.getAttribute('role')).toBe('group');
    expect(selector.getAttribute('aria-label')).toBe('Select a model routing tier');
    expect(selector.querySelector('[role="tablist"]')).toBeNull();
    const buttons = Array.from(selector.querySelectorAll<HTMLButtonElement>('button'));
    expect(buttons.map((button) => button.getAttribute('aria-pressed'))).toEqual(['true', 'false']);

    buttons[1].click();
    fixture.detectChanges();
    expect(buttons.map((button) => button.getAttribute('aria-pressed'))).toEqual(['false', 'true']);
  });

  it('exposes provider budget details as a labelled, keyboard-focusable information group', () => {
    preferences.setMode('llm-policy', 'advanced');
    fixture.detectChanges();
    const page: HTMLElement = fixture.nativeElement;
    const disclosure = page.querySelector(
      'hai-progressive-section[sectionid="provider-inventory"] .hai-progressive-section__summary',
    ) as HTMLButtonElement;
    disclosure.click();
    fixture.detectChanges();

    const budget = page.querySelector('.provider-stats [role="group"]') as HTMLElement;
    expect(budget).not.toBeNull();
    expect(budget.tagName).toBe('DIV');
    expect(budget.getAttribute('tabindex')).toBe('0');
    expect(budget.getAttribute('aria-label')).toContain('Budget details for Local test provider');
    expect(budget.getAttribute('aria-label')).toContain('Input tokens: 123');
    expect(budget.getAttribute('aria-label')).toContain('Output tokens: 45');
    expect(budget.getAttribute('title')).toContain('Daily max: EUR 2');

    budget.focus();
    expect(document.activeElement).toBe(budget);
  });
});
