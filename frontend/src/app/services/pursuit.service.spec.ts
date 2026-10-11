import { PursuitService } from './pursuit.service';
import { of } from 'rxjs';

describe('PursuitService response normalization', () => {
  it('loads compact pursuit list records by default', (done) => {
    const http = {
      get: jasmine.createSpy('get').and.returnValue(of([])),
    };
    const service = new PursuitService(http as any);

    service.list().subscribe(() => {
      const [url, options] = http.get.calls.mostRecent().args;
      expect(url).toBe('/api/v1/pursuits/');
      expect(options.params.get('includeArchived')).toBe('false');
      expect(options.params.get('view')).toBe('summary');
      done();
    });
  });

  it('loads the bounded pursuit dashboard view and normalizes empty queues', (done) => {
    const http = {
      get: jasmine.createSpy('get').and.returnValue(of({ counts: { active: 2 } })),
    };
    const service = new PursuitService(http as any);

    service.dashboard().subscribe((dashboard) => {
      expect(dashboard.counts['active']).toBe(2);
      expect(dashboard.decisionQueue).toEqual([]);
      expect(dashboard.needsRobert).toEqual([]);
      const [url, options] = http.get.calls.mostRecent().args;
      expect(url).toBe('/api/v1/pursuits/dashboard');
      expect(options.params.get('view')).toBe('summary');
      done();
    });
  });

  it('requests aggregate-only pursuit counts for lightweight consumers', (done) => {
    const http = {
      get: jasmine.createSpy('get').and.returnValue(of({ counts: { active: 2, needsRobert: 1 } })),
    };
    const service = new PursuitService(http as any);

    service.dashboard('counts').subscribe((dashboard) => {
      expect(dashboard.counts['active']).toBe(2);
      expect(dashboard.needsRobert).toEqual([]);
      const [, options] = http.get.calls.mostRecent().args;
      expect(options.params.get('view')).toBe('counts');
      done();
    });
  });

  it('submits portfolio planning to the advisory collection endpoint', (done) => {
    const response = { authority: 'advisory_only', canExecute: false, priorities: [], exclusions: [] };
    const http = {
      post: jasmine.createSpy('post').and.returnValue(of(response)),
    };
    const service = new PursuitService(http as any);
    const request = {
      planId: 'portfolio-test',
      asOf: '2026-08-04T08:00:00.000Z',
      horizonStart: '2026-08-04T08:00:00.000Z',
      horizonEnd: '2026-08-04T17:00:00.000Z',
      durationMode: 'expected' as const,
      availability: [{ start: '2026-08-04T08:00:00.000Z', end: '2026-08-04T17:00:00.000Z' }],
      pursuits: [],
      budget: {},
      approvalPolicy: { softDeadlineMiss: true },
    };

    service.planPortfolio(request).subscribe((result) => {
      expect(result).toBe(response as any);
      expect(http.post).toHaveBeenCalledOnceWith('/api/v1/pursuits/portfolio-plan', request);
      done();
    });
  });

  it('submits the exact planning request and decision digest for governed allocation acceptance', (done) => {
    const decisionDigest = 'a'.repeat(64);
    const response = {
      allocation: {
        id: 'allocation-1',
        planId: 'portfolio-test',
        requestDigest: 'b'.repeat(64),
        decisionDigest,
        status: 'accepted',
        durationMode: 'expected',
        horizonStart: '2026-08-04T08:00:00.000Z',
        horizonEnd: '2026-08-04T17:00:00.000Z',
        actor: 'owner@example.com',
        confirmation: 'ACCEPT PORTFOLIO ALLOCATION',
        recordDigest: 'c'.repeat(64),
        acceptedAt: '2026-08-04T09:00:00.000Z',
      },
      items: [],
      replayed: false,
      authority: 'allocation_only',
      canExecute: false,
    };
    const http = {
      post: jasmine.createSpy('post').and.returnValue(of(response)),
    };
    const service = new PursuitService(http as any);
    const planningRequest = {
      planId: 'portfolio-test',
      asOf: '2026-08-04T08:00:00.000Z',
      horizonStart: '2026-08-04T08:00:00.000Z',
      horizonEnd: '2026-08-04T17:00:00.000Z',
      durationMode: 'expected' as const,
      availability: [{ start: '2026-08-04T08:00:00.000Z', end: '2026-08-04T17:00:00.000Z' }],
      pursuits: [],
      budget: {},
      approvalPolicy: { softDeadlineMiss: true },
    };
    const request = {
      planningRequest,
      expectedDecisionDigest: decisionDigest,
      confirmation: 'ACCEPT PORTFOLIO ALLOCATION' as const,
    };

    service.acceptPortfolioAllocation(request).subscribe((result) => {
      expect(result).toBe(response as any);
      expect(http.post).toHaveBeenCalledOnceWith('/api/v1/pursuits/portfolio-plan/accept', request);
      expect(http.post.calls.mostRecent().args[1].planningRequest).toBe(planningRequest);
      done();
    });
  });

  it('loads bounded owner portfolio allocation history from the read-only endpoint', (done) => {
    const response: any[] = [];
    const http = {
      get: jasmine.createSpy('get').and.returnValue(of(response)),
    };
    const service = new PursuitService(http as any);

    service.portfolioAllocations(25).subscribe((result) => {
      expect(result).toBe(response);
      expect(http.get).toHaveBeenCalledTimes(1);
      const [url, options] = http.get.calls.mostRecent().args;
      expect(url).toBe('/api/v1/pursuits/portfolio-allocations');
      expect(options.params.get('limit')).toBe('25');
      done();
    });
  });

  it('prepares proposal-only execution evidence for one immutable allocation', (done) => {
    const response = { authority: 'proposal_only', canExecute: false, items: [] };
    const http = {
      post: jasmine.createSpy('post').and.returnValue(of(response)),
    };
    const service = new PursuitService(http as any);
    const request = {
      expectedAllocationDigest: 'a'.repeat(64),
      confirmation: 'PREPARE EXECUTION PROPOSALS' as const,
    };

    service.preparePortfolioExecutionProposals('allocation/with spaces', request).subscribe((result) => {
      expect(result).toBe(response as any);
      expect(http.post).toHaveBeenCalledOnceWith(
        '/api/v1/pursuits/portfolio-allocations/allocation%2Fwith%20spaces/execution-proposals',
        request,
      );
      done();
    });
  });

  it('restores the latest immutable execution proposals for bounded allocations', (done) => {
    const response: any[] = [];
    const http = {
      get: jasmine.createSpy('get').and.returnValue(of(response)),
    };
    const service = new PursuitService(http as any);

    service.portfolioExecutionProposals(['allocation-1', 'allocation-2']).subscribe((result) => {
      expect(result).toBe(response);
      expect(http.get).toHaveBeenCalledTimes(1);
      const [url, options] = http.get.calls.mostRecent().args;
      expect(url).toBe('/api/v1/pursuits/portfolio-execution-proposals');
      expect(options.params.get('allocationIds')).toBe('allocation-1,allocation-2');
      done();
    });
  });

  it('records a non-executing decision for one immutable proposal item', (done) => {
    const response = { authority: 'approval_decision_only', canExecute: false, replayed: false };
    const http = {
      post: jasmine.createSpy('post').and.returnValue(of(response)),
    };
    const service = new PursuitService(http as any);
    const request = {
      expectedItemDigest: 'a'.repeat(64),
      decision: 'approved' as const,
      reason: 'The evidence is complete and the bounded proposal is suitable for separate authorization review.',
      confirmation: 'APPROVE EXECUTION PROPOSAL ITEM' as const,
    };

    service.decidePortfolioExecutionProposalItem('proposal/item with spaces', request).subscribe((result) => {
      expect(result).toBe(response as any);
      expect(http.post).toHaveBeenCalledOnceWith(
        '/api/v1/pursuits/portfolio-execution-proposal-items/proposal%2Fitem%20with%20spaces/decisions',
        request,
      );
      done();
    });
  });

  it('loads bounded immutable proposal item decision history', (done) => {
    const response = { decisions: [], authority: 'approval_decision_only', canExecute: false };
    const http = {
      get: jasmine.createSpy('get').and.returnValue(of(response)),
    };
    const service = new PursuitService(http as any);

    service.portfolioExecutionProposalDecisionHistory('proposal/item with spaces', 500).subscribe((result) => {
      expect(result).toBe(response as any);
      expect(http.get).toHaveBeenCalledTimes(1);
      const [url, options] = http.get.calls.mostRecent().args;
      expect(url).toBe('/api/v1/pursuits/portfolio-execution-proposal-items/proposal%2Fitem%20with%20spaces/decisions');
      expect(options.params.get('limit')).toBe('100');
      done();
    });
  });

  it('evaluates one exact workflow effect without exposing caller-selected execution fields', (done) => {
    const response = { authority: 'execution_authorization_only', canExecute: false };
    const http = {
      post: jasmine.createSpy('post').and.returnValue(of(response)),
    };
    const service = new PursuitService(http as any);
    const request = {
      expectedItemDigest: 'a'.repeat(64),
      expectedDecisionDigest: 'b'.repeat(64),
      confirmation: 'AUTHORIZE PORTFOLIO WORKFLOW EFFECT' as const,
    };

    service.authorizePortfolioWorkflowEffect('proposal/item with spaces', request).subscribe((result) => {
      expect(result).toBe(response as any);
      expect(http.post).toHaveBeenCalledOnceWith(
        '/api/v1/pursuits/portfolio-execution-proposal-items/proposal%2Fitem%20with%20spaces/authorize-workflow',
        request,
      );
      expect(Object.keys(http.post.calls.mostRecent().args[1])).toEqual([
        'expectedItemDigest', 'expectedDecisionDigest', 'confirmation',
      ]);
      done();
    });
  });

  it('loads read-only coordination for an encoded immutable proposal', (done) => {
    const response = { authority: 'coordination_preview_only', canExecute: false, items: [], dispatchRuns: [] };
    const http = {
      get: jasmine.createSpy('get').and.returnValue(of(response)),
    };
    const service = new PursuitService(http as any);

    service.portfolioDispatchCoordination('proposal/with spaces').subscribe((result) => {
      expect(result).toBe(response as any);
      expect(http.get).toHaveBeenCalledOnceWith(
        '/api/v1/pursuits/portfolio-execution-proposals/proposal%2Fwith%20spaces/coordination',
      );
      done();
    });
  });

  it('loads bounded read-only coordination for recovered immutable proposals', (done) => {
    const response: any[] = [];
    const http = {
      get: jasmine.createSpy('get').and.returnValue(of(response)),
    };
    const service = new PursuitService(http as any);

    service.portfolioDispatchCoordinations(['proposal-1', 'proposal-2']).subscribe((result) => {
      expect(result).toBe(response);
      expect(http.get).toHaveBeenCalledTimes(1);
      const [url, options] = http.get.calls.mostRecent().args;
      expect(url).toBe('/api/v1/pursuits/portfolio-execution-proposals/coordination');
      expect(options.params.get('proposalIds')).toBe('proposal-1,proposal-2');
      done();
    });
  });

  it('submits only the exact selected portfolio dispatch contract', (done) => {
    const response = { authority: 'portfolio_dispatch_result', canExecute: false, items: [] };
    const request = {
      expectedProposalDigest: 'a'.repeat(64),
      items: [{
        proposalItemId: 'proposal-item-1',
        expectedItemDigest: 'b'.repeat(64),
        expectedDecisionDigest: 'c'.repeat(64),
      }],
      confirmation: 'DISPATCH APPROVED PORTFOLIO WORKFLOWS' as const,
    };
    const http = {
      post: jasmine.createSpy('post').and.returnValue(of(response)),
    };
    const service = new PursuitService(http as any);

    service.dispatchPortfolioWorkflows('proposal/with spaces', request).subscribe((result) => {
      expect(result).toBe(response as any);
      expect(http.post).toHaveBeenCalledOnceWith(
        '/api/v1/pursuits/portfolio-execution-proposals/proposal%2Fwith%20spaces/dispatch',
        request,
      );
      done();
    });
  });

  it('normalizes nullable action queue members at the API boundary', () => {
    const service = new PursuitService({} as any);
    const detail = (service as any).normalizeDetail({
      pursuit: {},
      actionQueues: {
        needsRobert: null,
        vaReady: null,
        systemReady: null,
        waiting: null,
      },
    });

    expect(detail.actionQueues).toEqual({
      needsRobert: [],
      vaReady: [],
      systemReady: [],
      waiting: [],
    });
  });

  it('fails closed when an older detail response has no resource projection', () => {
    const service = new PursuitService({} as any);
    const detail = (service as any).normalizeDetail({ pursuit: {}, actionQueues: {} });

    expect(detail.resourceUsage.state).toBe('unavailable');
    expect(detail.resourceUsage.available).toBeFalse();
    expect(detail.resourceUsage.blockingReason).toContain('not available');
  });

  it('does not retain the unavailable fallback blocker for a healthy projection', () => {
    const service = new PursuitService({} as any);
    const detail = (service as any).normalizeDetail({
      pursuit: {},
      actionQueues: {},
      resourceUsage: {
        state: 'within_limits',
        available: true,
        limitsConfigured: true,
        effortLimitHours: 2,
        effortRemainingHours: 2,
        spendLimitEur: 1,
        spendRemainingEur: 1,
      },
    });

    expect(detail.resourceUsage.state).toBe('within_limits');
    expect(detail.resourceUsage.blockingReason).toBe('');
    expect(detail.resourceUsage.effortReservedHours).toBe(0);
    expect(detail.resourceUsage.spendCommittedEur).toBe(0);
		expect(detail.resourceUsage.reservations).toEqual([]);
  });

	it('releases only a confirmed orphan through the approval endpoint', (done) => {
		const response = { state: 'within_limits', reservations: [] };
		const http = {
			post: jasmine.createSpy('post').and.returnValue(of(response)),
		};
		const service = new PursuitService(http as any);

		service.releaseResourceReservation('pursuit-1', 'reservation-1', 'Worker crashed and no process remains.').subscribe((usage) => {
			expect(usage).toBe(response as any);
			expect(http.post).toHaveBeenCalledWith(
				'/api/v1/pursuits/pursuit-1/resource-reservations/reservation-1/release',
				{ confirmedOrphan: true, reason: 'Worker crashed and no process remains.' },
			);
			done();
		});
	});

  it('loads a bounded immutable resource event list', (done) => {
    const http = {
      get: jasmine.createSpy('get').and.returnValue(of({ events: null })),
    };
    const service = new PursuitService(http as any);

    service.resourceEvents('pursuit-1', 9999).subscribe((events) => {
      expect(events).toEqual([]);
      expect(http.get).toHaveBeenCalledWith('/api/v1/pursuits/pursuit-1/resource-events', jasmine.objectContaining({
        params: jasmine.anything(),
      }));
      expect(http.get.calls.mostRecent().args[1].params.get('limit')).toBe('500');
      done();
    });
  });

  it('submits only the exact receipt-bound portfolio workflow effect', (done) => {
    const response = { authority: 'workflow_effect_executed', canExecute: false };
    const request = {
      authorizationReceiptId: '31f97f38-181d-4aee-bf8f-36a3cf9d1576',
      expectedItemDigest: 'a'.repeat(64),
      expectedDecisionDigest: 'b'.repeat(64),
      confirmation: 'CREATE APPROVED PORTFOLIO WORKFLOW' as const,
    };
    const http = {
      post: jasmine.createSpy('post').and.returnValue(of(response)),
    };
    const service = new PursuitService(http as any);

    service.executePortfolioWorkflowEffect('proposal item/1', request).subscribe((result) => {
      expect(result).toBe(response as any);
      expect(http.post).toHaveBeenCalledOnceWith(
        '/api/v1/pursuits/portfolio-execution-proposal-items/proposal%20item%2F1/execute-workflow',
        request,
      );
      done();
    });
  });

  it('settles measured usage against one verified portfolio workflow', (done) => {
    const response = { authority: 'verified_accounting_only', canExecute: false, replayed: false };
    const request = {
      workflowId: 'a2288803-346a-4713-8d3b-e6da77de6a0d',
      expectedItemDigest: 'a'.repeat(64),
      actualEffortMinutes: 42,
      actualCostMicros: 125000,
      confirmation: 'SETTLE VERIFIED PORTFOLIO WORK' as const,
    };
    const http = {
      post: jasmine.createSpy('post').and.returnValue(of(response)),
    };
    const service = new PursuitService(http as any);

    service.settlePortfolioWorkflow('proposal item/1', request).subscribe((result) => {
      expect(result).toBe(response as any);
      expect(http.post).toHaveBeenCalledOnceWith(
        '/api/v1/pursuits/portfolio-execution-proposal-items/proposal%20item%2F1/settle-workflow',
        request,
      );
      done();
    });
  });
});
