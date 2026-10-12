import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';
import { ReactiveFormsModule } from '@angular/forms';
import { RouterModule, Routes } from '@angular/router';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzEmptyModule } from 'ng-zorro-antd/empty';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzLayoutModule } from 'ng-zorro-antd/layout';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { TaskBlueprintComponent } from './task-blueprint.component';
import { TASK_PLAN_SERVICE_TOKEN } from '../../services/task-plan/task-plan.service.token';
import { TaskPlanService } from '../../services/task-plan/task-plan.service';
import { ControlRoomModule } from '../../control-room/control-room.module';

const routes: Routes = [{ path: '', component: TaskBlueprintComponent }];

@NgModule({
  declarations: [TaskBlueprintComponent],
  imports: [
    CommonModule,
    RouterModule.forChild(routes),
    ReactiveFormsModule,
    ControlRoomModule,
    NzButtonModule,
    NzCardModule,
    NzEmptyModule,
    NzFormModule,
    NzIconModule,
    NzInputModule,
    NzLayoutModule,
    NzModalModule,
    NzTableModule,
    NzTagModule,
  ],
  providers: [{ provide: TASK_PLAN_SERVICE_TOKEN, useClass: TaskPlanService }],
})
export class TaskBlueprintModule {}
