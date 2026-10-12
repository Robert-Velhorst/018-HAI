import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule, ReactiveFormsModule } from '@angular/forms';
import { RouterModule, Routes } from '@angular/router';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzCheckboxModule } from 'ng-zorro-antd/checkbox';
import { NzEmptyModule } from 'ng-zorro-antd/empty';
import { NzDrawerModule } from 'ng-zorro-antd/drawer';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzLayoutModule } from 'ng-zorro-antd/layout';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzTableModule } from 'ng-zorro-antd/table';
import { WorkflowEngineComponent } from './workflow-engine.component';
import { WORKFLOW_SERVICE_TOKEN } from '../../services/workflow/workflow.service.token';
import { WorkflowService } from '../../services/workflow/workflow.service';
import { ControlRoomModule } from '../../control-room/control-room.module';

const routes: Routes = [{ path: '', component: WorkflowEngineComponent }];

@NgModule({
  declarations: [WorkflowEngineComponent],
  imports: [
    CommonModule,
    RouterModule.forChild(routes),
    FormsModule,
    ReactiveFormsModule,
    NzButtonModule,
    NzCardModule,
    NzCheckboxModule,
    NzEmptyModule,
    NzDrawerModule,
    NzFormModule,
    NzIconModule,
    NzInputModule,
    NzLayoutModule,
    NzModalModule,
    NzSelectModule,
    NzTableModule,
    ControlRoomModule,
  ],
  providers: [{ provide: WORKFLOW_SERVICE_TOKEN, useClass: WorkflowService }],
})
export class WorkflowEngineModule {}
