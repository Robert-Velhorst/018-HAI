import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { MCPPreflightService } from './mcp-preflight.service';
import { provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';

describe('MCPPreflightService', () => {
  let service: MCPPreflightService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [], providers: [provideHttpClient(withInterceptorsFromDi()), provideHttpClientTesting()] });
    service = TestBed.inject(MCPPreflightService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('loads the configured read-only MCP readiness boundary', () => {
    service.overview().subscribe((overview) => expect(overview.scope).toContain('Read-only'));

    const request = http.expectOne('/api/v1/mcp-preflight/overview');
    expect(request.request.method).toBe('GET');
    request.flush({ enabled: false, scope: 'Read-only MCP preflight.', servers: [] });
  });

  it('runs preflight only for the selected configured server', () => {
    service.run('github-local').subscribe((result) => expect(result.status).toBe('ready'));

    const request = http.expectOne('/api/v1/mcp-preflight/github-local/run');
    expect(request.request.method).toBe('POST');
    expect(request.request.body).toEqual({});
    request.flush({
      id: 'mcp-preflight-1',
      serverId: 'github-local',
      status: 'ready',
      detail: 'MCP handshake and tool listing completed.',
      toolCount: 2,
      truncated: false,
      durationMs: 12,
      checkedAt: '2026-07-20T00:00:00Z',
    });
  });
});
