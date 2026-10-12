import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ReactiveFormsModule } from '@angular/forms';
import { NO_ERRORS_SCHEMA } from '@angular/core';
import { NzNotificationService } from 'ng-zorro-antd/notification';

import { AutomationsFormComponent } from './automations-form.component';

describe('AutomationsFormComponent', () => {
  let component: AutomationsFormComponent;
  let fixture: ComponentFixture<AutomationsFormComponent>;

  beforeEach(() => {
    TestBed.configureTestingModule({
      declarations: [AutomationsFormComponent],
      imports: [ReactiveFormsModule],
      schemas: [NO_ERRORS_SCHEMA],
      providers: [{ provide: NzNotificationService, useValue: {} }],
    });
    fixture = TestBed.createComponent(AutomationsFormComponent);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('should create', () => {
    expect(component).toBeTruthy();
  });

  it('keeps the editor open until the parent confirms the saved OpenClaw model', () => {
    const automation = { name: 'Draft', host: 'localhost', port: 80, position: 0, image: '',
      removeImage: false, runtimeType: 'openclaw', launchType: 'agent_runtime',
      runtimeModel: 'ollama/qwen3:14b' };
    component.openModal(automation, true);
    expect(component.automationForm.get('runtimeModel')?.value).toBe(automation.runtimeModel);
    const submit = spyOn(component.formDataSubmitted, 'emit');
    component.onSubmit();
    expect(submit).toHaveBeenCalledWith(jasmine.objectContaining({ runtimeModel: automation.runtimeModel }));
    expect(component.isVisible).toBeTrue();
    expect(component.submitting).toBeTrue();

    component.failSubmission();
    expect(component.isVisible).toBeTrue();
    expect(component.submitting).toBeFalse();
    component.completeSubmission();
    expect(component.isVisible).toBeFalse();
  });

  it('clears a hidden invalid model when switching away from OpenClaw', () => {
    component.automationForm.patchValue({ name: 'Draft', host: 'localhost', port: 80,
      runtimeType: 'openclaw', launchType: 'agent_runtime', runtimeModel: 'invalid model' });
    expect(component.automationForm.valid).toBeFalse();
    component.automationForm.get('runtimeType')?.setValue('hermes');
    expect(component.automationForm.get('runtimeModel')?.value).toBe('');
    expect(component.automationForm.valid).toBeTrue();
  });
});
