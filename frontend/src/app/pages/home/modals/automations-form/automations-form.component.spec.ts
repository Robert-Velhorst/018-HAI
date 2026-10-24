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

  it('restores creation defaults after closing and reopening the editor', () => {
    component.openModal();
    component.closeModal();
    component.openModal();
    expect(component.automationForm.value).toEqual(jasmine.objectContaining({
      launchType: 'browser_url', runtimeType: 'browser', healthCheckType: 'http',
      healthCheckIntervalSeconds: 60, expectedHttpStatus: 200,
    }));
  });

  it('does not carry the prior record or uploaded image into a new automation', () => {
    component.openModal({ id: 'prior-record', name: 'Existing', host: 'localhost', port: 80,
      position: 0, image: '', removeImage: false, runtimeType: 'openclaw',
      launchType: 'agent_runtime', runtimeModel: 'ollama/qwen3:14b' }, true);
    component.selectedImage = new File(['synthetic image'], 'fixture.png', { type: 'image/png' });
    component.removeImage = true;
    component.openModal();
    expect(component.automationForm.get('id')?.value).toBeNull();
    expect(component.automationForm.get('name')?.value).toBeFalsy();
    expect(component.automationForm.get('runtimeModel')?.value).toBeFalsy();
    expect(component.selectedImage).toBeUndefined();
    expect(component.removeImage).toBeFalse();
    expect(component.modalTitle).toBe('Add Automation');
  });

  it('does not replace the record or editor state during an unconfirmed save', () => {
    const original = { id: 'original-record', name: 'Original', host: 'localhost', port: 80,
      position: 0, image: '', removeImage: false };
    component.openModal(original, true);
    component.onSubmit();
    component.openModal({ ...original, id: 'different-record', name: 'Other' }, true);
    expect(component.automationForm.get('id')?.value).toBe('original-record');
    expect(component.automationForm.get('name')?.value).toBe('Original');
    expect(component.submitting).toBeTrue();
    expect(component.isVisible).toBeTrue();
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
