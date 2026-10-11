import { of } from 'rxjs';
import { ConnectedSourceService } from './connected-source.service';

describe('ConnectedSourceService', () => {
  it('requests the lightweight owner source overview for the current project', (done) => {
    const http = {
      get: jasmine.createSpy('get').and.returnValue(of({})),
    };
    const service = new ConnectedSourceService(http as any);

    service.overview('018-HAI', false).subscribe(() => {
      const [url, options] = http.get.calls.mostRecent().args;
      expect(url).toBe('/api/v1/sources/overview');
      expect(options.params.get('projectKey')).toBe('018-HAI');
      expect(options.params.get('includeArchived')).toBe('false');
      done();
    });
  });

  it('requests a bounded source audit history when a limit is provided', (done) => {
    const http = {
      get: jasmine.createSpy('get').and.returnValue(of([])),
    };
    const service = new ConnectedSourceService(http as any);

    service.auditLogs(undefined, 8).subscribe(() => {
      const [url, options] = http.get.calls.mostRecent().args;
      expect(url).toBe('/api/v1/sources/audit-logs');
      expect(options.params.get('sourceId')).toBeNull();
      expect(options.params.get('limit')).toBe('8');
      done();
    });
  });

  it('requests bounded extraction and sync-job histories when limits are provided', () => {
    const http = {
      get: jasmine.createSpy('get').and.returnValue(of([])),
    };
    const service = new ConnectedSourceService(http as any);

    service.extractions('018-HAI', false, 8).subscribe();
    let [url, options] = http.get.calls.mostRecent().args;
    expect(url).toBe('/api/v1/sources/extractions');
    expect(options.params.get('projectKey')).toBe('018-HAI');
    expect(options.params.get('includeArchived')).toBe('false');
    expect(options.params.get('limit')).toBe('8');

    service.syncJobs(undefined, 6).subscribe();
    [url, options] = http.get.calls.mostRecent().args;
    expect(url).toBe('/api/v1/sources/sync-jobs');
    expect(options.params.get('sourceId')).toBeNull();
    expect(options.params.get('limit')).toBe('6');
  });
});
