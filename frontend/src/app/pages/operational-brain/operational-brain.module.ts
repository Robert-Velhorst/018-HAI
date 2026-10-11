import { CommonModule } from '@angular/common'
import { NgModule } from '@angular/core'
import { FormsModule } from '@angular/forms'
import { RouterModule, Routes } from '@angular/router'
import { NzButtonModule } from 'ng-zorro-antd/button'
import { NzDrawerModule } from 'ng-zorro-antd/drawer'
import { NzIconModule } from 'ng-zorro-antd/icon'
import { NzSpinModule } from 'ng-zorro-antd/spin'
import { ControlRoomModule } from '../../control-room/control-room.module'
import { OperationalBrainComponent } from './operational-brain.component'

const routes: Routes = [{ path: '', component: OperationalBrainComponent }]

@NgModule({
  declarations: [OperationalBrainComponent],
  imports: [
    CommonModule,
    FormsModule,
    RouterModule.forChild(routes),
    ControlRoomModule,
    NzButtonModule,
    NzDrawerModule,
    NzIconModule,
    NzSpinModule,
  ],
})
export class OperationalBrainModule {}
