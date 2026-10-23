import { fakeAsync, tick } from '@angular/core/testing';
import { EMPTY, of, Subject } from 'rxjs';
import { FrameworkRegistryComponent } from './framework-registry.component';
import { HttpTimeoutPolicy } from '../../shared/http-timeout-policy';
import { HttpErrorResponse } from '@angular/common/http';

describe('Framework registry request observation', () => {
  const owner = { authenticated: true, subject: 'fixture-owner', role: 'owner',
    permissions: { canRead: true, canOperate: true, canApprove: true, canAdminister: true } };
  const framework = { id: 'formal-planning', name: 'Planning', family: 'planning',
    maximumAutonomyLevel: 3, effectiveAutonomyLevel: 2, adaptations: [], enabled: true,
    pinned: false, status: 'active', suitableProblemTypes: [], triggerConditions: [] };
  const constitution = { id: 'fixture-constitution', version: 1, status: 'active',
    values: [], prohibitions: [], standingPermissions: [], preferences: [], relationshipRules: [],
    financialBoundaries: [], communicationRules: [], escalationRules: [], protectedRules: [] };
  const selection = { id: 'fixture-selection', createdAt: '2026-10-23T00:00:00Z', selected: [] };
  const readPaths = [
    ['session', 'authorization'], ['overview', 'overview'], ['frameworks', 'frameworks'],
    ['selections', 'selections'], ['constitution', 'constitution'], ['constitutionHistory', 'constitutionHistory'],
  ];
  const operations = [
    ...readPaths.map(([method, errorKey]) => ({ method, errorKey, mutation: false,
      start: (c: FrameworkRegistryComponent) => c.refresh() })),
    { method: 'familyTaxonomy', errorKey: 'familyTaxonomy', mutation: false,
      start: (c: FrameworkRegistryComponent) => c.loadFamilyTaxonomy() },
    { method: 'framework', errorKey: 'inspector', mutation: false,
      start: (c: FrameworkRegistryComponent) => c.openFrameworkById(framework.id) },
    { method: 'preferenceHistory', errorKey: 'history', mutation: false,
      start: (c: FrameworkRegistryComponent) => c.openFrameworkById(framework.id) },
    { method: 'overview', errorKey: 'overview-followup', mutation: false,
      start: (c: FrameworkRegistryComponent) => (c as any).refreshOverview() },
    { method: 'select', errorKey: 'selection', mutation: true,
      start: (c: FrameworkRegistryComponent) => { c.selectionDraft.request = 'Prepare a plan'; c.selectFrameworks(); } },
    { method: 'updatePreference', errorKey: 'preference', mutation: true,
      start: (c: FrameworkRegistryComponent) => { c.selectedFramework = framework as any; c.savePreference(); } },
    { method: 'createConstitutionDraft', errorKey: 'draft', mutation: true,
      start: (c: FrameworkRegistryComponent) => {
        c.constitution = constitution as any; c.constitutionEditor.changeSummary = 'Fixture amendment'; c.createConstitutionDraft();
      } },
    { method: 'activateConstitution', errorKey: 'activation', mutation: true,
      start: (c: FrameworkRegistryComponent) => {
        c.constitutionDraft = { ...constitution, id: 'fixture-draft', version: 2, status: 'draft' } as any;
        c.activationConfirmation = 'ACTIVATE CONSTITUTION'; c.activationNote = 'Fixture owner reviewed'; c.activateConstitutionDraft();
      } },
  ];

  function fixture() {
    const responses: Record<string, unknown> = { session: owner,
      overview: { total: 1, families: {}, constitutionVersion: 1 }, frameworks: [framework], selections: [],
      constitution: { constitution, source: 'fixture' }, constitutionHistory: { history: [], truncated: false },
      familyTaxonomy: { version: 'fixture', digest: 'a'.repeat(64), families: [] },
      framework, preferenceHistory: [], select: selection, updatePreference: framework,
      createConstitutionDraft: { ...constitution, id: 'fixture-draft', version: 2, status: 'draft' },
      activateConstitution: { ...constitution, version: 2 } };
    const service: Record<string, jasmine.Spy> = {};
    for (const [method, response] of Object.entries(responses)) service[method] = jasmine.createSpy(method).and.returnValue(of(response));
    const notification = jasmine.createSpyObj('notification', ['success', 'warning', 'error']);
    const c = new FrameworkRegistryComponent(service as any, { session: service['session'] } as any,
      notification, {} as any, { get: () => ({ mode: 'basic' }) } as any);
    c.authSession = owner as any;
    return { c, service, notification };
  }

  function busy(c: FrameworkRegistryComponent): boolean {
    return [c.loading, c.selectionBusy, c.familyTaxonomyLoading, c.inspectorLoading,
      c.preferenceHistoryLoading, c.preferenceSaving, c.constitutionSaving, c.constitutionActivating].some(Boolean);
  }
  function failed(c: FrameworkRegistryComponent, notification: any): boolean {
    return Object.keys(c.loadErrors).length > 0 || !!c.inspectorError || !!c.preferenceHistoryError || notification.error.calls.count() > 0;
  }

  for (const operation of operations) {
    const label = `${operation.method}/${operation.errorKey}`;
    it(`${label} accepts its first successful response`, () => {
      const f = fixture(); operation.start(f.c);
      expect(f.service[operation.method]).toHaveBeenCalled();
      expect(busy(f.c)).toBeFalse();
      expect(f.notification.error).not.toHaveBeenCalled();
    });
    it(`${label} reports empty completion instead of silent success`, () => {
      const f = fixture(); f.service[operation.method].and.returnValue(EMPTY); operation.start(f.c);
      expect(busy(f.c)).toBeFalse(); expect(failed(f.c, f.notification)).toBeTrue();
      expect(f.notification.success).not.toHaveBeenCalled();
    });
    it(`${label} catches synchronous service failures and releases state`, () => {
      const f = fixture(); f.service[operation.method].and.callFake(() => { throw new Error('PRIVATE_SERVICE_CANARY'); });
      expect(() => operation.start(f.c)).not.toThrow();
      expect(busy(f.c)).toBeFalse(); expect(failed(f.c, f.notification)).toBeTrue();
      expect(JSON.stringify([f.c.loadErrors, f.notification.error.calls.allArgs()])).not.toContain('PRIVATE_SERVICE_CANARY');
    });
    it(`${label} has a bounded response deadline`, fakeAsync(() => {
      const f = fixture(); const pending = new Subject<any>(); f.service[operation.method].and.returnValue(pending);
      operation.start(f.c); tick(operation.mutation ? HttpTimeoutPolicy.operationMs : HttpTimeoutPolicy.readMs);
      expect(pending.observed).toBeFalse(); expect(busy(f.c)).toBeFalse(); expect(failed(f.c, f.notification)).toBeTrue();
    }));
    it(`${label} releases pending observations on destruction`, () => {
      const f = fixture(); const pending = new Subject<any>(); f.service[operation.method].and.returnValue(pending);
      operation.start(f.c); expect(pending.observed).toBeTrue(); f.c.ngOnDestroy();
      expect(pending.observed).toBeFalse(); expect(busy(f.c)).toBeFalse();
    });
    it(`${label} does not invoke a service after destruction`, () => {
      const f = fixture(); f.c.ngOnDestroy(); operation.start(f.c);
      expect(f.service[operation.method]).not.toHaveBeenCalled(); expect(busy(f.c)).toBeFalse();
    });
    it(`${label} stops observing after one response even if the source stays open`, () => {
      const f = fixture(); const pending = new Subject<any>();
      // Obtain the ordinary fixture value without depending on RxJS internals.
      let value: unknown; f.service[operation.method]().subscribe((result: unknown) => value = result);
      f.service[operation.method].calls.reset(); f.service[operation.method].and.returnValue(pending);
      operation.start(f.c); pending.next(value);
      expect(pending.observed).toBeFalse(); expect(busy(f.c)).toBeFalse();
    });
  }

  for (const field of ['authenticated', 'canAdminister', 'canOperate', 'missingPermissions']) {
    it(`does not coerce ${field} into component authority`, () => {
      const f = fixture();
      const session: any = { ...owner, permissions: { ...owner.permissions } };
      if (field === 'authenticated') session.authenticated = 'true';
      if (field === 'canAdminister' || field === 'canOperate') session.permissions[field] = 'true';
      if (field === 'missingPermissions') session.permissions = undefined;
      f.c.authSession = session;
      expect(() => f.c.canManageOwnerControls).not.toThrow();
      expect(() => f.c.canRequestSelection).not.toThrow();
      if (field !== 'canOperate') expect(f.c.canManageOwnerControls).toBeFalse();
      if (field !== 'canAdminister') expect(f.c.canRequestSelection).toBeFalse();
    });
  }

  for (const operation of operations.filter(operation => operation.mutation)) {
    it(`${operation.method} retains safe unconfirmed-outcome copy for asynchronous server failures`, () => {
      const f = fixture(); const pending = new Subject<any>();
      f.service[operation.method].and.returnValue(pending);
      operation.start(f.c);
      pending.error(new HttpErrorResponse({ status: 400, error: 'PRIVATE_PAYLOAD_CANARY' }));
      expect(f.notification.error).toHaveBeenCalledTimes(1);
      expect(JSON.stringify(f.notification.error.calls.allArgs())).not.toContain('PRIVATE_PAYLOAD_CANARY');
      expect(f.notification.error.calls.mostRecent().args[1]).toMatch(/unconfirmed|could not confirm/);
      expect(f.notification.success).not.toHaveBeenCalled();
      expect(busy(f.c)).toBeFalse();
    });
  }

  it('does not let an old owner refresh restore authority after a denied write', () => {
    const f = fixture(); const write = new Subject<any>(); const session = new Subject<any>();
    f.c.selectedFramework = framework as any;
    f.service['updatePreference'].and.returnValue(write);
    f.c.savePreference();
    f.service['session'].and.returnValue(session);
    f.c.refresh();
    expect(session.observed).toBeTrue();
    write.error(new HttpErrorResponse({ status: 403 }));
    expect(session.observed).toBeFalse();
    session.next(owner);
    expect(f.c.canManageOwnerControls).toBeFalse();
    expect(f.c.canRequestSelection).toBeFalse();
    f.c.savePreference();
    expect(f.service['updatePreference']).toHaveBeenCalledTimes(1);
  });

  for (const returnToFirst of [false, true]) {
    it(`does not replace a newer inspector with an old preference receipt, return=${returnToFirst}`, () => {
      const f = fixture(); const write = new Subject<any>();
      f.service['updatePreference'].and.returnValue(write);
      f.c.openFrameworkById(framework.id);
      f.c.savePreference();
      const newer = { ...framework, id: returnToFirst ? framework.id : 'other-framework', name: 'Newer inspection' };
      f.service['framework'].and.returnValue(of(newer));
      f.c.openFrameworkById(newer.id);
      write.next({ ...framework, name: 'Old write acknowledgement' });
      expect(f.c.selectedFramework).toBe(newer as any);
      expect(f.c.preferenceSaving).toBeFalse();
    });
  }

  it('does not steal focus with a delayed inspector close after destruction', fakeAsync(() => {
    const f = fixture(); const trigger = document.createElement('button'); const next = document.createElement('button');
    document.body.append(trigger, next);
    try {
      trigger.focus(); f.c.openFrameworkById(framework.id); f.c.closeInspector();
      f.c.ngOnDestroy(); next.focus(); tick();
      expect(document.activeElement).toBe(next);
    } finally { trigger.remove(); next.remove(); }
  }));

  it('does not bind a foreign framework response to the current inspector', () => {
    const f = fixture();
    f.service['framework'].and.returnValue(of({ ...framework, id: 'foreign-framework' }));
    f.c.openFrameworkById(framework.id);
    expect(f.c.selectedFramework).toBeUndefined();
    expect(f.c.inspectorError).toContain('unavailable');
    expect(f.notification.error).toHaveBeenCalledTimes(1);
  });

  it('does not announce or apply a preference receipt for another framework', () => {
    const f = fixture(); f.c.selectedFramework = framework as any; f.c.frameworks = [framework as any];
    f.service['updatePreference'].and.returnValue(of({ ...framework, id: 'foreign-framework' }));
    f.c.savePreference();
    expect(f.notification.success).not.toHaveBeenCalled();
    expect(f.notification.error).toHaveBeenCalledTimes(1);
    expect(f.c.selectedFramework).toBe(framework as any);
    expect(f.c.frameworks).toEqual([framework as any]);
  });
});
