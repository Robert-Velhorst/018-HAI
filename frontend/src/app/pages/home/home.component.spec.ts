import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ReactiveFormsModule } from '@angular/forms';
import { RouterTestingModule } from '@angular/router/testing';
import { NO_ERRORS_SCHEMA } from '@angular/core';
import { CommonModule } from '@angular/common';
import { NoopAnimationsModule } from '@angular/platform-browser/animations';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzModalService } from 'ng-zorro-antd/modal';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzLayoutModule } from 'ng-zorro-antd/layout';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { of, throwError } from 'rxjs';
import { ControlRoomModule } from '../../control-room/control-room.module';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';

import { HomeComponent } from './home.component';
import { AUTOMATIONS_SERVICE_TOKEN } from '../../services/automations/automations.service.token';
import { AUTH_SERVICE_TOKEN } from '../../services/auth/auth.service.token';
import { USER_SERVICE_TOKEN } from '../../services/user/user.service.token';

describe('HomeComponent', () => {
  let component: HomeComponent;
  let fixture: ComponentFixture<HomeComponent>;

  beforeEach(() => {
    TestBed.configureTestingModule({
      declarations: [HomeComponent],
      imports: [
        CommonModule,
        ReactiveFormsModule,
        RouterTestingModule,
        ControlRoomModule,
        NoopAnimationsModule,
        NzButtonModule,
        NzFormModule,
        NzIconModule,
        NzInputModule,
        NzModalModule,
        NzCardModule,
        NzLayoutModule,
      ],
      schemas: [NO_ERRORS_SCHEMA],
      providers: [
        { provide: NzNotificationService, useValue: { error: jasmine.createSpy('error'), success: jasmine.createSpy('success'), create: jasmine.createSpy('create') } },
        { provide: NzModalService, useValue: {} },
        { provide: AUTOMATIONS_SERVICE_TOKEN, useValue: {
          getAutomations: jasmine.createSpy('getAutomations').and.returnValue(of([])),
          swapAutomations: jasmine.createSpy('swapAutomations').and.returnValue(of(undefined)),
          addAutomation: jasmine.createSpy('addAutomation').and.returnValue(of({})),
          updateAutomation: jasmine.createSpy('updateAutomation').and.returnValue(of({})),
        } },
        { provide: AUTH_SERVICE_TOKEN, useValue: {} },
        { provide: USER_SERVICE_TOKEN, useValue: {} },
      ],
    });
    fixture = TestBed.createComponent(HomeComponent);
    component = fixture.componentInstance;
    TestBed.inject(ModuleViewPreferencesService).reset('automations');
    // Shallow creation smoke test: no detectChanges so ngOnInit's data loading
    // (which needs a full service mock) is not exercised here.
  });

  it('should create', () => {
    expect(component).toBeTruthy();
  });

  it('leaves navigation to AppShell while preserving page and account actions', () => {
    fixture.detectChanges();
    const page: HTMLElement = fixture.nativeElement;
    expect(page.querySelector('nz-drawer, nav, [aria-label="Open navigation"]')).toBeNull();

    const add = spyOn(component, 'openAutomationModal');
    const profile = spyOn(component, 'showProfileModal');
    const logout = spyOn(component, 'logout');
    (page.querySelector('[data-testid="home-add-automation"]') as HTMLButtonElement).click();
    (page.querySelector('[aria-label="Profile"]') as HTMLButtonElement).click();
    (page.querySelector('[aria-label="Logout"]') as HTMLButtonElement).click();
    expect(add).toHaveBeenCalledTimes(1);
    expect(profile).toHaveBeenCalledTimes(1);
    expect(logout).toHaveBeenCalledTimes(1);
  });

  for (const state of ['empty', 'loaded', 'management'] as const) {
    it(`fits ${state} automation content within a 375px shell without clipping`, () => {
      const name = 'Automation'.repeat(20);
      const service = TestBed.inject(AUTOMATIONS_SERVICE_TOKEN) as any;
      service.getAutomations.and.returnValue(of(state === 'empty' ? [] : [
        { id: 'synthetic', name, host: 'backend', position: 0 },
      ]));
      const preferences = TestBed.inject(ModuleViewPreferencesService);
      preferences.setMode('automations', state === 'management' ? 'advanced' : 'basic');
      if (state === 'management') preferences.setSection('automations', 'automation-management', true);

      const shell = document.createElement('div');
      shell.style.cssText = 'width: 375px; margin-left: 24px;';
      document.body.appendChild(shell);
      shell.appendChild(fixture.nativeElement);
      try {
        fixture.detectChanges();
        const page: HTMLElement = fixture.nativeElement;
        const bounds = shell.getBoundingClientRect();
        for (const selector of ['.header-container', '.header-content', '.header-content__actions', '.content-container', '.card-container', '.automation-empty', '.automation-management-row']) {
          const element = page.querySelector(selector) as HTMLElement | null;
          if (!element) continue;
          expect(element.getBoundingClientRect().right).withContext(selector).toBeLessThanOrEqual(bounds.right + 1);
          expect(element.scrollWidth).withContext(selector).toBeLessThanOrEqual(element.clientWidth + 1);
          expect(getComputedStyle(element).overflowX).withContext(selector).not.toMatch(/hidden|clip/);
        }
        expect(getComputedStyle(page.querySelector('.header-container')!).position).not.toBe('fixed');
        if (state !== 'empty') {
          const launch = page.querySelector('.card-container a') as HTMLAnchorElement;
          expect(launch.getAttribute('href')).toBe('/backend');
          expect(launch.getAttribute('aria-label')).toBe(name);
          expect(page.querySelector('.card-container__card__title')?.textContent).toBe(name);
        }
        if (state === 'management') {
          expect(page.querySelector('[aria-label="Edit ' + name + '"]')).not.toBeNull();
          expect(page.querySelector('[aria-label="Delete ' + name + '"]')).not.toBeNull();
        }
      } finally {
        shell.remove();
      }
    });
  }

  it('keeps automation launch and add actions in Basic and persists its management disclosure', () => {
    fixture.detectChanges();

    const page: HTMLElement = fixture.nativeElement;
    expect(page.querySelector('[data-testid="home-add-automation"]')).not.toBeNull();
    expect(page.querySelector('[data-testid="automation-empty-add"]')).not.toBeNull();

    expect(page.querySelector('hai-progressive-section[sectionid="automation-management"] .hai-progressive-section')).toBeNull();
    const preferences = TestBed.inject(ModuleViewPreferencesService);
    preferences.setMode('automations', 'advanced');
    fixture.detectChanges();
    const section = page.querySelector('hai-progressive-section[sectionid="automation-management"]') as HTMLElement;
    expect(section.getAttribute('moduleid')).toBe('automations');
    expect(section.getAttribute('sectionid')).toBe('automation-management');
    expect(section.querySelector('.hai-progressive-section--advanced')).not.toBeNull();

    const toggle = section.querySelector('button') as HTMLButtonElement;
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    toggle.click();
    fixture.detectChanges();

    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(TestBed.inject(ModuleViewPreferencesService).get('automations').openSections['automation-management']).toBeTrue();
  });

  it('does not present a failed automation fetch as a confirmed empty registry', () => {
    fixture.detectChanges();
    const service = TestBed.inject(AUTOMATIONS_SERVICE_TOKEN) as any;
    service.getAutomations.and.returnValue(throwError(() => new Error('offline')));
    component.loadAutomations();
    fixture.detectChanges();

    expect(component.automationLoadState).toBe('error');
    expect(fixture.nativeElement.querySelector('.automation-empty h2')?.textContent).toContain('could not be loaded');
    expect(fixture.nativeElement.querySelector('[data-testid="automation-empty-add"]')).toBeNull();
  });

  it('treats malformed registry data as unavailable instead of sorting it as an empty list', () => {
    fixture.detectChanges();
    const service = TestBed.inject(AUTOMATIONS_SERVICE_TOKEN) as any;
    service.getAutomations.and.returnValue(of(null));

    component.loadAutomations();
    fixture.detectChanges();

    expect(component.automationLoadState).toBe('error');
    expect(fixture.nativeElement.querySelector('.automation-empty h2')?.textContent).not.toContain('No automations');
  });

  it('keeps a failed automation submission reviewable and allows retry', () => {
    fixture.detectChanges();
    const service = TestBed.inject(AUTOMATIONS_SERVICE_TOKEN) as any;
    const form = {
      submitting: true,
      isUpdate: false,
      completeSubmission: jasmine.createSpy('completeSubmission'),
      failSubmission: jasmine.createSpy('failSubmission').and.callFake(function(this: { submitting: boolean }) {
        this.submitting = false;
      }),
    };
    component.automationModal = form as any;
    service.addAutomation.and.returnValue(throwError(() => new Error('offline')));

    component.handleFormDataAutomationSubmitted({ name: 'Draft' } as any);

    expect(form.failSubmission).toHaveBeenCalledTimes(1);
    expect(form.completeSubmission).not.toHaveBeenCalled();
    expect(form.submitting).toBeFalse();
  });

  it('restores the prior automation order when the reorder request fails', () => {
    const service = TestBed.inject(AUTOMATIONS_SERVICE_TOKEN) as any;
    const first = { id: 'first', name: 'First', position: 0 };
    const second = { id: 'second', name: 'Second', position: 1 };
    component.automations = [first, second] as any;
    service.swapAutomations.and.returnValue(throwError(() => new Error('offline')));
    service.getAutomations.and.returnValue(of([first, second]));

    component.drop({ previousIndex: 0, currentIndex: 1 } as any);

    expect(component.automations.map((item) => item.id)).toEqual(['first', 'second']);
    expect(component.automationReorderInFlight).toBeFalse();
    expect(component.automationLoadState).toBe('loaded');
  });

});
