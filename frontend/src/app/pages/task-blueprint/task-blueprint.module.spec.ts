import { provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { NoopAnimationsModule } from '@angular/platform-browser/animations';
import { NzModalService } from 'ng-zorro-antd/modal';
import { TaskBlueprintModule } from './task-blueprint.module';
import { provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';

describe('TaskBlueprintModule', () => {
  it('provides the modal dependency required by the lazy route component', () => {
    TestBed.configureTestingModule({
    imports: [NoopAnimationsModule, TaskBlueprintModule],
    providers: [provideHttpClient(withInterceptorsFromDi()), provideHttpClientTesting()]
});

    expect(TestBed.inject(NzModalService)).toBeTruthy();
  });
});
