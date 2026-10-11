import { of } from 'rxjs';
import { AmbientService } from './ambient.service';

describe('AmbientService overview projections', () => {
  it('requests the summary projection explicitly for the Control Center', (done) => {
    const http = {
      get: jasmine.createSpy('get').and.returnValue(of({ needs: [], opportunities: [], scans: [], warnings: [] })),
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
