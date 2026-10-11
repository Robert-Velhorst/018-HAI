import { TestBed } from '@angular/core/testing';
import { HttpClientTestingModule, HttpTestingController } from '@angular/common/http/testing';
import { BackgroundOperationsService } from './background-operations.service';
import { HttpErrorResponse } from '@angular/common/http';

describe('BackgroundOperationsService', () => {
  let service: BackgroundOperationsService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [HttpClientTestingModule] });
    service = TestBed.inject(BackgroundOperationsService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('loads the ledger dashboard and filtered operations in one request', () => {
    service.overview({ status: 'blocked' }).subscribe((overview) => {
      expect(overview.dashboard.blocked).toBe(1);
      expect(overview.operations[0].status).toBe('blocked');
    });

    const request = http.expectOne('/api/v1/operations/overview?status=blocked');
    expect(request.request.method).toBe('GET');
    request.flush({ dashboard: { blocked: 1 }, operations: [{ status: 'blocked' }] });
  });

  it('keeps a run 409 error-plus-receipt envelope on the error channel with no replay', () => {
    const envelope = {
      operationId: 'id', operation: { id: 'id', status: 'running', verificationStatus: 'pending' },
      verified: false, failed: false, interrupted: false, reconciliationRequired: true, outcomeRecorded: false,
      receipt: { output: { artifactHash: 'receipt-hash', boundedOutput: 'retained output' } },
    };
    service.run('id').subscribe({
      next: () => fail('An HTTP failure must not become verified success.'),
      error: (error: HttpErrorResponse) => {
        expect(error.status).toBe(409);
        expect(error.error).toEqual(envelope);
      },
    });
    const request = http.expectOne('/api/v1/operations/id/run');
    expect(request.request.method).toBe('POST');
    request.flush(envelope, { status: 409, statusText: 'Conflict' });
    http.expectNone('/api/v1/operations/id/run');
  });

  it('preserves the additive receipt and recording fields on successful run responses', () => {
    const envelope = {
      operationId: 'id', operation: { id: 'id', status: 'completed', verificationStatus: 'passed' },
      verified: true, failed: false, interrupted: false, reconciliationRequired: false, outcomeRecorded: true,
      receipt: { output: { artifactHash: 'receipt-hash' } },
    };
    service.run('id').subscribe((result) => expect(result).toEqual(envelope));
    http.expectOne('/api/v1/operations/id/run').flush(envelope);
  });

  it('validates and returns a source approval preview bound to the requested operation', () => {
    const digest = 'a'.repeat(64)
    let actual: unknown
    service.approvalPreview('source-op').subscribe((preview) => actual = preview)

    const request = http.expectOne('/api/v1/operations/source-op/approval-preview')
    expect(request.request.method).toBe('GET')
    request.flush({
      operation: {
        id: 'source-op', version: 7, status: 'awaiting_approval', requiresApproval: true,
        updatedAt: '2026-10-09T10:00:00Z', sourceType: 'email', sourceIdentityHash: 'identity',
      },
      revision: { operationId: 'source-op', version: 7, revisionDigest: digest },
    })

    expect(actual).toEqual(jasmine.objectContaining({
      operation: jasmine.objectContaining({ id: 'source-op', version: 7 }),
      revision: { operationId: 'source-op', version: 7, revisionDigest: digest },
    }))
  });

  it('rejects malformed or cross-bound source approval previews', () => {
    let error: unknown
    service.approvalPreview('source-op').subscribe({ error: (value) => error = value })
    http.expectOne('/api/v1/operations/source-op/approval-preview').flush({
      operation: {
        id: 'another-op', version: 7, status: 'awaiting_approval', requiresApproval: true,
        updatedAt: '2026-10-09T10:00:00Z', sourceId: 'source-id',
      },
      revision: { operationId: 'source-op', version: 7, revisionDigest: 'not-a-digest' },
    })
    expect(error instanceof Error).toBeTrue()
    expect((error as Error).message).toContain('invalid or did not match')
  });

  it('posts an exact revision binding for source approvals and keeps legacy approvals empty', () => {
    const binding = { expectedVersion: 9, revisionDigest: 'b'.repeat(64) }
    service.approve('source-op', binding).subscribe()
    const sourceRequest = http.expectOne('/api/v1/operations/source-op/approve')
    expect(sourceRequest.request.method).toBe('POST')
    expect(sourceRequest.request.body).toEqual(binding)
    sourceRequest.flush({ id: 'source-op' })

    service.approve('legacy-op').subscribe()
    const legacyRequest = http.expectOne('/api/v1/operations/legacy-op/approve')
    expect(legacyRequest.request.body).toEqual({})
    legacyRequest.flush({ id: 'legacy-op' })
  });
});
