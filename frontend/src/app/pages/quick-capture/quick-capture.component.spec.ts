import { FormBuilder } from '@angular/forms';
import { CommonModule } from '@angular/common';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { NO_ERRORS_SCHEMA } from '@angular/core';
import { ReactiveFormsModule } from '@angular/forms';
import { of, throwError } from 'rxjs';
import { ControlRoomModule } from '../../control-room/control-room.module';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { ContextMemoryService } from '../../services/context-memory/context-memory.service';
import { QuickCaptureComponent } from './quick-capture.component';

describe('QuickCaptureComponent', () => {
  beforeEach(() => localStorage.removeItem('hai_quick_capture_draft'));

  function make(memoryService?: Pick<ContextMemoryService, 'create'>): QuickCaptureComponent {
    return new QuickCaptureComponent(
      new FormBuilder(),
      (memoryService || { create: () => of({}) }) as ContextMemoryService
    );
  }

  it('is invalid empty and valid when filled', () => {
    const c = make();
    expect(c.form.valid).toBeFalse();
    c.form.setValue({ title: 'A title', content: 'enough content' });
    expect(c.form.valid).toBeTrue();
  });

  it('enforces content minimum length', () => {
    const c = make();
    c.form.setValue({ title: 'ok', content: 'ab' });
    expect(c.contentControl?.hasError('minlength')).toBeTrue();
  });

  it('autosaves a draft and clears it', () => {
    const c = make();
    c.autosave({ title: 't', content: 'hello there' });
    expect(localStorage.getItem('hai_quick_capture_draft')).toContain('hello there');
    expect(c.savedAt).not.toBeNull();

    c.clearDraft();
    expect(localStorage.getItem('hai_quick_capture_draft')).toBeNull();
    expect(c.savedAt).toBeNull();
  });

  it('does not submit when invalid', () => {
    const c = make();
    c.submit();
    expect(c.submitted).toBeFalse();
  });

  it('stores a valid capture in local memory before clearing the draft', () => {
    const create = jasmine.createSpy().and.returnValue(of({}));
    const c = make({ create });
    c.form.setValue({ title: 'Call solicitor', content: 'Prepare the evidence list.' });

    c.submit();

    expect(create).toHaveBeenCalledWith(jasmine.objectContaining({
      kind: 'note',
      summary: 'Call solicitor',
      content: 'Prepare the evidence list.',
      sourceLabel: 'Quick capture',
    }));
    expect(c.submitted).toBeTrue();
    expect(c.form.value).toEqual({ title: '', content: '' });
  });

  it('retains the form when local memory rejects the capture', () => {
    const c = make({ create: () => throwError(() => new Error('offline')) });
    c.form.setValue({ title: 'Call solicitor', content: 'Prepare the evidence list.' });

    c.submit();

    expect(c.submitted).toBeFalse();
    expect(c.saveError).toContain('Could not save');
    expect(c.form.value.content).toBe('Prepare the evidence list.');
  });
});

describe('QuickCaptureComponent progressive disclosure', () => {
  let fixture: ComponentFixture<QuickCaptureComponent>;
  let memoryService: jasmine.SpyObj<ContextMemoryService>;

  beforeEach(async () => {
    memoryService = jasmine.createSpyObj<ContextMemoryService>('ContextMemoryService', ['create']);
    memoryService.create.and.returnValue(of({} as any));
    await TestBed.configureTestingModule({
      declarations: [QuickCaptureComponent],
      imports: [CommonModule, ReactiveFormsModule, ControlRoomModule],
      schemas: [NO_ERRORS_SCHEMA],
      providers: [{ provide: ContextMemoryService, useValue: memoryService }],
    }).compileComponents();
    TestBed.inject(ModuleViewPreferencesService).reset('quick-capture');
    fixture = TestBed.createComponent(QuickCaptureComponent);
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture?.destroy();
    TestBed.inject(ModuleViewPreferencesService).reset('quick-capture');
    localStorage.removeItem('hai_quick_capture_draft');
  });

  it('keeps capture and failure feedback in Basic while persisting its detail disclosure', () => {
    const page: HTMLElement = fixture.nativeElement;
    expect(page.querySelector('#qc-title-input')).not.toBeNull();
    expect(page.querySelector('#qc-content-input')).not.toBeNull();
    expect(page.querySelector('button[type="submit"]')?.textContent).toContain('Save to memory');

    expect(page.querySelector('hai-progressive-section[sectionid="capture-details"] .hai-progressive-section')).toBeNull();

    memoryService.create.and.returnValue(throwError(() => new Error('offline')));
    fixture.componentInstance.form.setValue({ title: 'Keep this draft', content: 'Retry after the memory service returns.' });
    fixture.componentInstance.submit();
    fixture.detectChanges();

    expect(page.querySelector('.quick-capture__error')?.textContent).toContain('Your draft is still available');
    const preferences = TestBed.inject(ModuleViewPreferencesService);
    preferences.setMode('quick-capture', 'advanced');
    fixture.detectChanges();
    const section = page.querySelector('hai-progressive-section[sectionid="capture-details"]') as HTMLElement;
    expect(section.getAttribute('moduleid')).toBe('quick-capture');
    expect(section.querySelector('.hai-progressive-section--advanced')).not.toBeNull();
    expect(section.querySelector('.hai-progressive-section__content')).toBeNull();

    (section.querySelector('button') as HTMLButtonElement).click();
    fixture.detectChanges();
    expect(TestBed.inject(ModuleViewPreferencesService).get('quick-capture').openSections['capture-details']).toBeTrue();
  });
});
