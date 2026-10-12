import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ReactiveFormsModule } from '@angular/forms';
import { RouterTestingModule } from '@angular/router/testing';
import { NO_ERRORS_SCHEMA } from '@angular/core';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { NzModalService } from 'ng-zorro-antd/modal';
import { throwError } from 'rxjs';

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
      imports: [ReactiveFormsModule, RouterTestingModule],
      schemas: [NO_ERRORS_SCHEMA],
      providers: [
        { provide: NzNotificationService, useValue: {} },
        { provide: NzModalService, useValue: {} },
        { provide: AUTOMATIONS_SERVICE_TOKEN, useValue: {} },
        { provide: AUTH_SERVICE_TOKEN, useValue: {} },
        { provide: USER_SERVICE_TOKEN, useValue: {} },
      ],
    });
    fixture = TestBed.createComponent(HomeComponent);
    component = fixture.componentInstance;
    // Shallow creation smoke test: no detectChanges so ngOnInit's data loading
    // (which needs a full service mock) is not exercised here.
  });

  it('should create', () => {
    expect(component).toBeTruthy();
  });

  it('restores the visible automation order when persistence fails', () => {
    const notification = TestBed.inject(NzNotificationService) as unknown as { error: jasmine.Spy };
    notification.error = jasmine.createSpy('error');
    const automationsService = TestBed.inject(AUTOMATIONS_SERVICE_TOKEN) as unknown as {
      swapAutomations: jasmine.Spy;
    };
    automationsService.swapAutomations = jasmine
      .createSpy('swapAutomations')
      .and.returnValue(throwError(() => new Error('offline')));
    component.automations = [
      { id: 'first', name: 'First', image: '', port: 1, position: 0, host: '', removeImage: false },
      { id: 'second', name: 'Second', image: '', port: 2, position: 1, host: '', removeImage: false },
    ];

    component.drop({ previousIndex: 0, currentIndex: 1 } as any);

    expect(component.automations.map((automation) => automation.id)).toEqual(['first', 'second']);
    expect(notification.error).toHaveBeenCalled();
  });

  it('keeps the profile modal closed and explains when the profile cannot load', () => {
    const notification = TestBed.inject(NzNotificationService) as unknown as { error: jasmine.Spy };
    notification.error = jasmine.createSpy('error');
    const userService = TestBed.inject(USER_SERVICE_TOKEN) as unknown as {
      getUser: jasmine.Spy;
    };
    userService.getUser = jasmine
      .createSpy('getUser')
      .and.returnValue(throwError(() => new Error('offline')));

    component.showProfileModal();

    expect(component.isProfileVisible).toBeFalse();
    expect(notification.error).toHaveBeenCalledWith(
      'Profile unavailable',
      'Your profile could not be loaded. Check the connection and try again.'
    );
  });

  it('records a failed automation load instead of treating it as an empty registry', () => {
    const notification = TestBed.inject(NzNotificationService) as unknown as { create: jasmine.Spy };
    notification.create = jasmine.createSpy('create');
    const automationsService = TestBed.inject(AUTOMATIONS_SERVICE_TOKEN) as unknown as {
      getAutomations: jasmine.Spy;
    };
    automationsService.getAutomations = jasmine
      .createSpy('getAutomations')
      .and.returnValue(throwError(() => new Error('offline')));

    component.loadAutomations();

    expect(component.automationLoadFailed).toBeTrue();
  });
});
