import { CommonModule } from '@angular/common'
import { NgModule } from '@angular/core'
import { RouterModule, Routes } from '@angular/router'
import { NzButtonModule } from 'ng-zorro-antd/button'
import { NzDrawerModule } from 'ng-zorro-antd/drawer'
import { NzEmptyModule } from 'ng-zorro-antd/empty'
import { NzIconModule } from 'ng-zorro-antd/icon'
import { NzModalModule } from 'ng-zorro-antd/modal'
import { NzTableModule } from 'ng-zorro-antd/table'
import { NzTagModule } from 'ng-zorro-antd/tag'
import { NzTimelineModule } from 'ng-zorro-antd/timeline'
import { BackgroundOperationsComponent } from './background-operations.component'
import { ControlRoomModule } from '../../control-room/control-room.module'

const routes: Routes = [{ path: '', component: BackgroundOperationsComponent }]

@NgModule({
  declarations: [BackgroundOperationsComponent],
  imports: [
    CommonModule,
    RouterModule.forChild(routes),
    NzButtonModule,
    NzDrawerModule,
    NzEmptyModule,
    NzIconModule,
    NzModalModule,
    NzTableModule,
    NzTagModule,
    NzTimelineModule,
    ControlRoomModule,
  ],
})
export class BackgroundOperationsModule {}
