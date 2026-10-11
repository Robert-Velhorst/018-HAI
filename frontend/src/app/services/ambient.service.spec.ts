import { of } from 'rxjs';
import { AmbientService } from './ambient.service';

describe('AmbientService overview projections', () => {
  it('requests the summary projection explicitly for lightweight consumers', (done) => {
    const http = {
      get: jasmine.createSpy('get').and.returnValue(of({
        generatedAt: '2026-08-15T00:00:00Z',
        policy: {},
        needs: [],
        opportunities: [],
        scans: [],
        warnings: [],
      })),
    };
    const service = new AmbientService(http as any);

    service.overview('summary').subscribe(() => {
      const [url, options] = http.get.calls.mostRecent().args;
      expect(url).toBe('/api/v1/ambient/overview');
      expect(options.params.get('view')).toBe('summary');
      done();
    });
  });
});
