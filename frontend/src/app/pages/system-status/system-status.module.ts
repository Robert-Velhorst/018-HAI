import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterModule, Routes } from '@angular/router';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { SystemStatusComponent } from './system-status.component';
import { SYSTEM_STATUS_SERVICE_TOKEN } from '../../services/system-status/system-status.service.token';
import { SystemStatusService } from '../../services/system-status/system-status.service';
import { ControlRoomModule } from '../../control-room/control-room.module';

const routes: Routes = [{ path: '', component: SystemStatusComponent }];

@NgModule({
  declarations: [SystemStatusComponent],
  imports: [
    CommonModule,
    RouterModule.forChild(routes),
    NzButtonModule,
    NzCardModule,
    NzIconModule,
    ControlRoomModule,
  ],
  providers: [
    { provide: SYSTEM_STATUS_SERVICE_TOKEN, useClass: SystemStatusService },
  ],
})
export class SystemStatusModule {}
