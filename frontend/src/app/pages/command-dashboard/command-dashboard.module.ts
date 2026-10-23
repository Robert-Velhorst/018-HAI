import { CommonModule } from '@angular/common';
import { NgModule } from '@angular/core';
import { ReactiveFormsModule } from '@angular/forms';
import { RouterModule, Routes } from '@angular/router';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzEmptyModule } from 'ng-zorro-antd/empty';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzLayoutModule } from 'ng-zorro-antd/layout';
import { NzListModule } from 'ng-zorro-antd/list';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { CommandDashboardComponent } from './command-dashboard.component';
import { MEMORY_ENGINE_SERVICE_TOKEN } from '../../services/memory-engine/memory-engine.service.token';
import { MemoryEngineService } from '../../services/memory-engine/memory-engine.service';
import { ControlRoomModule } from '../../control-room/control-room.module';

const routes: Routes = [{ path: '', component: CommandDashboardComponent }];

@NgModule({
  declarations: [CommandDashboardComponent],
  imports: [
    CommonModule,
    ReactiveFormsModule,
    RouterModule.forChild(routes),
    ControlRoomModule,
    NzAlertModule,
    NzButtonModule,
    NzCardModule,
    NzEmptyModule,
    NzIconModule,
    NzInputModule,
    NzLayoutModule,
    NzListModule,
    NzModalModule,
    NzTableModule,
    NzTagModule,
  ],
  providers: [{ provide: MEMORY_ENGINE_SERVICE_TOKEN, useClass: MemoryEngineService }],
})
export class CommandDashboardModule {}
