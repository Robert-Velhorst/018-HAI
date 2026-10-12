import { CommonModule } from '@angular/common'
import { NgModule } from '@angular/core'
import { RouterModule, Routes } from '@angular/router'
import { NzButtonModule } from 'ng-zorro-antd/button'
import { NzIconModule } from 'ng-zorro-antd/icon'
import { ControlRoomModule } from '../../control-room/control-room.module'
import { SkillsComponent } from './skills.component'

const routes: Routes = [{ path: '', component: SkillsComponent }]

@NgModule({
  declarations: [SkillsComponent],
  imports: [CommonModule, RouterModule.forChild(routes), NzButtonModule, NzIconModule, ControlRoomModule],
})
export class SkillsModule {}
