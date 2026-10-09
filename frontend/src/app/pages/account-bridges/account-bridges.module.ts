import { CommonModule } from '@angular/common'
import { NgModule } from '@angular/core'
import { RouterModule, Routes } from '@angular/router'
import { NzButtonModule } from 'ng-zorro-antd/button'
import { NzCardModule } from 'ng-zorro-antd/card'
import { NzEmptyModule } from 'ng-zorro-antd/empty'
import { NzTableModule } from 'ng-zorro-antd/table'
import { NzTagModule } from 'ng-zorro-antd/tag'
import { ControlRoomModule } from '../../control-room/control-room.module'
import { AccountBridgesComponent } from './account-bridges.component'

const routes: Routes = [{ path: '', component: AccountBridgesComponent }]

@NgModule({
  declarations: [AccountBridgesComponent],
  imports: [
    CommonModule,
    RouterModule.forChild(routes),
    ControlRoomModule,
    NzButtonModule,
    NzCardModule,
    NzEmptyModule,
    NzTableModule,
    NzTagModule,
  ],
})
export class AccountBridgesModule {}
