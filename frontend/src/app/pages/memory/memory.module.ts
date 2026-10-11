import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule, ReactiveFormsModule } from '@angular/forms';
import { RouterModule, Routes } from '@angular/router';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzCheckboxModule } from 'ng-zorro-antd/checkbox';
import { NzEmptyModule } from 'ng-zorro-antd/empty';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzLayoutModule } from 'ng-zorro-antd/layout';
import { NzPaginationModule } from 'ng-zorro-antd/pagination';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { MemoryComponent } from './memory.component';
import { CONTEXT_MEMORY_SERVICE_TOKEN } from '../../services/context-memory/context-memory.service.token';
import { ContextMemoryService } from '../../services/context-memory/context-memory.service';
import { MEMORY_ENGINE_SERVICE_TOKEN } from '../../services/memory-engine/memory-engine.service.token';
import { MemoryEngineService } from '../../services/memory-engine/memory-engine.service';

const routes: Routes = [{ path: '', component: MemoryComponent }];

@NgModule({
  declarations: [MemoryComponent],
  imports: [
    CommonModule,
    RouterModule.forChild(routes),
    FormsModule,
    ReactiveFormsModule,
    NzButtonModule,
    NzCardModule,
    NzCheckboxModule,
    NzEmptyModule,
    NzFormModule,
    NzIconModule,
    NzInputModule,
    NzLayoutModule,
    NzPaginationModule,
    NzSelectModule,
    NzTableModule,
    NzTagModule,
  ],
  providers: [
    { provide: CONTEXT_MEMORY_SERVICE_TOKEN, useClass: ContextMemoryService },
    { provide: MEMORY_ENGINE_SERVICE_TOKEN, useClass: MemoryEngineService },
  ],
})
export class MemoryModule {}
