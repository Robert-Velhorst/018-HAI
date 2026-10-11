import { CommonModule } from '@angular/common'
import { NgModule } from '@angular/core'
import { FormsModule } from '@angular/forms'
import { RouterModule, Routes } from '@angular/router'
import { NzAlertModule } from 'ng-zorro-antd/alert'
import { NzButtonModule } from 'ng-zorro-antd/button'
import { NzCardModule } from 'ng-zorro-antd/card'
import { NzIconModule } from 'ng-zorro-antd/icon'
import { NzModalModule } from 'ng-zorro-antd/modal'
import { NzRadioModule } from 'ng-zorro-antd/radio'
import { NzTableModule } from 'ng-zorro-antd/table'
import { NzTagModule } from 'ng-zorro-antd/tag'
import { RuntimeControlComponent } from './runtime-control.component'

const routes: Routes = [{ path: '', component: RuntimeControlComponent }]

@NgModule({
  declarations: [RuntimeControlComponent],
  imports: [
    CommonModule,
    FormsModule,
    RouterModule.forChild(routes),
    NzButtonModule,
    NzCardModule,
    NzIconModule,
    NzModalModule,
    NzAlertModule,
    NzRadioModule,
    NzTableModule,
    NzTagModule,
  ],
})
export class RuntimeControlModule {}
