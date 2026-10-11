import { CommonModule } from '@angular/common'
import { NgModule } from '@angular/core'
import { RouterModule, Routes } from '@angular/router'
import { NzButtonModule } from 'ng-zorro-antd/button'
import { NzCardModule } from 'ng-zorro-antd/card'
import { NzIconModule } from 'ng-zorro-antd/icon'
import { NzTagModule } from 'ng-zorro-antd/tag'
import { ControlRoomModule } from '../../control-room/control-room.module'
import { RuntimeLabComponent } from './runtime-lab.component'
import { OpenclawMaintenanceComponent } from '../../components/openclaw-maintenance/openclaw-maintenance.component'

const routes: Routes = [{ path: '', component: RuntimeLabComponent }]

@NgModule({
  declarations: [RuntimeLabComponent],
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
})
export class RuntimeLabModule {}
