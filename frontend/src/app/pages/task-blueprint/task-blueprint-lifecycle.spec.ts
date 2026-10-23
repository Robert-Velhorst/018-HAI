import { fakeAsync, tick } from '@angular/core/testing';
import { FormBuilder } from '@angular/forms';
import { Subject, of } from 'rxjs';
import { TaskBlueprintComponent } from './task-blueprint.component';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';

describe('Task Planning observation lifetime', () => {
  const pending: Subject<any>[] = [];
  const subject = () => {
    const value = new Subject<any>();
    pending.push(value);
    return value;
  };
  function setup() {
    const task = jasmine.createSpyObj('task', ['logs', 'reviewQueue', 'resolveReviewItem', 'reconcileApprovedReviews']);
    task.logs.and.returnValue(of([]));
    task.reviewQueue.and.returnValue(of([]));
    const assistant = jasmine.createSpyObj('assistant', ['command']);
    const typed = jasmine.createSpyObj('typed', ['propose']);
    const crew = jasmine.createSpyObj('crew', ['propose']);
    const notification = jasmine.createSpyObj('notification', ['success', 'info', 'warning', 'error']);
    const modal = jasmine.createSpyObj('modal', ['confirm']);
    const route = subject();
    const preferences = new ModuleViewPreferencesService(document);
    const component = new TaskBlueprintComponent(
      new FormBuilder(), task, assistant, notification, modal, {} as any,
      { queryParamMap: route } as any, typed, crew, preferences,
    );
    component.planForm.patchValue({ request: 'Prepare a source-linked draft.' });
    return { component, task, assistant, typed, crew, notification, modal, route };
  }
  const destroy = (component: TaskBlueprintComponent) => (component as any).ngOnDestroy?.();
  afterEach(() => {
    pending.splice(0).forEach(value => value.complete());
    localStorage.removeItem('hai.module-view.v1.task-blueprint');
  });

  for (const kind of ['logs', 'queue', 'command', 'typed', 'crew', 'decision', 'retry history', 'recovery']) {
    it(`ends ${kind} observation on destruction without late failure notifications`, () => {
      const { component, task, assistant, typed, crew, notification } = setup();
      const response = subject();
      if (kind === 'logs') { task.logs.and.returnValue(response); component.loadLogs(); }
      if (kind === 'queue') { task.reviewQueue.and.returnValue(response); component.loadReviewQueue(); }
      if (kind === 'command') { assistant.command.and.returnValue(response); component.createPlan(); }
      if (kind === 'typed') { typed.propose.and.returnValue(response); component.requestTypedProposal(); }
      if (kind === 'crew') { crew.propose.and.returnValue(response); component.requestCrewProposal(); }
      if (kind === 'decision') {
        task.resolveReviewItem.and.returnValue(response);
        component.resolveReviewItem({ id: 'review-1', status: 'open' } as any, false);
      }
      if (kind === 'retry history') {
        component.uncertainCommand = { intent: 'run', request: { message: 'Prepare a source-linked draft.' } };
        task.logs.and.returnValue(response);
        task.reviewQueue.and.returnValue(response);
        component.refreshHistoryBeforeUncertainRetry();
      }
      if (kind === 'recovery') {
        task.reconcileApprovedReviews.and.returnValue(response);
        component.previewApprovedReviewRecovery();
      }
      expect(response.observed).toBeTrue();

      destroy(component);

      expect(response.observed).toBeFalse();
      const messages = component.chatMessages;
      response.error(new Error('fixture failure after navigation'));
      expect(component.chatMessages).toBe(messages);
      for (const method of ['error', 'warning', 'info', 'success']) {
        expect(notification[method]).not.toHaveBeenCalled();
      }
      expect(component.commandInFlight).toBeFalse();
      expect(component.reviewQueueLoading).toBeFalse();
      expect(component.typedProposalLoading).toBeFalse();
      expect(component.crewProposalLoading).toBeFalse();
      expect(component.retryCheckLoading).toBeFalse();
      expect(component.reconcilingReviews).toBeFalse();
    });
  }

  it('ends the route observer and refuses all new service calls after destruction', () => {
    const { component, route, task, assistant, typed, crew } = setup();
    component.ngOnInit();
    expect(route.observed).toBeTrue();
    destroy(component);
    expect(route.observed).toBeFalse();
    task.logs.calls.reset(); task.reviewQueue.calls.reset();

    component.ngOnInit();
    component.loadLogs(); component.loadReviewQueue(); component.createPlan(); component.runAgentCycle();
    component.requestTypedProposal(); component.requestCrewProposal(); component.previewApprovedReviewRecovery();
    component.resolveReviewItem({ id: 'review-1', status: 'open' } as any, false);

    expect(route.observed).toBeFalse();
    for (const method of ['logs', 'reviewQueue', 'reconcileApprovedReviews', 'resolveReviewItem']) {
      expect(task[method]).not.toHaveBeenCalled();
    }
    expect(assistant.command).not.toHaveBeenCalled();
    expect(typed.propose).not.toHaveBeenCalled(); expect(crew.propose).not.toHaveBeenCalled();
  });

  for (const kind of ['retry', 'operation review', 'recovery']) {
    it(`cannot dispatch a retained ${kind} confirmation after destruction`, () => {
      const { component, task, assistant, modal } = setup();
      if (kind === 'retry') {
        component.uncertainCommand = { intent: 'run', request: { message: 'Prepare a source-linked draft.' } };
        component.refreshHistoryBeforeUncertainRetry();
      } else if (kind === 'operation review') {
        component.resolveReviewItem({ id: 'review-1', status: 'needs_review', taskId: 'operation:prior' } as any, true);
      } else {
        component.reconciliation = { dryRun: true, eligible: 1, items: [] } as any;
        component.reconciliationPreviewAt = Date.now();
        component.applyApprovedReviewRecovery();
      }
      expect(modal.confirm).toHaveBeenCalledTimes(1);
      const confirmation = modal.confirm.calls.mostRecent().args[0];
      destroy(component);
      confirmation.nzOnOk();
      expect(assistant.command).not.toHaveBeenCalled();
      expect(task.resolveReviewItem).not.toHaveBeenCalled();
      expect(task.reconcileApprovedReviews).not.toHaveBeenCalled();
    });
  }

  it('does not let a delayed section focus target a replacement page', fakeAsync(() => {
    const { component } = setup();
    const section = document.createElement('section');
    section.id = 'task-inspector';
    const button = document.createElement('button');
    button.className = 'hai-progressive-section__summary';
    section.append(button);
    document.body.append(section);
    const focus = spyOn(button, 'focus');
    const scroll = spyOn(section, 'scrollIntoView');
    try {
      component.showInspector('evidence');
      destroy(component);
      tick();
      expect(focus).not.toHaveBeenCalled();
      expect(scroll).not.toHaveBeenCalled();
    } finally { section.remove(); }
  }));
});
