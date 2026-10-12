import { TestBed } from '@angular/core/testing';
import { HttpClientTestingModule, HttpTestingController } from '@angular/common/http/testing';
import { AgentRuntimeService } from './agent-runtime.service';

describe('AgentRuntimeService', () => {
  let service: AgentRuntimeService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [HttpClientTestingModule] });
    service = TestBed.inject(AgentRuntimeService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('loads the runtime overview in one request', () => {
    service.overview().subscribe((overview) => {
      expect(overview.runtimes[0].id).toBe('deepseek-harness');
      expect(overview.health[0].runtimeId).toBe('deepseek-harness');
    });

    const request = http.expectOne('/api/v1/agent-runtimes/overview');
    expect(request.request.method).toBe('GET');
    request.flush({
      runtimes: [{ id: 'deepseek-harness' }],
      health: [{ runtimeId: 'deepseek-harness' }],
    });
  });

  it('prepares then applies an exact OpenClaw ecosystem path authorization', () => {
    const authorization = {
      idempotencyKey: 'openclaw:one',
      taskId: 'openclaw-task-one',
      approvalSourceId: 'opscontrol-owner:proof',
      approvalBindingDigest: 'a'.repeat(64),
    };

    service.prepareOpenClawEcosystemPath('C:/OpenClaw/openclaw-main.zip').subscribe((value) => {
      expect(value).toEqual(authorization);
    });
    const prepare = http.expectOne('/api/v1/agent-runtimes/openclaw/ecosystem/approval/set-path');
    expect(prepare.request.method).toBe('POST');
    expect(prepare.request.body).toEqual({ ecosystemPath: 'C:/OpenClaw/openclaw-main.zip' });
    prepare.flush(authorization);

    service.setOpenClawEcosystemPath('C:/OpenClaw/openclaw-main.zip', authorization).subscribe();
    const apply = http.expectOne('/api/v1/agent-runtimes/openclaw/ecosystem');
    expect(apply.request.method).toBe('PATCH');
    expect(apply.request.body).toEqual({
      ecosystemPath: 'C:/OpenClaw/openclaw-main.zip',
      ...authorization,
    });
    apply.flush({ id: 'openclaw' });
  });

  it('keeps OpenClaw upload authorization in the multipart request only', () => {
    const authorization = {
      idempotencyKey: 'openclaw:upload',
      taskId: 'openclaw-upload-task',
      approvalSourceId: 'opscontrol-owner:proof',
      approvalBindingDigest: 'b'.repeat(64),
    };
    const file = new File(['zip-data'], 'openclaw-main.zip', { type: 'application/zip' });

    service.prepareOpenClawEcosystemUpload(file).subscribe();
    const prepare = http.expectOne('/api/v1/agent-runtimes/openclaw/ecosystem/approval/upload');
    expect(prepare.request.method).toBe('POST');
    const preparedFile = (prepare.request.body as FormData).get('ecosystem') as File;
    expect(preparedFile.name).toBe(file.name);
    expect(preparedFile.size).toBe(file.size);
    prepare.flush(authorization);

    service.uploadOpenClawEcosystem(file, authorization).subscribe();
    const apply = http.expectOne('/api/v1/agent-runtimes/openclaw/ecosystem/upload');
    expect(apply.request.method).toBe('POST');
    const body = apply.request.body as FormData;
    const appliedFile = body.get('ecosystem') as File;
    expect(appliedFile.name).toBe(file.name);
    expect(appliedFile.size).toBe(file.size);
    expect(body.get('approvalBindingDigest')).toBe(authorization.approvalBindingDigest);
    expect(body.get('taskId')).toBe(authorization.taskId);
    apply.flush({ id: 'openclaw' });
  });

  it('prepares and applies an owner-approved OpenClaw archive rollback', () => {
    const authorization = {
      idempotencyKey: 'openclaw:rollback',
      taskId: 'openclaw-rollback-task',
      approvalSourceId: 'opscontrol-owner:proof',
      approvalBindingDigest: 'c'.repeat(64),
    };

    service.prepareOpenClawEcosystemRollback().subscribe((value) => {
      expect(value).toEqual(authorization);
    });
    const prepare = http.expectOne('/api/v1/agent-runtimes/openclaw/ecosystem/approval/rollback');
    expect(prepare.request.method).toBe('POST');
    expect(prepare.request.body).toBeNull();
    prepare.flush(authorization);

    service.rollbackOpenClawEcosystem(authorization).subscribe((runtime) => {
      expect(runtime.id).toBe('openclaw');
      expect(runtime.ecosystemRollbackAvailable).toBeTrue();
    });
    const apply = http.expectOne('/api/v1/agent-runtimes/openclaw/ecosystem/rollback');
    expect(apply.request.method).toBe('POST');
    expect(apply.request.body).toEqual(authorization);
    apply.flush({ id: 'openclaw', ecosystemRollbackAvailable: true });
  });
});
