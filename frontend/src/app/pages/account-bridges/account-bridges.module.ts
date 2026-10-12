import { CommonModule } from '@angular/common'
import { NgModule } from '@angular/core'
import { RouterModule, Routes } from '@angular/router'
import { NzAlertModule } from 'ng-zorro-antd/alert'
import { NzButtonModule } from 'ng-zorro-antd/button'
import { NzCardModule } from 'ng-zorro-antd/card'
import { NzEmptyModule } from 'ng-zorro-antd/empty'
import { NzIconModule } from 'ng-zorro-antd/icon'
import { NzTableModule } from 'ng-zorro-antd/table'
import { NzTagModule } from 'ng-zorro-antd/tag'
import { AccountBridgesComponent } from './account-bridges.component'

const routes: Routes = [{ path: '', component: AccountBridgesComponent }]

@NgModule({
  declarations: [AccountBridgesComponent],
  imports: [
    CommonModule,
    RouterModule.forChild(routes),
    NzAlertModule,
    NzButtonModule,
    NzCardModule,
    NzEmptyModule,
    NzIconModule,
    NzTableModule,
    NzTagModule,
  ],
})
export class AccountBridgesModule {}
