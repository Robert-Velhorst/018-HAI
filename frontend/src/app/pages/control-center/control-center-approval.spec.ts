import { EMPTY, of, Subject } from 'rxjs';
import { ControlCenterComponent } from './control-center.component';

describe('Command Center approval acknowledgement', () => {
  const id = '11111111-1111-4111-8111-111111111111';
  const otherId = '22222222-2222-4222-8222-222222222222';
  function setup() {
    const workflow = jasmine.createSpyObj('workflow', ['resolveApproval']);
    const notification = jasmine.createSpyObj('notification', ['success', 'info', 'warning', 'error']);
    const component = new ControlCenterComponent(
      {} as any, workflow, {} as any, {} as any, {} as any, {} as any, {} as any,
      notification, {} as any, { get: () => ({ mode: 'basic' }) } as any, { markForCheck() {} } as any,
    );
    const item = { id, requiresApproval: true, approvalStatus: 'pending', currentState: 'needs_approval' } as any;
    component.workflowDashboard = { counts: { approvals: 1, blocked: 0, dueOpenLoops: 0 }, approvalItems: [item] } as any;
    const refresh = spyOn(component, 'refresh');
    return { component, workflow, notification, item, refresh };
  }
  function receipt(approved: boolean): any {
    return {
      item: { id, requiresApproval: true, approvalStatus: approved ? 'approved' : 'rejected', currentState: approved ? 'ready' : 'blocked' },
      decisions: [{ id: otherId, workflowId: id, decisionType: 'approval',
        decision: approved ? 'approved' : 'rejected', approved, createdAt: '2026-10-23T12:00:00Z' }],
    };
  }

  for (const approved of [true, false]) {
    it(`reports only a bound ${approved ? 'approval' : 'rejection'} acknowledgement, not execution`, () => {
      const { component, workflow, notification, item, refresh } = setup();
      workflow.resolveApproval.and.returnValue(of(receipt(approved)));
      component.resolveApproval(item, approved);
      expect(workflow.resolveApproval).toHaveBeenCalledTimes(1);
      expect(notification.success).not.toHaveBeenCalled();
      expect(notification.info).toHaveBeenCalledWith('Decision response received', jasmine.any(String));
      expect(component.resolvingId).toBe('');
      expect(refresh).toHaveBeenCalledTimes(1);
    });
  }

  for (const invalid of ['absent', 'foreign workflow', 'wrong approval', 'wrong state', 'not approval gated',
    'missing audit', 'foreign audit', 'wrong audit decision', 'wrong audit flag', 'wrong audit type', 'invalid audit id', 'invalid audit time']) {
    it(`does not claim success for ${invalid}`, () => {
      const { component, workflow, notification, item, refresh } = setup();
      let result = receipt(true);
      if (invalid === 'absent') result = undefined;
      if (invalid === 'foreign workflow') result.item.id = otherId;
      if (invalid === 'wrong approval') result.item.approvalStatus = 'rejected';
      if (invalid === 'wrong state') result.item.currentState = 'completed';
      if (invalid === 'not approval gated') result.item.requiresApproval = false;
      if (invalid === 'missing audit') result.decisions = [];
      if (invalid === 'foreign audit') result.decisions[0].workflowId = otherId;
      if (invalid === 'wrong audit decision') result.decisions[0].decision = 'rejected';
      if (invalid === 'wrong audit flag') result.decisions[0].approved = false;
      if (invalid === 'wrong audit type') result.decisions[0].decisionType = 'proposal';
      if (invalid === 'invalid audit id') result.decisions[0].id = 'not-an-id';
      if (invalid === 'invalid audit time') result.decisions[0].createdAt = 'not-a-time';
      workflow.resolveApproval.and.returnValue(of(result));
      component.resolveApproval(item, true);
      expect(notification.success).not.toHaveBeenCalled();
      expect(notification.info).not.toHaveBeenCalled();
      expect(notification.warning).toHaveBeenCalledWith('Decision outcome unconfirmed', jasmine.any(String));
      expect(component.resolvingId).toBe('');
      expect(refresh).toHaveBeenCalledTimes(1);
    });
  }

  for (const invalid of ['stale reference', 'not pending', 'wrong state', 'not gated', 'invalid id', 'nonboolean decision']) {
    it(`refuses a ${invalid} approval before dispatch`, () => {
      const { component, workflow, item } = setup();
      const selected = invalid === 'stale reference' ? { ...item } : item;
      if (invalid === 'not pending') selected.approvalStatus = 'approved';
      if (invalid === 'wrong state') selected.currentState = 'completed';
      if (invalid === 'not gated') selected.requiresApproval = false;
      if (invalid === 'invalid id') selected.id = 'not-an-id';
      component.resolveApproval(selected, invalid === 'nonboolean decision' ? 'true' as any : true);
      expect(workflow.resolveApproval).not.toHaveBeenCalled();
    });
  }

  it('handles empty completion and synchronous service failure as unconfirmed outcomes', () => {
    for (const kind of ['empty', 'throw']) {
      const { component, workflow, notification, item, refresh } = setup();
      if (kind === 'empty') workflow.resolveApproval.and.returnValue(EMPTY);
      else workflow.resolveApproval.and.throwError('fixture service failure');
      expect(() => component.resolveApproval(item, true)).not.toThrow();
      expect(component.resolvingId).toBe('');
      expect(notification.warning).toHaveBeenCalled();
      expect(notification.success).not.toHaveBeenCalled();
      expect(refresh).toHaveBeenCalledTimes(1);
    }
  });

  it('consumes only one decision response and releases the observer on destruction', () => {
    const { component, workflow, item, notification, refresh } = setup();
    const response = new Subject<any>();
    workflow.resolveApproval.and.returnValue(response);
    component.resolveApproval(item, true);
    response.next(receipt(true));
    expect(response.observed).toBeFalse();
    response.next(receipt(true));
    expect(refresh).toHaveBeenCalledTimes(1);
    expect(notification.info).toHaveBeenCalledTimes(1);

    component.resolveApproval(item, false);
    expect(response.observed).toBeTrue();
    component.ngOnDestroy();
    expect(response.observed).toBeFalse();
    expect(component.resolvingId).toBe('');
    component.resolveApproval(item, true);
    expect(workflow.resolveApproval).toHaveBeenCalledTimes(2);
  });
});
