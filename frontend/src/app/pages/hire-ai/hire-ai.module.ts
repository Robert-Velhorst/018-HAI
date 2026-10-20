import { CommonModule } from '@angular/common'
import { NgModule } from '@angular/core'
import { ReactiveFormsModule } from '@angular/forms'
import { RouterModule, Routes } from '@angular/router'
import { NzAlertModule } from 'ng-zorro-antd/alert'
import { NzButtonModule } from 'ng-zorro-antd/button'
import { NzCheckboxModule } from 'ng-zorro-antd/checkbox'
import { NzInputModule } from 'ng-zorro-antd/input'
import { NzSelectModule } from 'ng-zorro-antd/select'
import { NzSpinModule } from 'ng-zorro-antd/spin'
import { NzTagModule } from 'ng-zorro-antd/tag'
import { ControlRoomModule } from '../../control-room/control-room.module'
import { HireAIComponent } from './hire-ai.component'

const routes: Routes = [{ path: '', component: HireAIComponent }]

@NgModule({
  declarations: [HireAIComponent],
  imports: [CommonModule, ReactiveFormsModule, RouterModule.forChild(routes), ControlRoomModule, NzAlertModule, NzButtonModule, NzCheckboxModule, NzInputModule, NzSelectModule, NzSpinModule, NzTagModule],
})
export class HireAIModule {}
