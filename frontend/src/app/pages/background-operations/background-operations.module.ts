import { CommonModule } from '@angular/common'
import { NgModule } from '@angular/core'
import { FormsModule } from '@angular/forms'
import { RouterModule, Routes } from '@angular/router'
import { NzButtonModule } from 'ng-zorro-antd/button'
import { NzAlertModule } from 'ng-zorro-antd/alert'
import { NzCardModule } from 'ng-zorro-antd/card'
import { NzDrawerModule } from 'ng-zorro-antd/drawer'
import { NzEmptyModule } from 'ng-zorro-antd/empty'
import { NzIconModule } from 'ng-zorro-antd/icon'
import { NzLayoutModule } from 'ng-zorro-antd/layout'
import { NzRadioModule } from 'ng-zorro-antd/radio'
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
    FormsModule,
    RouterModule.forChild(routes),
    NzAlertModule,
    NzButtonModule,
    NzCardModule,
    NzDrawerModule,
    NzEmptyModule,
    NzIconModule,
    NzLayoutModule,
    NzRadioModule,
    NzTableModule,
    NzTagModule,
    NzTimelineModule,
    ControlRoomModule,
  ],
})
export class BackgroundOperationsModule {}
