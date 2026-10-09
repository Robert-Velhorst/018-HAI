import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterModule, Routes } from '@angular/router';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { ControlRoomModule } from '../../control-room/control-room.module';
import { SystemStatusComponent } from './system-status.component';
import { OpenclawMaintenanceComponent } from '../../components/openclaw-maintenance/openclaw-maintenance.component';
import { SYSTEM_STATUS_SERVICE_TOKEN } from '../../services/system-status/system-status.service.token';
import { SystemStatusService } from '../../services/system-status/system-status.service';

const routes: Routes = [{ path: '', component: SystemStatusComponent }];

@NgModule({
  declarations: [SystemStatusComponent],
  imports: [
    OpenclawMaintenanceComponent,
    CommonModule,
    ControlRoomModule,
    RouterModule.forChild(routes),
    NzButtonModule,
    NzCardModule,
    NzIconModule,
    NzTagModule,
  ],
  providers: [
    { provide: SYSTEM_STATUS_SERVICE_TOKEN, useClass: SystemStatusService },
  ],
})
export class SystemStatusModule {}
